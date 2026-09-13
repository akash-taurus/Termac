package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/atotto/clipboard"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tui/pkg/auth"
	"tui/pkg/config"
	"tui/pkg/explorer"
	"tui/pkg/git"
	"tui/pkg/github"
	"tui/pkg/plugin"
	"tui/pkg/shell"
	"tui/pkg/system"
	"tui/pkg/term"
	"tui/pkg/theme"
)

var version = "1.1.0"

// RepoStatus represents the git status of a repository
type RepoStatus string

const (
	StatusClean      RepoStatus = "Clean"
	StatusModified   RepoStatus = "Modified"
	StatusHasChanges RepoStatus = "Has Changes"
	StatusLoading    RepoStatus = "Loading..."
	StatusError      RepoStatus = "Error"
)

// RepoDetail represents a repository with full details
type RepoDetail struct {
	Name          string
	Path          string
	Remote        string
	Branch        string
	LastMessage   string
	LastAuthor    string
	Status        RepoStatus
	HasError      bool
	ErrorMsg      string
	Stars         int
	Forks         int
	Language      string
	OpenPRs       int
	OpenPRList    []github.PullRequest
	OpenPRsLoaded bool
	LocalOnly     bool
}

// ViewMode defines the current active dashboard tab
type ViewMode int

const (
	ViewLocal ViewMode = iota
	ViewGitHub
	ViewSystem
	ViewPlugins
)

// DashboardModel is the main Bubble Tea model
type DashboardModel struct {
	width           int
	height          int
	viewMode        ViewMode
	repos           []RepoDetail
	selected        int
	detailWidth     int
	spinner         spinner.Model
	message         string
	version         string
	userName        string
	scanning        bool
	token           *string

	// Theme
	themeIndex int

	// GitHub Authentication Modal
	authModalOpen   bool
	tokenInput      textinput.Model
	deviceCode      *auth.DeviceCodeResponse
	authPolling     bool
	authError       string

	// System Performance Monitor
	sysCollector *system.Collector
	sysSnapshot  system.SystemSnapshot

	// Plugin Manager
	pluginManager   *plugin.Manager
	plugins         []*plugin.PluginInstance
	selectedPlugin  int

	// In-Repo Directory & File Explorer
	explorerMode   bool
	activeExplorer *explorer.Explorer

	// Open Local Folder Modal
	openFolderModalOpen bool
	folderInput         textinput.Model

	// Git Commit Modal
	commitModalOpen bool
	commitInput     textinput.Model

	// Detailed Git State for active repo
	detailedGitStatus *git.DetailedGitStatus
	gitDiffActive     bool
	gitDiffText       string
	gitLogActive      bool
	gitLogItems       []git.CommitLogItem

	// Create & Push to GitHub Modal
	publishModalOpen bool
	publishNameInput textinput.Model
	publishPrivate   bool
}

func initialModel() DashboardModel {
	s := spinner.New()
	s.Spinner = spinner.Dot

	ti := textinput.New()
	ti.Placeholder = "Paste GitHub PAT (ghp_...) or token here"
	ti.CharLimit = 120
	ti.Width = 50
	ti.EchoMode = textinput.EchoNormal

	fInput := textinput.New()
	fInput.Placeholder = "Enter or paste directory path (e.g. Z:\\CodeBase\\TUI or .)"
	fInput.CharLimit = 250
	fInput.Width = 56
	fInput.EchoMode = textinput.EchoNormal

	cInput := textinput.New()
	cInput.Placeholder = "Enter commit message (e.g. feat: add git actions)"
	cInput.CharLimit = 160
	cInput.Width = 56
	cInput.EchoMode = textinput.EchoNormal

	pubInput := textinput.New()
	pubInput.Placeholder = "repository-name"
	pubInput.CharLimit = 100
	pubInput.Width = 40
	pubInput.EchoMode = textinput.EchoNormal

	pluginsDir, _ := config.PluginsPath()
	if _, err := os.Stat(pluginsDir); os.IsNotExist(err) {
		if _, localErr := os.Stat("plugins"); localErr == nil {
			pluginsDir = "plugins"
		}
	}

	collector := system.NewCollector(40)
	snap := collector.TakeSnapshot()

	mgr := plugin.NewManager(pluginsDir)
	discovered, _ := mgr.DiscoverPlugins()

	// Check if already authenticated
	var existingToken *string
	var existingUser string
	if tok, err := auth.LoadToken(); err == nil && tok.AccessToken != "" {
		existingToken = &tok.AccessToken
		if valid, _ := auth.ValidateToken(tok); valid {
			client := github.NewClient(existingToken)
			if u, err := client.GetAuthenticatedUser(); err == nil {
				existingUser = u.Login
			}
		}
	}

	// Always pre-load current working directory so user has it immediately
	var initialRepos []RepoDetail
	var initialGitStatus *git.DetailedGitStatus
	var initialGitLogs []git.CommitLogItem

	if cwd, err := os.Getwd(); err == nil {
		absCwd, _ := filepath.Abs(cwd)
		name := filepath.Base(absCwd)
		isGit := git.IsGitRepository(absCwd)
		branch := "(no git repo)"
		status := RepoStatus("Not a Git Repo")
		remote := "local"
		lastMsg := ""
		if isGit {
			if info, err := git.GetRepositoryInfo(absCwd); err == nil {
				branch = info.Branch
				lastMsg = info.Message
				remote = info.Remote
				status = StatusHasChanges
			}
			initialGitStatus, _ = git.GitStatusDetailed(absCwd)
			initialGitLogs, _ = git.GitLog(absCwd, 8)
			if initialGitStatus != nil && initialGitStatus.IsClean {
				status = StatusClean
			}
		}
		initialRepos = append(initialRepos, RepoDetail{
			Name:        name,
			Path:        absCwd,
			Branch:      branch,
			Remote:      remote,
			LastMessage: lastMsg,
			Status:      status,
			LocalOnly:   true,
		})
	}

	selectedIdx := -1
	if len(initialRepos) > 0 {
		selectedIdx = 0
	}

	return DashboardModel{
		version:           version,
		spinner:           s,
		viewMode:          ViewLocal,
		repos:             initialRepos,
		selected:          selectedIdx,
		themeIndex:        0,
		token:             existingToken,
		userName:          existingUser,
		tokenInput:        ti,
		folderInput:       fInput,
		commitInput:       cInput,
		publishNameInput:  pubInput,
		publishPrivate:    false,
		detailedGitStatus: initialGitStatus,
		gitLogItems:       initialGitLogs,
		sysCollector:      collector,
		sysSnapshot:       snap,
		pluginManager:     mgr,
		plugins:           discovered,
		selectedPlugin:    0,
		message:           "Welcome to Terminal Dashboard! Press [o] to open any local folder, [f] to explore, [a/c/P/F] for Git actions.",
	}
}

func (m DashboardModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		systemTickCmd(),
	)
}

// Tick message for system metrics polling
type systemTickMsg time.Time

func systemTickCmd() tea.Cmd {
	return tea.Tick(1500*time.Millisecond, func(t time.Time) tea.Msg {
		return systemTickMsg(t)
	})
}

// scanCommand scans for local repos asynchronously
func scanCommand(ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		home, _ := os.UserHomeDir()
		if home == "" {
			home = "."
		}
		repos, err := git.ScanDirectory(home, 3)
		if err != nil {
			return scanErrorMsg{Err: err}
		}
		return scanCompleteMsg{repos: repos}
	}
}

// githubLoginCommand attempts auto-authentication with existing token
func githubLoginCommand(ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		token, err := auth.LoadToken()
		if err != nil {
			return githubLoginMsg{Success: false, Err: err}
		}
		valid, err := auth.ValidateToken(token)
		if err != nil || !valid {
			return githubLoginMsg{Success: false, Err: fmt.Errorf("token invalid or expired")}
		}
		client := github.NewClient(&token.AccessToken)
		user, err := client.GetAuthenticatedUser()
		loginName := ""
		if err == nil && user != nil {
			loginName = user.Login
		}
		return githubLoginMsg{Success: true, Token: &token.AccessToken, User: loginName}
	}
}

// githubSubmitTokenCommand validates a user-provided PAT and saves it
func githubSubmitTokenCommand(tokenStr string) tea.Cmd {
	return func() tea.Msg {
		tok, user, err := auth.SaveTokenString(tokenStr)
		if err != nil {
			return githubTokenSubmitMsg{Success: false, Err: err}
		}
		return githubTokenSubmitMsg{Success: true, Token: &tok.AccessToken, User: user}
	}
}

// githubStartDeviceFlowCommand initiates device code grant
func githubStartDeviceFlowCommand() tea.Cmd {
	return func() tea.Msg {
		resp, err := auth.RequestDeviceCode("")
		if err != nil {
			return githubDeviceCodeMsg{Err: err}
		}
		return githubDeviceCodeMsg{Response: resp}
	}
}

// githubPollDeviceTokenCommand waits for user confirmation in browser
func githubPollDeviceTokenCommand(code *auth.DeviceCodeResponse) tea.Cmd {
	return func() tea.Msg {
		tok, err := auth.PollForDeviceToken("", code)
		if err != nil {
			return githubTokenSubmitMsg{Success: false, Err: err}
		}
		client := github.NewClient(&tok.AccessToken)
		loginName := ""
		if u, err := client.GetAuthenticatedUser(); err == nil && u != nil {
			loginName = u.Login
		}
		return githubTokenSubmitMsg{Success: true, Token: &tok.AccessToken, User: loginName}
	}
}

// githubReposCommand fetches repos from GitHub
func githubReposCommand(ctx context.Context, token *string) tea.Cmd {
	return func() tea.Msg {
		client := github.NewClient(token)
		user, err := client.GetAuthenticatedUser()
		if err != nil {
			return githubReposMsg{Err: err}
		}
		repos, err := client.GetRepositories(1, 30)
		if err != nil {
			return githubReposMsg{Err: err}
		}
		return githubReposMsg{User: user.Login, Repos: repos}
	}
}

// repoDetailCommand loads details for a specific repo
func repoDetailCommand(ctx context.Context, repo RepoDetail) tea.Cmd {
	return func() tea.Msg {
		if repo.LocalOnly {
			if !git.IsGitRepository(repo.Path) {
				updated := repo
				updated.Branch = "(no git repo)"
				updated.Status = RepoStatus("Not a Git Repo")
				return repoDetailMsg{
					Detail:    updated,
					GitStatus: &git.DetailedGitStatus{IsGitRepo: false},
				}
			}

			info, err := git.GetRepositoryInfo(repo.Path)
			if err != nil {
				updated := repo
				updated.HasError = true
				updated.ErrorMsg = err.Error()
				return repoDetailMsg{Detail: updated}
			}
			updated := repo
			updated.Branch = info.Branch
			updated.LastMessage = info.Message
			updated.Remote = info.Remote
			updated.Status = StatusHasChanges

			st, _ := git.GitStatusDetailed(repo.Path)
			logs, _ := git.GitLog(repo.Path, 15)
			if st != nil && st.IsClean {
				updated.Status = StatusClean
			}

			return repoDetailMsg{
				Detail:    updated,
				GitStatus: st,
				GitLogs:   logs,
			}
		}

		token, err := auth.LoadToken()
		if err != nil {
			return repoDetailMsg{Detail: repo}
		}

		client := github.NewClient(&token.AccessToken)
		parts := strings.SplitN(repo.Name, "/", 2)
		if len(parts) == 2 {
			owner, repoName := parts[0], parts[1]
			prs, err := client.GetPullRequests(owner, repoName)
			if err == nil {
				repo.OpenPRList = prs
				repo.OpenPRs = len(prs)
				repo.OpenPRsLoaded = true
			}
			commits, err := client.GetRepoCommits(owner, repoName, 1)
			if err == nil && len(commits) > 0 {
				repo.LastMessage = commits[0].Message
				repo.LastAuthor = commits[0].Author
			}
			repo.Status = StatusClean
		}
		return repoDetailMsg{Detail: repo}
	}
}

// Message types
type scanCompleteMsg struct{ repos []git.LocalRepository }
type scanErrorMsg struct{ Err error }
type githubLoginMsg struct {
	Success bool
	Token   *string
	User    string
	Err     error
}
type githubTokenSubmitMsg struct {
	Success bool
	Token   *string
	User    string
	Err     error
}
type githubDeviceCodeMsg struct {
	Response *auth.DeviceCodeResponse
	Err      error
}
type githubReposMsg struct {
	User  string
	Repos []github.Repo
	Err   error
}
type repoDetailMsg struct {
	Detail    RepoDetail
	GitStatus *git.DetailedGitStatus
	GitLogs   []git.CommitLogItem
}

type gitActionMsg struct {
	Action string
	Repo   string
	Output string
	Err    error
}

func gitPushCmd(repoPath string) tea.Cmd {
	return func() tea.Msg {
		out, err := git.GitPush(repoPath)
		return gitActionMsg{Action: "push", Repo: repoPath, Output: out, Err: err}
	}
}

func gitPullCmd(repoPath string) tea.Cmd {
	return func() tea.Msg {
		out, err := git.GitPull(repoPath)
		return gitActionMsg{Action: "pull", Repo: repoPath, Output: out, Err: err}
	}
}

func gitCommitCmd(repoPath, message string) tea.Cmd {
	return func() tea.Msg {
		_ = git.GitStageAll(repoPath)
		out, err := git.GitCommit(repoPath, message)
		return gitActionMsg{Action: "commit", Repo: repoPath, Output: out, Err: err}
	}
}

type guiFolderPickMsg struct {
	Path string
	Err  error
}

func pickFolderCmd() tea.Cmd {
	return func() tea.Msg {
		path, err := shell.OpenFolderDialog("Select a local folder to open in Terminal Dashboard")
		return guiFolderPickMsg{Path: path, Err: err}
	}
}

type publishResultMsg struct {
	Repo   *github.Repo
	Output string
	Err    error
}

func publishAndPushCmd(token, userName, repoPath, repoName, description string, isPrivate bool) tea.Cmd {
	return func() tea.Msg {
		if token == "" {
			return publishResultMsg{Err: fmt.Errorf("GitHub authentication required. Please press [l] to authenticate first")}
		}
		if repoName == "" {
			repoName = filepath.Base(repoPath)
		}

		// 1. If not a git repo, initialize it
		if !git.IsGitRepository(repoPath) {
			if err := git.GitInit(repoPath); err != nil {
				return publishResultMsg{Err: fmt.Errorf("git init failed: %w", err)}
			}
		}

		// 2. Ensure git user identity locally if none configured
		checkIdentity := exec.Command("git", "config", "user.name")
		checkIdentity.Dir = repoPath
		if out, err := checkIdentity.Output(); err != nil || strings.TrimSpace(string(out)) == "" {
			authorName := userName
			if authorName == "" {
				authorName = "User"
			}
			_ = exec.Command("git", "config", "user.name", authorName).Run()
			_ = exec.Command("git", "config", "user.email", authorName+"@users.noreply.github.com").Run()
		}

		_ = git.GitEnsureBranch(repoPath, "main")

		// 3. Stage all files and commit if there are pending/untracked files
		status, err := git.GitStatusDetailed(repoPath)
		if err == nil && (!status.IsClean || len(status.Files) > 0) {
			_ = git.GitStageAll(repoPath)
			_, _ = git.GitCommit(repoPath, fmt.Sprintf("Initial commit for %s", repoName))
		}

		// 4. Create repository on GitHub via API
		client := github.NewClient(&token)
		ghRepo, err := client.CreateRepository(github.CreateRepoRequest{
			Name:        repoName,
			Description: description,
			Private:     isPrivate,
			AutoInit:    false,
		})
		if err != nil {
			return publishResultMsg{Err: fmt.Errorf("GitHub repository creation failed: %w", err)}
		}

		// 5. Add or set remote origin
		if err := git.GitSetRemote(repoPath, "origin", ghRepo.CloneURL); err != nil {
			return publishResultMsg{Err: fmt.Errorf("failed to configure remote origin: %w", err)}
		}

		// 6. Push local commits to remote with authentication
		branch := git.GitBranchName(repoPath)
		if branch == "" {
			branch = "main"
		}

		pushURL := ghRepo.CloneURL
		if strings.HasPrefix(pushURL, "https://") {
			pushURL = strings.Replace(pushURL, "https://", fmt.Sprintf("https://x-access-token:%s@", token), 1)
		}

		out, err := git.GitPushUpstream(repoPath, pushURL, branch)
		// Reset origin in .git/config to clean URL without token
		_ = git.GitSetRemote(repoPath, "origin", ghRepo.CloneURL)

		if err != nil {
			return publishResultMsg{
				Repo: ghRepo,
				Err:  fmt.Errorf("created repository %s on GitHub, but push failed: %s (%v)", ghRepo.FullName, out, err),
			}
		}

		return publishResultMsg{
			Repo:   ghRepo,
			Output: out,
		}
	}
}

type pluginActionMsg struct {
	ID     string
	Action string
	Err    error
}

func (m DashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.detailWidth = m.width / 2
		return m, nil

	case systemTickMsg:
		if m.sysCollector != nil {
			m.sysSnapshot = m.sysCollector.TakeSnapshot()
		}
		return m, systemTickCmd()

	case tea.KeyMsg:
		// If GitHub authentication modal is open, capture input for text field
		if m.authModalOpen {
			switch msg.String() {
			case "esc":
				m.authModalOpen = false
				m.authPolling = false
				m.deviceCode = nil
				m.authError = ""
				m.tokenInput.Blur()
				return m, nil

			case "enter":
				val := strings.TrimSpace(m.tokenInput.Value())
				if val != "" {
					m.message = "Verifying GitHub token..."
					m.authError = ""
					return m, githubSubmitTokenCommand(val)
				}
				return m, nil

			case "c", "C":
				// Check environment variable
				if tok, err := auth.LoadToken(); err == nil && tok.AccessToken != "" {
					m.message = "Verifying token from environment..."
					return m, githubSubmitTokenCommand(tok.AccessToken)
				}
				m.authError = "No GITHUB_TOKEN or GH_TOKEN found in environment"
				return m, nil

			case "d", "D":
				// Initiate Device Flow
				m.message = "Requesting GitHub device authorization code..."
				m.authError = ""
				return m, githubStartDeviceFlowCommand()
			}

			m.tokenInput, cmd = m.tokenInput.Update(msg)
			return m, cmd
		}

		// If Open Folder modal is open
		if m.openFolderModalOpen {
			switch msg.String() {
			case "esc":
				m.openFolderModalOpen = false
				m.folderInput.Blur()
				return m, nil

			case "ctrl+b", "tab", "f2":
				m.message = "Opening Windows GUI File Explorer folder dialog..."
				return m, pickFolderCmd()

			case "enter":
				val := strings.TrimSpace(m.folderInput.Value())
				if val == "" {
					val = "."
				}
				absPath, err := filepath.Abs(val)
				if err != nil {
					m.message = fmt.Sprintf("Invalid path: %v", err)
					return m, nil
				}
				fi, err := os.Stat(absPath)
				if err != nil || !fi.IsDir() {
					m.message = fmt.Sprintf("Directory does not exist: %s", absPath)
					return m, nil
				}

				m.openFolderModalOpen = false
				m.folderInput.Blur()

				foundIdx := -1
				for idx, r := range m.repos {
					if filepath.Clean(r.Path) == filepath.Clean(absPath) {
						foundIdx = idx
						break
					}
				}

				if foundIdx != -1 {
					m.selected = foundIdx
				} else {
					name := filepath.Base(absPath)
					isGit := git.IsGitRepository(absPath)
					branch := "(no git repo)"
					status := RepoStatus("Not a Git Repo")
					remote := "local"
					lastMsg := ""
					if isGit {
						if info, err := git.GetRepositoryInfo(absPath); err == nil {
							branch = info.Branch
							lastMsg = info.Message
							remote = info.Remote
							status = StatusHasChanges
						}
					}
					newRepo := RepoDetail{
						Name:        name,
						Path:        absPath,
						Branch:      branch,
						Remote:      remote,
						LastMessage: lastMsg,
						Status:      status,
						LocalOnly:   true,
					}
					m.repos = append([]RepoDetail{newRepo}, m.repos...)
					m.selected = 0
				}

				m.message = fmt.Sprintf("Opened folder: %s", absPath)
				if m.selected >= 0 && m.selected < len(m.repos) {
					return m, repoDetailCommand(context.Background(), m.repos[m.selected])
				}
				return m, nil
			}

			m.folderInput, cmd = m.folderInput.Update(msg)
			return m, cmd
		}

		// If Commit modal is open
		if m.commitModalOpen {
			switch msg.String() {
			case "esc":
				m.commitModalOpen = false
				m.commitInput.Blur()
				return m, nil

			case "enter":
				msgText := strings.TrimSpace(m.commitInput.Value())
				if msgText == "" {
					m.message = "Commit message cannot be empty"
					return m, nil
				}
				m.commitModalOpen = false
				m.commitInput.Blur()
				if m.selected >= 0 && m.selected < len(m.repos) {
					repoPath := m.repos[m.selected].Path
					m.message = fmt.Sprintf("Committing changes to %s...", m.repos[m.selected].Name)
					return m, gitCommitCmd(repoPath, msgText)
				}
				return m, nil
			}

			m.commitInput, cmd = m.commitInput.Update(msg)
			return m, cmd
		}

		// If Publish & Push to GitHub modal is open
		if m.publishModalOpen {
			switch msg.String() {
			case "esc":
				m.publishModalOpen = false
				m.publishNameInput.Blur()
				return m, nil

			case "tab", "v", "V":
				m.publishPrivate = !m.publishPrivate
				return m, nil

			case "enter":
				val := strings.TrimSpace(m.publishNameInput.Value())
				if val == "" {
					m.message = "Repository name cannot be empty"
					return m, nil
				}
				m.publishModalOpen = false
				m.publishNameInput.Blur()
				if m.selected >= 0 && m.selected < len(m.repos) {
					repoPath := m.repos[m.selected].Path
					token := ""
					if m.token != nil {
						token = *m.token
					}
					m.message = fmt.Sprintf("Creating GitHub repo '%s' and pushing...", val)
					return m, publishAndPushCmd(token, m.userName, repoPath, val, "Created from Terminal Dashboard", m.publishPrivate)
				}
				return m, nil
			}

			m.publishNameInput, cmd = m.publishNameInput.Update(msg)
			return m, cmd
		}

		// If Directory Explorer is active, handle explorer navigation
		if m.explorerMode && m.activeExplorer != nil {
			switch msg.String() {
			case "q", "ctrl+c":
				m.message = "Shutting down plugins and restoring console..."
				if m.pluginManager != nil {
					m.pluginManager.StopAll()
				}
				_ = term.DisableMouseInput()
				return m, tea.Quit

			case "esc", "b", "B":
				m.explorerMode = false
				m.message = "Exited directory explorer"
				return m, nil

			case "up", "k":
				if len(m.activeExplorer.Entries) > 0 && m.activeExplorer.Selected > 0 {
					m.activeExplorer.Selected--
					sel := m.activeExplorer.Entries[m.activeExplorer.Selected]
					if !sel.IsDir {
						_ = m.activeExplorer.LoadPreview(sel.Path, 150)
					} else {
						m.activeExplorer.PreviewPath = ""
						m.activeExplorer.PreviewText = ""
					}
				}
				return m, nil

			case "down", "j":
				if len(m.activeExplorer.Entries) > 0 && m.activeExplorer.Selected < len(m.activeExplorer.Entries)-1 {
					m.activeExplorer.Selected++
					sel := m.activeExplorer.Entries[m.activeExplorer.Selected]
					if !sel.IsDir {
						_ = m.activeExplorer.LoadPreview(sel.Path, 150)
					} else {
						m.activeExplorer.PreviewPath = ""
						m.activeExplorer.PreviewText = ""
					}
				}
				return m, nil

			case "enter", "right", "l", "L":
				isDir, err := m.activeExplorer.OpenSelected()
				if err != nil {
					m.message = fmt.Sprintf("Error opening item: %v", err)
				} else if isDir {
					m.message = fmt.Sprintf("Entered directory: %s", m.activeExplorer.RelativeCurrentPath())
				} else {
					if m.activeExplorer.Selected >= 0 && m.activeExplorer.Selected < len(m.activeExplorer.Entries) {
						m.message = fmt.Sprintf("Previewing file: %s", m.activeExplorer.Entries[m.activeExplorer.Selected].Name)
					}
				}
				return m, nil

			case "backspace", "left", "h", "H":
				if m.activeExplorer.GoUp() {
					m.message = fmt.Sprintf("Navigated up to: %s", m.activeExplorer.RelativeCurrentPath())
				} else {
					m.message = "Already at repository root"
				}
				return m, nil

			case "e", "E":
				if err := m.activeExplorer.OpenInFileExplorer(); err != nil {
					m.message = fmt.Sprintf("Failed to launch Windows Explorer: %v", err)
				} else {
					m.message = fmt.Sprintf("Opened in Windows File Explorer: %s", m.activeExplorer.CurrentDir)
				}
				return m, nil

			case "p", "P":
				if err := m.activeExplorer.OpenInTerminal(); err != nil {
					m.message = fmt.Sprintf("Failed to launch Terminal: %v", err)
				} else {
					m.message = fmt.Sprintf("Opened Terminal at: %s", m.activeExplorer.CurrentDir)
				}
				return m, nil

			case "v", "V":
				if err := m.activeExplorer.OpenInVSCode(); err != nil {
					m.message = fmt.Sprintf("Failed to launch VS Code: %v", err)
				} else {
					m.message = fmt.Sprintf("Launched VS Code: %s", m.activeExplorer.CurrentDir)
				}
				return m, nil

			case "r", "R":
				if err := m.activeExplorer.Refresh(); err != nil {
					m.message = fmt.Sprintf("Refresh error: %v", err)
				} else {
					m.message = fmt.Sprintf("Refreshed contents of %s", m.activeExplorer.RelativeCurrentPath())
				}
				return m, nil

			case "t", "T":
				m.themeIndex = (m.themeIndex + 1) % len(theme.AvailableThemes)
				newTheme := theme.GetThemeByIndex(m.themeIndex)
				m.message = fmt.Sprintf("Switched theme to %s", newTheme.Name)
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			m.message = "Shutting down plugins and restoring console..."
			if m.pluginManager != nil {
				m.pluginManager.StopAll()
			}
			_ = term.DisableMouseInput()
			return m, tea.Quit

		case "esc":
			if m.gitDiffActive || m.gitLogActive {
				m.gitDiffActive = false
				m.gitLogActive = false
				m.message = "Exited diff/log view"
				return m, nil
			}

		case "b", "B":
			if m.viewMode == ViewLocal {
				m.message = "Opening Windows GUI File Explorer folder dialog..."
				return m, pickFolderCmd()
			}

		case "o", "O":
			if m.viewMode == ViewLocal {
				m.openFolderModalOpen = true
				m.folderInput.Focus()
				m.folderInput.Reset()
				return m, textinput.Blink
			}

		case "a", "A":
			if m.viewMode == ViewLocal && m.selected >= 0 && m.selected < len(m.repos) {
				repo := m.repos[m.selected]
				if git.IsGitRepository(repo.Path) {
					if err := git.GitStageAll(repo.Path); err != nil {
						m.message = fmt.Sprintf("Stage error: %v", err)
					} else {
						m.message = fmt.Sprintf("Staged all changes in %s (git add -A)", repo.Name)
						return m, repoDetailCommand(context.Background(), repo)
					}
				} else {
					m.message = "Not a Git repository. Press [i] to initialize git."
				}
				return m, nil
			}

		case "c", "C":
			if m.viewMode == ViewLocal && m.selected >= 0 && m.selected < len(m.repos) {
				repo := m.repos[m.selected]
				if git.IsGitRepository(repo.Path) {
					m.commitModalOpen = true
					m.commitInput.Focus()
					m.commitInput.Reset()
					return m, textinput.Blink
				} else {
					m.message = "Not a Git repository. Press [i] to initialize git."
				}
				return m, nil
			}

		case "n", "N":
			if m.viewMode == ViewLocal && m.selected >= 0 && m.selected < len(m.repos) {
				if m.token == nil {
					m.message = "GitHub authentication required to create and push repositories. Please login with [l]."
					m.authModalOpen = true
					m.tokenInput.Focus()
					return m, textinput.Blink
				}
				repo := m.repos[m.selected]
				cleanName := strings.ToLower(strings.ReplaceAll(repo.Name, " ", "-"))
				m.publishNameInput.SetValue(cleanName)
				m.publishNameInput.Focus()
				m.publishModalOpen = true
				m.publishPrivate = false
				m.message = fmt.Sprintf("Publishing local folder %s to GitHub...", repo.Name)
				return m, textinput.Blink
			}

		case "P":
			if m.viewMode == ViewLocal && m.selected >= 0 && m.selected < len(m.repos) {
				repo := m.repos[m.selected]
				if !git.IsGitRepository(repo.Path) || repo.Remote == "" || repo.Remote == "local" {
					if m.token == nil {
						m.message = "No remote configured. Please authenticate with GitHub [l] to create a new remote repo."
						m.authModalOpen = true
						m.tokenInput.Focus()
						return m, textinput.Blink
					}
					cleanName := strings.ToLower(strings.ReplaceAll(repo.Name, " ", "-"))
					m.publishNameInput.SetValue(cleanName)
					m.publishNameInput.Focus()
					m.publishModalOpen = true
					m.publishPrivate = false
					m.message = "No remote repository configured. Set repository details to create and push:"
					return m, textinput.Blink
				}
				m.message = fmt.Sprintf("Pushing commits for %s to remote...", repo.Name)
				return m, gitPushCmd(repo.Path)
			}

		case "F":
			if m.viewMode == ViewLocal && m.selected >= 0 && m.selected < len(m.repos) {
				repo := m.repos[m.selected]
				if git.IsGitRepository(repo.Path) {
					m.message = fmt.Sprintf("Pulling latest changes for %s...", repo.Name)
					return m, gitPullCmd(repo.Path)
				} else {
					m.message = "Not a Git repository."
				}
				return m, nil
			}

		case "i", "I":
			if m.viewMode == ViewLocal && m.selected >= 0 && m.selected < len(m.repos) {
				repo := m.repos[m.selected]
				if !git.IsGitRepository(repo.Path) {
					if err := git.GitInit(repo.Path); err != nil {
						m.message = fmt.Sprintf("Git init error: %v", err)
					} else {
						m.message = fmt.Sprintf("Initialized Git repository in %s!", repo.Path)
						return m, repoDetailCommand(context.Background(), repo)
					}
				} else {
					m.message = "Already a Git repository."
				}
				return m, nil
			}

		case "d", "D":
			if m.viewMode == ViewLocal && m.selected >= 0 && m.selected < len(m.repos) {
				repo := m.repos[m.selected]
				if git.IsGitRepository(repo.Path) {
					m.gitDiffActive = !m.gitDiffActive
					m.gitLogActive = false
					if m.gitDiffActive {
						out, _ := git.GitDiff(repo.Path)
						m.gitDiffText = out
						m.message = fmt.Sprintf("Viewing Git Diff for %s — Press [d] or [Esc] to close", repo.Name)
					} else {
						m.message = "Closed Git Diff view"
					}
				} else {
					m.message = "Not a Git repository."
				}
				return m, nil
			}

		case "g", "G":
			if m.viewMode == ViewLocal && m.selected >= 0 && m.selected < len(m.repos) {
				repo := m.repos[m.selected]
				if git.IsGitRepository(repo.Path) {
					m.gitLogActive = !m.gitLogActive
					m.gitDiffActive = false
					if m.gitLogActive {
						logs, _ := git.GitLog(repo.Path, 15)
						m.gitLogItems = logs
						m.message = fmt.Sprintf("Viewing Git Log for %s — Press [g] or [Esc] to close", repo.Name)
					} else {
						m.message = "Closed Git Log view"
					}
				} else {
					m.message = "Not a Git repository."
				}
				return m, nil
			}

		case "f":
			if m.selected >= 0 && m.selected < len(m.repos) {
				targetPath := m.repos[m.selected].Path
				if fi, err := os.Stat(targetPath); err == nil && fi.IsDir() {
					exp, err := explorer.NewExplorer(targetPath)
					if err != nil {
						m.message = fmt.Sprintf("Explorer error: %v", err)
					} else {
						m.activeExplorer = exp
						m.explorerMode = true
						m.message = fmt.Sprintf("Exploring %s (%s) — Press [Esc] to exit", m.repos[m.selected].Name, exp.RelativeCurrentPath())
					}
				} else {
					m.message = fmt.Sprintf("Repository path does not exist on disk: %s", targetPath)
				}
				return m, nil
			}

		case "e", "E":
			if m.selected >= 0 && m.selected < len(m.repos) {
				targetPath := m.repos[m.selected].Path
				if fi, err := os.Stat(targetPath); err == nil && fi.IsDir() {
					_ = exec.Command("explorer.exe", targetPath).Start()
					m.message = fmt.Sprintf("Opened in Windows File Explorer: %s", targetPath)
				}
				return m, nil
			}

		case "p":
			if m.selected >= 0 && m.selected < len(m.repos) {
				targetPath := m.repos[m.selected].Path
				if fi, err := os.Stat(targetPath); err == nil && fi.IsDir() {
					if _, err := exec.LookPath("wt.exe"); err == nil {
						_ = exec.Command("wt.exe", "-d", targetPath).Start()
					} else {
						_ = exec.Command("cmd.exe", "/c", "start", "powershell.exe", "-NoExit", "-Command", fmt.Sprintf("Set-Location -LiteralPath '%s'", targetPath)).Start()
					}
					m.message = fmt.Sprintf("Opened Terminal at: %s", targetPath)
				}
				return m, nil
			}

		case "l", "L":
			if m.viewMode == ViewGitHub {
				m.authModalOpen = true
				m.tokenInput.Focus()
				m.tokenInput.Reset()
				m.authError = ""
				m.deviceCode = nil
				return m, textinput.Blink
			}

		case "u", "U":
			if m.viewMode == ViewGitHub {
				_ = auth.DeleteToken()
				m.token = nil
				m.userName = ""
				m.repos = nil
				m.message = "Logged out from GitHub (token deleted)."
				return m, nil
			}

		case "t", "T":
			m.themeIndex = (m.themeIndex + 1) % len(theme.AvailableThemes)
			newTheme := theme.GetThemeByIndex(m.themeIndex)
			m.message = fmt.Sprintf("Switched theme to %s", newTheme.Name)
			return m, nil

		case "w", "W":
			exePath, err := os.Executable()
			if err == nil {
				iconPath := filepath.Join(filepath.Dir(exePath), "assets", "icon.ico")
				err = shell.RegisterTerminalProfile(exePath, iconPath)
				if err == nil {
					m.message = "Successfully added Terminal Dashboard profile to Windows Terminal!"
				} else {
					m.message = fmt.Sprintf("Windows Terminal profile notice: %v", err)
				}
			}
			return m, nil

		case "tab":
			m.viewMode = (m.viewMode + 1) % 4
			return m, m.handleViewChange()

		case "shift+tab":
			if m.viewMode == 0 {
				m.viewMode = 3
			} else {
				m.viewMode--
			}
			return m, m.handleViewChange()

		case "1":
			m.viewMode = ViewLocal
			return m, m.handleViewChange()

		case "2":
			m.viewMode = ViewGitHub
			return m, m.handleViewChange()

		case "3":
			m.viewMode = ViewSystem
			m.message = "System Performance Monitor (Real-time polling active)"
			if m.sysCollector != nil {
				m.sysSnapshot = m.sysCollector.TakeSnapshot()
			}
			return m, nil

		case "4":
			m.viewMode = ViewPlugins
			if m.pluginManager != nil {
				m.plugins, _ = m.pluginManager.DiscoverPlugins()
			}
			m.message = "Interactive Plugin Manager (Named Pipe gRPC)"
			return m, nil

		case "r", "R":
			if m.viewMode == ViewLocal {
				if !m.scanning {
					m.scanning = true
					m.message = "Scanning local repositories..."
					return m, scanCommand(context.Background())
				}
			} else if m.viewMode == ViewGitHub {
				if m.token != nil {
					m.message = "Refreshing GitHub repositories..."
					return m, githubReposCommand(context.Background(), m.token)
				} else {
					m.authModalOpen = true
					m.tokenInput.Focus()
					return m, textinput.Blink
				}
			} else if m.viewMode == ViewSystem {
				if m.sysCollector != nil {
					m.sysSnapshot = m.sysCollector.TakeSnapshot()
				}
				m.message = "System metrics refreshed."
				return m, nil
			} else if m.viewMode == ViewPlugins {
				if m.selectedPlugin >= 0 && m.selectedPlugin < len(m.plugins) {
					p := m.plugins[m.selectedPlugin]
					m.message = fmt.Sprintf("Reloading plugin %s...", p.Name)
					_ = m.pluginManager.FetchAndRender(context.Background(), p.ID, 80, 24)
					m.plugins = m.pluginManager.ListInstances()
				}
				return m, nil
			}

		case "up", "k":
			if m.viewMode == ViewPlugins {
				if len(m.plugins) > 0 && m.selectedPlugin > 0 {
					m.selectedPlugin--
				}
			} else {
				if len(m.repos) > 0 && m.selected > 0 {
					m.selected--
					m.gitDiffActive = false
					m.gitLogActive = false
					if m.viewMode == ViewLocal {
						return m, repoDetailCommand(context.Background(), m.repos[m.selected])
					}
				}
			}
			return m, nil

		case "down", "j":
			if m.viewMode == ViewPlugins {
				if len(m.plugins) > 0 && m.selectedPlugin < len(m.plugins)-1 {
					m.selectedPlugin++
				}
			} else {
				if len(m.repos) > 0 && m.selected < len(m.repos)-1 {
					m.selected++
					m.gitDiffActive = false
					m.gitLogActive = false
					if m.viewMode == ViewLocal {
						return m, repoDetailCommand(context.Background(), m.repos[m.selected])
					}
				}
			}
			return m, nil

		case "s", "S":
			if m.viewMode == ViewPlugins && len(m.plugins) > 0 && m.selectedPlugin < len(m.plugins) {
				p := m.plugins[m.selectedPlugin]
				m.message = fmt.Sprintf("Starting plugin %s over Named Pipe...", p.Name)
				mgr := m.pluginManager
				pluginID := p.ID
				return m, func() tea.Msg {
					err := mgr.StartPlugin(context.Background(), pluginID)
					return pluginActionMsg{ID: pluginID, Action: "start", Err: err}
				}
			}

		case "x", "X":
			if m.viewMode == ViewPlugins && len(m.plugins) > 0 && m.selectedPlugin < len(m.plugins) {
				p := m.plugins[m.selectedPlugin]
				m.message = fmt.Sprintf("Stopping plugin %s...", p.Name)
				_ = m.pluginManager.StopPlugin(p.ID)
				m.plugins = m.pluginManager.ListInstances()
				return m, nil
			}

		case "enter":
			if m.viewMode == ViewPlugins {
				if m.selectedPlugin >= 0 && m.selectedPlugin < len(m.plugins) {
					p := m.plugins[m.selectedPlugin]
					_ = m.pluginManager.FetchAndRender(context.Background(), p.ID, 80, 24)
					m.plugins = m.pluginManager.ListInstances()
				}
			} else if m.viewMode == ViewGitHub && m.token == nil {
				m.authModalOpen = true
				m.tokenInput.Focus()
				return m, textinput.Blink
			} else {
				if m.selected >= 0 && m.selected < len(m.repos) {
					targetPath := m.repos[m.selected].Path
					if m.viewMode == ViewLocal {
						if fi, err := os.Stat(targetPath); err == nil && fi.IsDir() {
							exp, err := explorer.NewExplorer(targetPath)
							if err == nil {
								m.activeExplorer = exp
								m.explorerMode = true
								m.message = fmt.Sprintf("Exploring %s (%s) — Press [Esc] to exit", m.repos[m.selected].Name, exp.RelativeCurrentPath())
								return m, nil
							}
						}
					}
					m.message = fmt.Sprintf("Loading details for %s...", m.repos[m.selected].Name)
					return m, repoDetailCommand(context.Background(), m.repos[m.selected])
				}
			}
			return m, nil
		}

	case githubTokenSubmitMsg:
		if msg.Success {
			m.token = msg.Token
			m.userName = msg.User
			m.authModalOpen = false
			m.authPolling = false
			m.tokenInput.Blur()
			m.message = fmt.Sprintf("Successfully authenticated as @%s! Fetching repositories...", msg.User)
			return m, githubReposCommand(context.Background(), msg.Token)
		}
		m.authError = msg.Err.Error()
		m.message = fmt.Sprintf("Authentication error: %v", msg.Err)
		return m, nil

	case githubDeviceCodeMsg:
		if msg.Err != nil {
			m.authError = fmt.Sprintf("Device code error: %v", msg.Err)
			return m, nil
		}
		m.deviceCode = msg.Response
		m.authPolling = true
		// Automatically copy code to clipboard for user convenience
		_ = clipboard.WriteAll(msg.Response.UserCode)
		// Automatically launch default browser to verification URL
		_ = exec.Command("cmd", "/c", "start", msg.Response.VerificationURL).Start()
		m.message = fmt.Sprintf("Code %s copied to clipboard! Browser opened to %s", msg.Response.UserCode, msg.Response.VerificationURL)
		return m, githubPollDeviceTokenCommand(msg.Response)

	case pluginActionMsg:
		if msg.Err != nil {
			m.message = fmt.Sprintf("Plugin error (%s): %v", msg.ID, msg.Err)
		} else {
			m.message = fmt.Sprintf("Plugin %s active over Named Pipe!", msg.ID)
		}
		if m.pluginManager != nil {
			m.plugins = m.pluginManager.ListInstances()
		}
		return m, nil

	case spinner.TickMsg:
		var sCmd tea.Cmd
		m.spinner, sCmd = m.spinner.Update(msg)
		return m, sCmd

	case scanCompleteMsg:
		m.scanning = false
		var details []RepoDetail
		for _, r := range msg.repos {
			info, err := git.GetRepositoryInfo(r.Path)
			if err != nil {
				details = append(details, RepoDetail{
					Name:      filepath.Base(r.Path),
					Path:      r.Path,
					Branch:    "unknown",
					Status:    StatusError,
					HasError:  true,
					ErrorMsg:  err.Error(),
					LocalOnly: true,
				})
				continue
			}
			details = append(details, RepoDetail{
				Name:        filepath.Base(info.Path),
				Path:        info.Path,
				Branch:      info.Branch,
				LastMessage: info.Message,
				Status:      StatusHasChanges,
				Remote:      info.Remote,
				LocalOnly:   true,
			})
		}
		m.repos = details
		m.selected = 0
		m.message = fmt.Sprintf("Found %d local repository(s)", len(details))
		if len(details) > 0 {
			return m, repoDetailCommand(context.Background(), details[0])
		}
		return m, nil

	case scanErrorMsg:
		m.scanning = false
		m.message = fmt.Sprintf("Scan error: %v", msg.Err)
		return m, nil

	case githubLoginMsg:
		if msg.Success {
			m.token = msg.Token
			m.userName = msg.User
			m.message = fmt.Sprintf("GitHub authenticated as @%s! Fetching repositories...", msg.User)
			return m, githubReposCommand(context.Background(), msg.Token)
		}
		// If no token exists, open auth modal so user can enter PAT
		m.message = "GitHub token required. Please login using a Personal Access Token (PAT)."
		m.authModalOpen = true
		m.tokenInput.Focus()
		return m, textinput.Blink

	case githubReposMsg:
		if msg.Err != nil {
			m.message = fmt.Sprintf("GitHub error: %v (press [l] to re-authenticate)", msg.Err)
			return m, nil
		}
		m.userName = msg.User
		var details []RepoDetail
		for _, r := range msg.Repos {
			details = append(details, RepoDetail{
				Name:      r.FullName,
				Path:      r.URL,
				Branch:    r.Branch,
				Status:    StatusLoading,
				Remote:    r.CloneURL,
				Stars:     r.Stars,
				Forks:     r.Forks,
				Language:  r.Language,
				LocalOnly: false,
			})
		}
		m.repos = details
		m.selected = 0
		m.message = fmt.Sprintf("Loaded %d GitHub repository(s) for @%s", len(details), msg.User)
		return m, nil

	case repoDetailMsg:
		if m.selected >= 0 && m.selected < len(m.repos) {
			m.repos[m.selected] = msg.Detail
			m.detailedGitStatus = msg.GitStatus
			m.gitLogItems = msg.GitLogs
			m.message = fmt.Sprintf("Updated: %s", msg.Detail.Name)
		}
		return m, nil

	case gitActionMsg:
		if msg.Err != nil {
			m.message = fmt.Sprintf("Git %s failed: %v", msg.Action, msg.Err)
		} else {
			if strings.TrimSpace(msg.Output) != "" {
				firstLine := strings.Split(strings.TrimSpace(msg.Output), "\n")[0]
				m.message = fmt.Sprintf("Git %s: %s", msg.Action, firstLine)
			} else {
				m.message = fmt.Sprintf("Git %s succeeded!", msg.Action)
			}
		}
		if m.selected >= 0 && m.selected < len(m.repos) {
			return m, repoDetailCommand(context.Background(), m.repos[m.selected])
		}
		return m, nil

	case guiFolderPickMsg:
		if msg.Err != nil {
			m.message = fmt.Sprintf("GUI folder picker error: %v", msg.Err)
			return m, nil
		}
		if strings.TrimSpace(msg.Path) == "" {
			m.message = "GUI folder selection canceled."
			return m, nil
		}

		absPath, err := filepath.Abs(strings.TrimSpace(msg.Path))
		if err != nil {
			m.message = fmt.Sprintf("Invalid selected path: %v", err)
			return m, nil
		}
		fi, err := os.Stat(absPath)
		if err != nil || !fi.IsDir() {
			m.message = fmt.Sprintf("Directory does not exist: %s", absPath)
			return m, nil
		}

		m.openFolderModalOpen = false
		m.folderInput.Blur()

		foundIdx := -1
		for idx, r := range m.repos {
			if filepath.Clean(r.Path) == filepath.Clean(absPath) {
				foundIdx = idx
				break
			}
		}

		if foundIdx != -1 {
			m.selected = foundIdx
		} else {
			name := filepath.Base(absPath)
			isGit := git.IsGitRepository(absPath)
			branch := "(no git repo)"
			status := RepoStatus("Not a Git Repo")
			remote := "local"
			lastMsg := ""
			if isGit {
				if info, err := git.GetRepositoryInfo(absPath); err == nil {
					branch = info.Branch
					lastMsg = info.Message
					remote = info.Remote
					status = StatusHasChanges
				}
			}
			newRepo := RepoDetail{
				Name:        name,
				Path:        absPath,
				Branch:      branch,
				Remote:      remote,
				LastMessage: lastMsg,
				Status:      status,
				LocalOnly:   true,
			}
			m.repos = append([]RepoDetail{newRepo}, m.repos...)
			m.selected = 0
		}

		m.message = fmt.Sprintf("Navigated to folder via GUI Explorer: %s", absPath)
		if m.selected >= 0 && m.selected < len(m.repos) {
			return m, repoDetailCommand(context.Background(), m.repos[m.selected])
		}
		return m, nil

	case publishResultMsg:
		if msg.Err != nil {
			m.message = fmt.Sprintf("Publish error: %v", msg.Err)
		} else {
			m.message = fmt.Sprintf("🎉 Successfully created & pushed to GitHub: %s! (%s)", msg.Repo.FullName, msg.Repo.Branch)
			if m.selected >= 0 && m.selected < len(m.repos) {
				m.repos[m.selected].Remote = msg.Repo.CloneURL
			}
		}
		if m.selected >= 0 && m.selected < len(m.repos) {
			return m, repoDetailCommand(context.Background(), m.repos[m.selected])
		}
		return m, nil
	}

	return m, nil
}

func (m *DashboardModel) handleViewChange() tea.Cmd {
	switch m.viewMode {
	case ViewLocal:
		m.message = "Showing local repositories"
		if !m.scanning && len(m.repos) == 0 {
			m.scanning = true
			return scanCommand(context.Background())
		}
	case ViewGitHub:
		if m.token == nil {
			m.message = "Authenticating with GitHub..."
			return githubLoginCommand(context.Background())
		}
		if len(m.repos) == 0 {
			m.message = "Fetching GitHub repositories..."
			return githubReposCommand(context.Background(), m.token)
		}
	case ViewSystem:
		m.message = "System Performance Monitor (Real-time polling active)"
		if m.sysCollector != nil {
			m.sysSnapshot = m.sysCollector.TakeSnapshot()
		}
	case ViewPlugins:
		m.message = "Interactive Plugin Manager (Named Pipe gRPC)"
		if m.pluginManager != nil {
			m.plugins, _ = m.pluginManager.DiscoverPlugins()
		}
	}
	return nil
}

func (m DashboardModel) View() string {
	pal := theme.GetThemeByIndex(m.themeIndex)

	termWidth := m.width
	if termWidth <= 0 {
		termWidth = 100
	}
	termHeight := m.height
	if termHeight <= 0 {
		termHeight = 30
	}

	// 1. Top Title Bar & Header
	logo := lipgloss.NewStyle().
		Bold(true).
		Background(pal.Primary).
		Foreground(pal.Background).
		Padding(0, 1).
		Render("⚡ TERMINAL DASHBOARD")

	verPill := lipgloss.NewStyle().
		Bold(true).
		Background(pal.Border).
		Foreground(pal.Foreground).
		Padding(0, 1).
		Render("v" + m.version)

	leftHeader := lipgloss.JoinHorizontal(lipgloss.Center, logo, verPill)

	themePill := lipgloss.NewStyle().
		Foreground(pal.Highlight).
		Bold(true).
		Render("🎨 " + pal.Name) + lipgloss.NewStyle().Foreground(pal.Muted).Render(" [t]")

	var userPill string
	if m.userName != "" {
		userPill = lipgloss.NewStyle().
			Bold(true).
			Foreground(pal.Success).
			Render("● @" + m.userName)
	} else {
		userPill = lipgloss.NewStyle().
			Foreground(pal.Muted).
			Render("○ Guest [l]")
	}

	rightHeader := lipgloss.JoinHorizontal(
		lipgloss.Center,
		themePill,
		lipgloss.NewStyle().Foreground(pal.Border).Render("  │  "),
		userPill,
	)

	headerGap := termWidth - lipgloss.Width(leftHeader) - lipgloss.Width(rightHeader) - 2
	if headerGap < 1 {
		headerGap = 1
	}
	headerLine := lipgloss.JoinHorizontal(lipgloss.Center, leftHeader, strings.Repeat(" ", headerGap), rightHeader)

	// 2. Navigation Tabs
	tabLocal := formatTab("1", "Local Repos", "📁", m.viewMode == ViewLocal && !m.explorerMode, pal)
	tabGH := formatTab("2", "GitHub", "🐙", m.viewMode == ViewGitHub && !m.explorerMode, pal)
	tabSys := formatTab("3", "System Metrics", "📈", m.viewMode == ViewSystem, pal)
	tabPlug := formatTab("4", "Plugins", "🔌", m.viewMode == ViewPlugins, pal)

	tabsList := []string{tabLocal, tabGH, tabSys, tabPlug}
	if m.explorerMode && m.activeExplorer != nil {
		expTab := lipgloss.NewStyle().
			Bold(true).
			Background(pal.Highlight).
			Foreground(pal.Background).
			Padding(0, 1).
			MarginRight(1).
			Render(fmt.Sprintf(" 📂 EXPLORER: %s ", m.activeExplorer.RelativeCurrentPath()))
		tabsList = append(tabsList, expTab)
	}
	tabsLine := lipgloss.JoinHorizontal(lipgloss.Top, tabsList...)

	// 3. Subtitle / Shortcut Keycaps Bar
	var keyItems []string
	if m.explorerMode {
		keyItems = []string{
			renderKeyBadge("Enter / →", "Open", pal),
			renderKeyBadge("Backspace / ←", "Up", pal),
			renderKeyBadge("e", "GUI Explorer", pal),
			renderKeyBadge("p", "Terminal", pal),
			renderKeyBadge("v", "VS Code", pal),
			renderKeyBadge("r", "Refresh", pal),
			renderKeyBadge("Esc / b", "Back to Repos", pal),
		}
	} else if m.viewMode == ViewGitHub {
		keyItems = []string{
			renderKeyBadge("Tab", "Switch Tab", pal),
			renderKeyBadge("l", "Login / PAT", pal),
			renderKeyBadge("u", "Logout", pal),
			renderKeyBadge("r", "Refresh", pal),
			renderKeyBadge("t", "Theme", pal),
			renderKeyBadge("q", "Quit", pal),
		}
	} else if m.viewMode == ViewPlugins {
		keyItems = []string{
			renderKeyBadge("Tab", "Switch Tab", pal),
			renderKeyBadge("s", "Start Plugin", pal),
			renderKeyBadge("x", "Stop", pal),
			renderKeyBadge("r", "Reload / Render", pal),
			renderKeyBadge("j/k", "Select", pal),
			renderKeyBadge("t", "Theme", pal),
			renderKeyBadge("q", "Quit", pal),
		}
	} else if m.viewMode == ViewSystem {
		keyItems = []string{
			renderKeyBadge("Tab", "Switch Tab", pal),
			renderKeyBadge("r", "Refresh Metrics", pal),
			renderKeyBadge("t", "Theme", pal),
			renderKeyBadge("w", "Add to WT", pal),
			renderKeyBadge("q", "Quit", pal),
		}
	} else {
		// ViewLocal
		keyItems = []string{
			renderKeyBadge("n", "Create & Push", pal),
			renderKeyBadge("b", "Browse GUI", pal),
			renderKeyBadge("o", "Open Folder", pal),
			renderKeyBadge("a", "Stage All", pal),
			renderKeyBadge("c", "Commit", pal),
			renderKeyBadge("P", "Push", pal),
			renderKeyBadge("F", "Pull", pal),
			renderKeyBadge("d", "Diff", pal),
			renderKeyBadge("g", "Log", pal),
			renderKeyBadge("f / Enter", "Files", pal),
			renderKeyBadge("e", "GUI Explorer", pal),
			renderKeyBadge("p", "Terminal", pal),
			renderKeyBadge("t", "Theme", pal),
			renderKeyBadge("q", "Quit", pal),
		}
	}

	bullet := lipgloss.NewStyle().Foreground(pal.Border).Render("  •  ")
	subtitleLine := " " + strings.Join(keyItems, bullet)

	// Thin horizontal divider
	dividerLine := lipgloss.NewStyle().Foreground(pal.Border).Render(strings.Repeat("─", termWidth))

	// 4. Main Body Height Management
	mainHeight := termHeight - 8
	if mainHeight < 14 {
		mainHeight = 14
	}

	var mainBody string
	if m.authModalOpen {
		mainBody = m.renderAuthModal(pal)
	} else if m.openFolderModalOpen {
		mainBody = m.renderOpenFolderModal(pal)
	} else if m.commitModalOpen {
		mainBody = m.renderCommitModal(pal)
	} else if m.publishModalOpen {
		mainBody = m.renderPublishModal(pal)
	} else if m.explorerMode && m.activeExplorer != nil {
		mainBody = m.renderExplorerView(pal, termWidth, mainHeight)
	} else {
		switch m.viewMode {
		case ViewLocal, ViewGitHub:
			mainBody = m.renderRepoView(pal, termWidth, mainHeight)
		case ViewSystem:
			mainBody = m.renderSystemView(pal, termWidth, mainHeight)
		case ViewPlugins:
			mainBody = m.renderPluginsView(pal, termWidth, mainHeight)
		}
	}

	// 5. Status Bar with Indicator Icon
	statusIcon := lipgloss.NewStyle().Foreground(pal.Highlight).Render("ℹ ")
	msgLower := strings.ToLower(m.message)
	if strings.Contains(msgLower, "error") || strings.Contains(msgLower, "fail") {
		statusIcon = lipgloss.NewStyle().Foreground(pal.Danger).Bold(true).Render("✖ ")
	} else if strings.Contains(msgLower, "success") || strings.Contains(msgLower, "authenticat") || strings.Contains(msgLower, "clean") {
		statusIcon = lipgloss.NewStyle().Foreground(pal.Success).Bold(true).Render("✔ ")
	} else if strings.Contains(msgLower, "scan") || strings.Contains(msgLower, "load") || strings.Contains(msgLower, "polling") {
		statusIcon = lipgloss.NewStyle().Foreground(pal.Warning).Render("◌ ")
	}

	statusBar := lipgloss.NewStyle().
		Background(pal.Background).
		Foreground(pal.Foreground).
		Padding(0, 1).
		Render(fmt.Sprintf("%sStatus: %s", statusIcon, m.message))

	// 6. Bottom Footer
	footerLeft := lipgloss.NewStyle().
		Foreground(pal.Muted).
		Render("🐙 GitHub: " + userPill)

	footerCenter := lipgloss.NewStyle().
		Foreground(pal.Muted).
		Render("🔒 Zero-TCP Named Pipe IPC  •  🛡️ Windows Job Object Guard")

	footerRight := lipgloss.NewStyle().
		Foreground(pal.Muted).
		Render(fmt.Sprintf("v%s • UTF-8 VTP", m.version))

	footerMid := lipgloss.NewStyle().Foreground(pal.Border).Render("  │  ")
	footerCombinedLeft := lipgloss.JoinHorizontal(lipgloss.Center, footerLeft, footerMid, footerCenter)
	footerGap := termWidth - lipgloss.Width(footerCombinedLeft) - lipgloss.Width(footerRight) - 2
	if footerGap < 1 {
		footerGap = 1
	}
	footer := lipgloss.JoinHorizontal(lipgloss.Center, footerCombinedLeft, strings.Repeat(" ", footerGap), footerRight)

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		headerLine,
		tabsLine,
		subtitleLine,
		dividerLine,
		mainBody,
		statusBar,
		footer,
	)

	return lipgloss.NewStyle().Width(termWidth).Height(termHeight).Render(content)
}

func formatTab(num, name, icon string, active bool, pal theme.Palette) string {
	if active {
		label := fmt.Sprintf(" %s %s %s ",
			lipgloss.NewStyle().Bold(true).Foreground(pal.Highlight).Render(num),
			icon,
			name,
		)
		return lipgloss.NewStyle().
			Bold(true).
			Background(pal.Secondary).
			Foreground(pal.Foreground).
			Padding(0, 1).
			MarginRight(1).
			Render(label)
	}
	label := fmt.Sprintf(" %s %s %s ",
		lipgloss.NewStyle().Foreground(pal.Muted).Render(num),
		icon,
		name,
	)
	return lipgloss.NewStyle().
		Foreground(pal.Muted).
		Padding(0, 1).
		MarginRight(1).
		Render(label)
}

func renderKeyBadge(key, label string, pal theme.Palette) string {
	k := lipgloss.NewStyle().Bold(true).Foreground(pal.Highlight).Render("[" + key + "]")
	l := lipgloss.NewStyle().Foreground(pal.Foreground).Render(" " + label)
	return k + l
}

func (m DashboardModel) renderAuthModal(pal theme.Palette) string {
	modalWidth := 64
	var b strings.Builder

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Primary).Render(" 🔐 GitHub Authentication\n\n"))
	b.WriteString("  Authenticate to view your private repositories, issues, and PRs.\n\n")

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render("  Option 1: Personal Access Token (PAT)\n"))
	b.WriteString("  Create token at: https://github.com/settings/tokens (Scope: repo, read:org)\n\n")
	b.WriteString(fmt.Sprintf("  Token: %s\n\n", m.tokenInput.View()))

	if m.deviceCode != nil {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Warning).Render("  Option 2: Device Code Authorization Active!\n"))
		b.WriteString(fmt.Sprintf("    1. Open URL:  %s\n", lipgloss.NewStyle().Bold(true).Foreground(pal.Highlight).Render(m.deviceCode.VerificationURL)))
		b.WriteString(fmt.Sprintf("    2. Enter Code: %s\n", lipgloss.NewStyle().Bold(true).Foreground(pal.Primary).Render(m.deviceCode.UserCode)))
		b.WriteString(fmt.Sprintf("    3. Waiting for authorization... %s\n\n", m.spinner.View()))
	} else {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render("  Option 2: GitHub Device Flow\n"))
		b.WriteString("  Press [d] to request a browser device verification code.\n\n")
	}

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render("  Option 3: Environment Variables\n"))
	b.WriteString("  Press [c] to check and import GITHUB_TOKEN or GH_TOKEN.\n\n")

	if m.authError != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(pal.Danger).Bold(true).Render(fmt.Sprintf("  ✖ Error: %s\n\n", m.authError)))
	}

	b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render("  Controls: [Enter] Submit PAT | [d] Device Flow | [c] Check Env | [Esc] Cancel\n"))

	return lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(pal.Primary).
		Padding(1, 2).
		Width(modalWidth).
		Render(b.String())
}

func (m DashboardModel) renderOpenFolderModal(pal theme.Palette) string {
	modalWidth := 72
	var b strings.Builder

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Primary).Render(" 📂 Open Local Folder\n\n"))
	b.WriteString("  Enter the directory path to open in the dashboard.\n")
	b.WriteString("  • If it is a Git repository, all branches, worktree status, diffs, and actions will load.\n")
	b.WriteString("  • If it is NOT a Git repo, you can initialize it with [i] (git init) or explore files.\n\n")

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render("  Folder Path:\n"))
	b.WriteString(fmt.Sprintf("  %s\n\n", m.folderInput.View()))

	b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render("  Examples:\n"))
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Foreground).Render("    • .                     (current working directory)\n"))
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Foreground).Render("    • Z:\\CodeBase\\TUI       (absolute path)\n"))
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Foreground).Render("    • ..\\another-project    (relative path)\n\n"))

	b.WriteString(lipgloss.NewStyle().Foreground(pal.Highlight).Bold(true).Render("  ⚡ Prefer GUI? Press [Tab] or [Ctrl+B] to launch Windows GUI File Explorer Dialog!\n\n"))
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render("  Controls: [Tab / Ctrl+B] Browse GUI | [Enter] Open Typed Path | [Esc] Cancel\n"))

	return lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(pal.Primary).
		Padding(1, 2).
		Width(modalWidth).
		Render(b.String())
}

func (m DashboardModel) renderCommitModal(pal theme.Palette) string {
	modalWidth := 72
	var b strings.Builder

	repoName := ""
	if m.selected >= 0 && m.selected < len(m.repos) {
		repoName = m.repos[m.selected].Name
	}

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Primary).Render(fmt.Sprintf(" 💾 Commit Changes — %s\n\n", repoName)))
	b.WriteString("  All modified and untracked files will be automatically staged (git add -A)\n")
	b.WriteString("  before creating the commit.\n\n")

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render("  Commit Message:\n"))
	b.WriteString(fmt.Sprintf("  %s\n\n", m.commitInput.View()))

	if m.detailedGitStatus != nil && len(m.detailedGitStatus.Files) > 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(pal.Warning).Bold(true).Render(fmt.Sprintf("  Changes to be committed (%d file(s)):\n", len(m.detailedGitStatus.Files))))
		limit := 4
		for idx, f := range m.detailedGitStatus.Files {
			if idx >= limit {
				b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render(fmt.Sprintf("    ... and %d more file(s)\n", len(m.detailedGitStatus.Files)-limit)))
				break
			}
			codeColor := pal.Success
			if strings.Contains(f.Status, "?") {
				codeColor = pal.Highlight
			} else if strings.Contains(f.Status, "M") {
				codeColor = pal.Warning
			}
			b.WriteString(fmt.Sprintf("    [%s] %s\n", lipgloss.NewStyle().Foreground(codeColor).Bold(true).Render(f.Status), f.Path))
		}
		b.WriteString("\n")
	}

	b.WriteString(lipgloss.NewStyle().Foreground(pal.Highlight).Render("  Controls: [Enter] Create Commit | [Esc] Cancel\n"))

	return lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(pal.Primary).
		Padding(1, 2).
		Width(modalWidth).
		Render(b.String())
}

func (m DashboardModel) renderPublishModal(pal theme.Palette) string {
	modalWidth := 74
	var b strings.Builder

	repoPath := ""
	if m.selected >= 0 && m.selected < len(m.repos) {
		repoPath = m.repos[m.selected].Path
	}

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Primary).Render(" 🚀 Create New GitHub Repository & Push\n\n"))
	b.WriteString(fmt.Sprintf("  Local Folder: %s\n\n",
		lipgloss.NewStyle().Foreground(pal.Highlight).Bold(true).Render(truncPath(repoPath, 50))))

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render("  Repository Name on GitHub:\n"))
	b.WriteString(fmt.Sprintf("  %s\n\n", m.publishNameInput.View()))

	var visBadge string
	if m.publishPrivate {
		visBadge = lipgloss.NewStyle().Bold(true).Background(pal.Warning).Foreground(pal.Background).Padding(0, 1).Render("🔒 Private") +
			lipgloss.NewStyle().Foreground(pal.Muted).Render("  (Only you can see this repository)")
	} else {
		visBadge = lipgloss.NewStyle().Bold(true).Background(pal.Success).Foreground(pal.Background).Padding(0, 1).Render("🌐 Public") +
			lipgloss.NewStyle().Foreground(pal.Muted).Render("  (Anyone on GitHub can see this repository)")
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render("  Visibility: ") + visBadge + "\n")
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Highlight).Render("  Press [v] or [Tab] to toggle Public / Private\n\n"))

	targetAccount := "@" + m.userName
	if m.userName == "" {
		targetAccount = "Guest (Login with [l])"
	}
	b.WriteString(fmt.Sprintf("  Target GitHub Account: %s\n\n", lipgloss.NewStyle().Bold(true).Foreground(pal.Accent).Render(targetAccount)))

	b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render("  Automated Actions upon [Enter]:\n"))
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Foreground).Render("    1. Initialize Git repository and set branch to 'main'\n"))
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Foreground).Render("    2. Stage all files (git add -A) and create initial commit\n"))
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Foreground).Render("    3. Create remote repository via GitHub REST API\n"))
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Foreground).Render("    4. Connect remote origin & push code (git push -u origin main)\n\n"))

	b.WriteString(lipgloss.NewStyle().Foreground(pal.Highlight).Render("  Controls: [Enter] Create & Push | [v / Tab] Toggle Visibility | [Esc] Cancel\n"))

	return lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(pal.Primary).
		Padding(1, 2).
		Width(modalWidth).
		Render(b.String())
}

func (m DashboardModel) renderRepoView(pal theme.Palette, totalWidth, totalHeight int) string {
	leftWidth := (totalWidth * 42) / 100
	if leftWidth < 36 {
		leftWidth = 36
	}
	if leftWidth > 52 {
		leftWidth = 52
	}
	rightWidth := totalWidth - leftWidth - 4
	if rightWidth < 38 {
		rightWidth = 38
	}

	// Left: Repo list
	var repoList strings.Builder
	repoTitle := "📦 Local Repositories"
	if m.viewMode == ViewGitHub {
		repoTitle = "🐙 GitHub Repositories"
	}
	repoList.WriteString(lipgloss.NewStyle().
		Bold(true).
		Foreground(pal.Primary).
		Render(fmt.Sprintf(" %s (%d)\n", repoTitle, len(m.repos))))

	repoList.WriteString(lipgloss.NewStyle().
		Foreground(pal.Muted).
		Render(fmt.Sprintf("   %-18s %-9s %s\n", "NAME", "BRANCH", "STATUS")))
	repoList.WriteString(lipgloss.NewStyle().
		Foreground(pal.Border).
		Render("  " + strings.Repeat("─", leftWidth-6) + "\n"))

	maxDisplay := totalHeight - 5
	if maxDisplay < 4 {
		maxDisplay = 4
	}

	if m.scanning {
		repoList.WriteString("\n  " + m.spinner.View() + " Scanning repositories...\n")
	} else if len(m.repos) == 0 {
		if m.viewMode == ViewGitHub && m.token == nil {
			repoList.WriteString("\n  Not authenticated with GitHub.\n\n  Press [l] to enter a Personal Access Token (PAT).\n")
		} else {
			repoList.WriteString("\n  No repositories found.\n  Press [r] to scan or refresh.\n")
		}
	} else {
		startIdx := 0
		if m.selected >= maxDisplay {
			startIdx = m.selected - maxDisplay + 1
		}
		endIdx := startIdx + maxDisplay
		if endIdx > len(m.repos) {
			endIdx = len(m.repos)
		}

		for i := startIdx; i < endIdx; i++ {
			repo := m.repos[i]
			var statusBadge string
			switch repo.Status {
			case StatusClean:
				statusBadge = lipgloss.NewStyle().Foreground(pal.Success).Render("● Clean")
			case StatusModified, StatusHasChanges:
				statusBadge = lipgloss.NewStyle().Foreground(pal.Warning).Render("▲ Changed")
			case StatusLoading:
				statusBadge = lipgloss.NewStyle().Foreground(pal.Accent).Render("◌ Loading")
			case StatusError:
				statusBadge = lipgloss.NewStyle().Foreground(pal.Danger).Render("✖ Error")
			default:
				statusBadge = lipgloss.NewStyle().Foreground(pal.Muted).Render(string(repo.Status))
			}

			nameColWidth := leftWidth - 24
			if nameColWidth < 12 {
				nameColWidth = 12
			}
			rName := trunc(repo.Name, nameColWidth)
			rBranch := trunc(repo.Branch, 8)
			if rBranch == "" {
				rBranch = "-"
			}

			if i == m.selected {
				rowText := fmt.Sprintf(" ▸ %-*s %-9s %s", nameColWidth, rName, rBranch, statusBadge)
				repoList.WriteString(lipgloss.NewStyle().
					Background(pal.Secondary).
					Foreground(pal.Foreground).
					Bold(true).
					Render(rowText) + "\n")
			} else {
				rowText := fmt.Sprintf("   %-*s %-9s %s\n", nameColWidth, rName, rBranch, statusBadge)
				repoList.WriteString(rowText)
			}
		}

		if len(m.repos) > maxDisplay {
			repoList.WriteString(lipgloss.NewStyle().
				Foreground(pal.Muted).
				Render(fmt.Sprintf("\n  Showing %d-%d of %d repos", startIdx+1, endIdx, len(m.repos))))
		}
	}

	leftStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Border).
		Width(leftWidth).
		Height(totalHeight).
		Padding(0, 1).
		Render(repoList.String())

	detailStr := m.viewDetail(pal, rightWidth, totalHeight)
	return lipgloss.JoinHorizontal(lipgloss.Top, leftStyle, detailStr)
}

func (m DashboardModel) renderGitDiffView(pal theme.Palette, width, height int) string {
	var b strings.Builder
	repoName := ""
	if m.selected >= 0 && m.selected < len(m.repos) {
		repoName = m.repos[m.selected].Name
	}

	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(pal.Primary).
		Render(fmt.Sprintf("📜 Git Diff (HEAD) — %s", repoName))
	b.WriteString(header + "  " + lipgloss.NewStyle().Foreground(pal.Highlight).Bold(true).Render("[d / Esc] Back\n\n"))

	if strings.TrimSpace(m.gitDiffText) == "" {
		b.WriteString(lipgloss.NewStyle().Foreground(pal.Success).Render("  ✔ Working tree is clean. No unstaged or staged changes relative to HEAD.\n\n"))
		b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render("  Press [c] to commit changes or [Esc] to exit diff view.\n"))
	} else {
		lines := strings.Split(m.gitDiffText, "\n")
		maxLines := height - 6
		if maxLines < 6 {
			maxLines = 6
		}
		dispCount := 0
		for _, line := range lines {
			if dispCount >= maxLines {
				b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render(fmt.Sprintf("\n  ... and %d more diff lines", len(lines)-maxLines)))
				break
			}
			dispCount++
			if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
				b.WriteString(lipgloss.NewStyle().Foreground(pal.Success).Render(line) + "\n")
			} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
				b.WriteString(lipgloss.NewStyle().Foreground(pal.Danger).Render(line) + "\n")
			} else if strings.HasPrefix(line, "@@") {
				b.WriteString(lipgloss.NewStyle().Foreground(pal.Highlight).Bold(true).Render(line) + "\n")
			} else if strings.HasPrefix(line, "diff --git") {
				b.WriteString(lipgloss.NewStyle().Foreground(pal.Primary).Bold(true).Render(line) + "\n")
			} else {
				b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render(line) + "\n")
			}
		}
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Highlight).
		Padding(0, 1).
		Width(width).
		Height(height).
		Render(b.String())
}

func (m DashboardModel) renderGitLogView(pal theme.Palette, width, height int) string {
	var b strings.Builder
	repoName := ""
	if m.selected >= 0 && m.selected < len(m.repos) {
		repoName = m.repos[m.selected].Name
	}

	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(pal.Primary).
		Render(fmt.Sprintf("📜 Git Commit History — %s", repoName))
	b.WriteString(header + "  " + lipgloss.NewStyle().Foreground(pal.Highlight).Bold(true).Render("[g / Esc] Back\n\n"))

	if len(m.gitLogItems) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render("  No commits found in this repository branch.\n\n"))
	} else {
		maxItems := (height - 6) / 2
		if maxItems < 3 {
			maxItems = 3
		}
		for i, item := range m.gitLogItems {
			if i >= maxItems {
				b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render(fmt.Sprintf("  ... and %d more commits\n", len(m.gitLogItems)-maxItems)))
				break
			}
			hashBadge := lipgloss.NewStyle().Bold(true).Foreground(pal.Highlight).Render(item.Hash)
			dateBadge := lipgloss.NewStyle().Foreground(pal.Muted).Render(item.Date)
			authorBadge := lipgloss.NewStyle().Foreground(pal.Secondary).Render("👤 " + item.Author)
			subj := lipgloss.NewStyle().Foreground(pal.Foreground).Render(trunc(item.Subject, width-10))

			b.WriteString(fmt.Sprintf("  %s  %s  %s\n", hashBadge, dateBadge, authorBadge))
			b.WriteString(fmt.Sprintf("    %s\n\n", subj))
		}
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Highlight).
		Padding(0, 1).
		Width(width).
		Height(height).
		Render(b.String())
}

func (m DashboardModel) viewDetail(pal theme.Palette, detailWidth, totalHeight int) string {
	if m.selected < 0 || m.selected >= len(m.repos) {
		hint := "\n  Select a repository from the list to view details.\n\n  Press [j/k] or [↑/↓] to navigate.\n"
		if m.viewMode == ViewGitHub && m.token == nil {
			hint = "\n  Press [l] to Login with your GitHub Personal Access Token (PAT)\n"
		}
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(pal.Border).
			Padding(0, 1).
			Width(detailWidth).
			Height(totalHeight).
			Render(hint)
	}

	if m.gitDiffActive {
		return m.renderGitDiffView(pal, detailWidth, totalHeight)
	}
	if m.gitLogActive {
		return m.renderGitLogView(pal, detailWidth, totalHeight)
	}

	repo := m.repos[m.selected]
	var b strings.Builder

	innerW := detailWidth - 6
	if innerW < 30 {
		innerW = 30
	}

	// Local directory check
	isGitRepo := true
	if repo.LocalOnly {
		isGitRepo = git.IsGitRepository(repo.Path)
	}

	if !isGitRepo {
		// Non-git folder card
		title := lipgloss.NewStyle().
			Bold(true).
			Foreground(pal.Primary).
			Render("📁 " + repo.Name)
		badge := lipgloss.NewStyle().
			Bold(true).
			Background(pal.Warning).
			Foreground(pal.Background).
			Padding(0, 1).
			Render("Non-Git Folder")
		b.WriteString(title + "  " + badge + "\n\n")

		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render(" ┌─ Folder Information " + strings.Repeat("─", max(innerW-22, 2)) + "┐\n"))
		b.WriteString(fmt.Sprintf(" │  %-10s : %s\n", "Path", lipgloss.NewStyle().Foreground(pal.Foreground).Render(truncPath(repo.Path, innerW-16))))
		b.WriteString(fmt.Sprintf(" │  %-10s : %s\n", "Status", lipgloss.NewStyle().Foreground(pal.Warning).Bold(true).Render("Regular Local Directory (No Git tracking)")))
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render(" └" + strings.Repeat("─", innerW) + "┘\n\n"))

		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Highlight).Render(" ⚡ Actions Available:\n"))
		btnPublish := lipgloss.NewStyle().Bold(true).Background(pal.Primary).Foreground(pal.Background).Padding(0, 1).Render("[n] Create & Push to GitHub")
		btnInit := lipgloss.NewStyle().Bold(true).Background(pal.Secondary).Foreground(pal.Foreground).Padding(0, 1).Render("[i] Git Init")
		b.WriteString("   " + btnPublish + "  " + btnInit + "\n\n")

		btnBrowse := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[b] Browse GUI")
		btnOpen := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[o] Open Folder")
		b.WriteString("   " + btnBrowse + "  " + btnOpen + "\n\n")

		btnFiles := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[f / Enter] Browse Files")
		btnExp := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[e] GUI Explorer")
		btnTerm := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[p] Terminal")
		b.WriteString("   " + btnFiles + "  " + btnExp + "  " + btnTerm + "\n")

		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(pal.Border).
			Padding(0, 1).
			Width(detailWidth).
			Height(totalHeight).
			Render(b.String())
	}

	// Title Card
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(pal.Primary).
		Render("📦 " + repo.Name)

	if repo.Language != "" {
		langPill := lipgloss.NewStyle().
			Bold(true).
			Background(pal.Border).
			Foreground(pal.Highlight).
			Padding(0, 1).
			Render(repo.Language)
		title += " " + langPill
	}
	b.WriteString(title + "\n\n")

	if repo.HasError {
		b.WriteString(lipgloss.NewStyle().
			Foreground(pal.Danger).
			Render(fmt.Sprintf("  ✖ Error: %s\n\n", repo.ErrorMsg)))
	}

	// Information Box
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render(" ┌─ Repository Information " + strings.Repeat("─", max(innerW-26, 2)) + "┐\n"))
	b.WriteString(fmt.Sprintf(" │  %-10s : %s\n", "Path", lipgloss.NewStyle().Foreground(pal.Foreground).Render(truncPath(repo.Path, innerW-16))))
	b.WriteString(fmt.Sprintf(" │  %-10s : %s\n", "Branch", lipgloss.NewStyle().Foreground(pal.Highlight).Render("⎇ "+repo.Branch)))
	if repo.Remote != "" {
		b.WriteString(fmt.Sprintf(" │  %-10s : %s\n", "Remote", lipgloss.NewStyle().Foreground(pal.Muted).Render(truncPath(repo.Remote, innerW-16))))
	}
	if !repo.LocalOnly {
		b.WriteString(fmt.Sprintf(" │  %-10s : ★ %-5d  ⑂ %-5d\n", "Community", repo.Stars, repo.Forks))
	} else if m.detailedGitStatus != nil && m.detailedGitStatus.Upstream != "" {
		b.WriteString(fmt.Sprintf(" │  %-10s : %s\n", "Upstream", lipgloss.NewStyle().Foreground(pal.Accent).Render(m.detailedGitStatus.Upstream)))
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render(" └" + strings.Repeat("─", innerW) + "┘\n\n"))

	// Git Worktree & Sync Box
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render(" ┌─ Git Worktree & Sync " + strings.Repeat("─", max(innerW-23, 2)) + "┐\n"))
	if m.detailedGitStatus != nil {
		syncStr := "✔ Up to date with remote"
		syncColor := pal.Success
		if m.detailedGitStatus.Ahead > 0 || m.detailedGitStatus.Behind > 0 {
			syncStr = fmt.Sprintf("↑ %d ahead  •  ↓ %d behind", m.detailedGitStatus.Ahead, m.detailedGitStatus.Behind)
			syncColor = pal.Warning
		} else if m.detailedGitStatus.Upstream == "" {
			syncStr = "Local branch (no remote tracking)"
			syncColor = pal.Muted
		}
		b.WriteString(fmt.Sprintf(" │  Sync   : %s\n", lipgloss.NewStyle().Foreground(syncColor).Bold(true).Render(syncStr)))

		stagedPill := lipgloss.NewStyle().Foreground(pal.Success).Bold(true).Render(fmt.Sprintf("● %d staged", m.detailedGitStatus.StagedCount))
		unstagedPill := lipgloss.NewStyle().Foreground(pal.Warning).Bold(true).Render(fmt.Sprintf("▲ %d modified", m.detailedGitStatus.UnstagedCount))
		untrackedPill := lipgloss.NewStyle().Foreground(pal.Highlight).Bold(true).Render(fmt.Sprintf("? %d untracked", m.detailedGitStatus.UntrackedCount))
		b.WriteString(fmt.Sprintf(" │  Status : %s  │  %s  │  %s\n", stagedPill, unstagedPill, untrackedPill))
	} else {
		var statusColor lipgloss.Color = pal.Success
		if repo.Status == StatusModified || repo.Status == StatusHasChanges {
			statusColor = pal.Warning
		} else if repo.HasError {
			statusColor = pal.Danger
		}
		b.WriteString(fmt.Sprintf(" │  Status : %s\n", lipgloss.NewStyle().Foreground(statusColor).Bold(true).Render(string(repo.Status))))
	}

	if repo.LastMessage != "" {
		b.WriteString(fmt.Sprintf(" │  Commit : %s\n", lipgloss.NewStyle().Foreground(pal.Foreground).Render("\""+trunc(repo.LastMessage, innerW-14)+"\"")))
	}
	if repo.LastAuthor != "" {
		b.WriteString(fmt.Sprintf(" │  Author : %s\n", lipgloss.NewStyle().Foreground(pal.Muted).Render("👤 "+repo.LastAuthor)))
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render(" └" + strings.Repeat("─", innerW) + "┘\n\n"))

	// PRs section if GitHub
	if !repo.LocalOnly {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render(" ┌─ Pull Requests " + strings.Repeat("─", max(innerW-17, 2)) + "┐\n"))
		if !repo.OpenPRsLoaded {
			b.WriteString(" │  Loading pull requests...\n")
		} else if len(repo.OpenPRList) == 0 {
			b.WriteString(" │  No open pull requests\n")
		} else {
			limit := 4
			for idx, pr := range repo.OpenPRList {
				if idx >= limit {
					b.WriteString(fmt.Sprintf(" │  ... and %d more PR(s)\n", len(repo.OpenPRList)-limit))
					break
				}
				b.WriteString(fmt.Sprintf(" │  #%-3d %s\n", pr.Number, trunc(pr.Title, innerW-12)))
			}
		}
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render(" └" + strings.Repeat("─", innerW) + "┘\n\n"))
	}

	// Action buttons
	if repo.LocalOnly {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Highlight).Render(" ⚡ Git Actions:\n"))
		btnStage := lipgloss.NewStyle().Bold(true).Background(pal.Secondary).Foreground(pal.Foreground).Padding(0, 1).Render("[a] Stage All")
		btnCommit := lipgloss.NewStyle().Bold(true).Background(pal.Secondary).Foreground(pal.Foreground).Padding(0, 1).Render("[c] Commit")
		btnPublish := lipgloss.NewStyle().Bold(true).Background(pal.Highlight).Foreground(pal.Background).Padding(0, 1).Render("[n] Create & Push Repo")
		b.WriteString("   " + btnStage + "  " + btnCommit + "  " + btnPublish + "\n\n")

		btnPush := lipgloss.NewStyle().Bold(true).Background(pal.Primary).Foreground(pal.Background).Padding(0, 1).Render("[P] Push")
		btnPull := lipgloss.NewStyle().Bold(true).Background(pal.Primary).Foreground(pal.Background).Padding(0, 1).Render("[F] Pull")
		btnDiff := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[d] Diff")
		btnLog := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[g] Log")
		b.WriteString("   " + btnPush + "  " + btnPull + "  " + btnDiff + "  " + btnLog + "\n\n")

		btnBrowse := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[b] Browse GUI")
		btnOpen := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[o] Open Folder")
		btnExplore := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[f / Enter] Files")
		btnExp := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[e] GUI Explorer")
		btnTerm := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[p] Terminal")
		b.WriteString("   " + btnBrowse + "  " + btnOpen + "  " + btnExplore + "  " + btnExp + "  " + btnTerm + "\n")
	} else if repo.Path != "" {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Highlight).Render(" ⚡ Quick Actions:\n"))
		btnExplore := lipgloss.NewStyle().Bold(true).Background(pal.Secondary).Foreground(pal.Foreground).Padding(0, 1).Render("[f / Enter] Explore Files")
		btnExp := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[e] GUI Explorer")
		btnTerm := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[p] Terminal")
		b.WriteString("   " + btnExplore + "  " + btnExp + "  " + btnTerm + "\n")
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Border).
		Padding(0, 1).
		Width(detailWidth).
		Height(totalHeight).
		Render(b.String())
}

func (m DashboardModel) renderExplorerView(pal theme.Palette, totalWidth, totalHeight int) string {
	exp := m.activeExplorer
	if exp == nil {
		return "No active explorer"
	}

	leftColWidth := (totalWidth * 42) / 100
	if leftColWidth < 36 {
		leftColWidth = 36
	}
	if leftColWidth > 52 {
		leftColWidth = 52
	}
	rightColWidth := totalWidth - leftColWidth - 4
	if rightColWidth < 40 {
		rightColWidth = 40
	}

	maxListRows := totalHeight - 5
	if maxListRows < 6 {
		maxListRows = 6
	}

	// 1. Left Column: Directory & File Listing
	var left strings.Builder
	currentRel := exp.RelativeCurrentPath()
	left.WriteString(lipgloss.NewStyle().
		Bold(true).
		Foreground(pal.Primary).
		Render(fmt.Sprintf(" 📂 %s", currentRel)) + " " +
		lipgloss.NewStyle().Foreground(pal.Muted).Render(fmt.Sprintf("(%d items)\n", len(exp.Entries))))

	left.WriteString(lipgloss.NewStyle().
		Foreground(pal.Muted).
		Render(fmt.Sprintf("   %-22s %-5s %7s\n", "NAME", "GIT", "SIZE")))
	left.WriteString(lipgloss.NewStyle().
		Foreground(pal.Border).
		Render("  " + strings.Repeat("─", leftColWidth-6) + "\n"))

	if len(exp.Entries) == 0 {
		left.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render("\n   (Empty directory)\n"))
	} else {
		startIdx := 0
		if exp.Selected >= maxListRows {
			startIdx = exp.Selected - maxListRows + 1
		}
		endIdx := startIdx + maxListRows
		if endIdx > len(exp.Entries) {
			endIdx = len(exp.Entries)
		}

		for i := startIdx; i < endIdx; i++ {
			entry := exp.Entries[i]

			icon := "📄 "
			if entry.IsDir {
				icon = "📁 "
			} else {
				switch entry.Ext {
				case ".go":
					icon = "🔷 "
				case ".md", ".txt":
					icon = "📝 "
				case ".json", ".yaml", ".yml", ".toml":
					icon = "⚙️  "
				case ".py":
					icon = "🐍 "
				case ".bat", ".ps1", ".cmd", ".sh":
					icon = "⚡ "
				case ".exe", ".dll":
					icon = "📦 "
				case ".png", ".jpg", ".ico", ".svg":
					icon = "🖼️  "
				}
			}

			gitBadge := "   "
			gitColor := pal.Muted
			if entry.GitStatus != "" {
				switch entry.GitStatus {
				case "M":
					gitBadge = "[M]"
					gitColor = pal.Warning
				case "?":
					gitBadge = "[?]"
					gitColor = pal.Accent
				case "A":
					gitBadge = "[A]"
					gitColor = pal.Success
				case "D":
					gitBadge = "[D]"
					gitColor = pal.Danger
				default:
					gitBadge = fmt.Sprintf("[%s]", entry.GitStatus)
					gitColor = pal.Primary
				}
			}

			sizeStr := "[DIR]"
			if !entry.IsDir {
				sizeStr = explorer.HumanSize(uint64(entry.Size))
			}

			nameWidth := leftColWidth - 22
			if nameWidth < 10 {
				nameWidth = 10
			}
			displayName := trunc(entry.Name, nameWidth)

			if i == exp.Selected {
				rowText := fmt.Sprintf(" ▸ %s%-*s %s %7s", icon, nameWidth, displayName, gitBadge, sizeStr)
				left.WriteString(lipgloss.NewStyle().
					Background(pal.Secondary).
					Foreground(pal.Foreground).
					Bold(true).
					Render(rowText) + "\n")
			} else {
				left.WriteString(fmt.Sprintf("   %s%-*s ", icon, nameWidth, displayName))
				left.WriteString(lipgloss.NewStyle().Foreground(gitColor).Render(fmt.Sprintf("%-3s ", gitBadge)))
				left.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render(fmt.Sprintf("%7s\n", sizeStr)))
			}
		}

		if len(exp.Entries) > maxListRows {
			left.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).
				Render(fmt.Sprintf("\n  Showing %d-%d of %d items", startIdx+1, endIdx, len(exp.Entries))))
		}
	}

	leftStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Border).
		Width(leftColWidth).
		Height(totalHeight).
		Padding(0, 1).
		Render(left.String())

	// 2. Right Column: File Preview / Directory Details
	var right strings.Builder
	innerW := rightColWidth - 6
	if innerW < 30 {
		innerW = 30
	}

	if exp.Selected >= 0 && exp.Selected < len(exp.Entries) {
		selectedEntry := exp.Entries[exp.Selected]

		if selectedEntry.IsDir {
			right.WriteString(lipgloss.NewStyle().
				Bold(true).
				Foreground(pal.Primary).
				Render(fmt.Sprintf(" 📁 %s\n\n", selectedEntry.Name)))

			right.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render(" ┌─ Directory Details " + strings.Repeat("─", max(innerW-21, 2)) + "┐\n"))
			right.WriteString(fmt.Sprintf(" │  Full Path : %s\n", lipgloss.NewStyle().Foreground(pal.Foreground).Render(truncPath(selectedEntry.Path, innerW-14))))
			right.WriteString(fmt.Sprintf(" │  Relative  : %s\n", lipgloss.NewStyle().Foreground(pal.Highlight).Render(truncPath(selectedEntry.RelPath, innerW-14))))
			right.WriteString(fmt.Sprintf(" │  Modified  : %s\n", selectedEntry.ModTime.Format("2006-01-02 15:04:05")))
			right.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render(" └" + strings.Repeat("─", innerW) + "┘\n\n"))

			right.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Highlight).Render(" ⚡ Folder Navigation & Actions:\n"))
			btnOpen := lipgloss.NewStyle().Bold(true).Background(pal.Secondary).Foreground(pal.Foreground).Padding(0, 1).Render("[Enter / →] Open")
			btnUp := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[Backspace / ←] Up")
			btnExp := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[e] Explorer")
			btnTerm := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[p] Terminal")
			btnCode := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[v] VS Code")
			btnBack := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[Esc] Back")

			right.WriteString("   " + btnOpen + "  " + btnUp + "\n")
			right.WriteString("   " + btnExp + "  " + btnTerm + "  " + btnCode + "  " + btnBack + "\n\n")

			if selectedEntry.GitStatus != "" {
				right.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Warning).
					Render(fmt.Sprintf("  ▲ Git Status: Folder has uncommitted changes [%s]\n", selectedEntry.GitStatus)))
			}
		} else {
			// File preview
			right.WriteString(lipgloss.NewStyle().
				Bold(true).
				Foreground(pal.Primary).
				Render(fmt.Sprintf(" 📄 %s", selectedEntry.Name)))

			infoLine := fmt.Sprintf("  %s • %s",
				explorer.HumanSize(uint64(selectedEntry.Size)),
				selectedEntry.ModTime.Format("2006-01-02 15:04:05"))
			if selectedEntry.GitStatus != "" {
				infoLine += fmt.Sprintf(" • Git: [%s]", selectedEntry.GitStatus)
			}
			right.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render(infoLine + "\n\n"))

			btnExp := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[e] Explorer")
			btnTerm := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[p] Terminal")
			btnCode := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[v] VS Code")
			btnBack := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[Esc] Back")
			right.WriteString("  " + btnExp + "  " + btnTerm + "  " + btnCode + "  " + btnBack + "\n\n")

			right.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render("  Source Preview:\n"))

			if exp.PreviewError != "" {
				right.WriteString(lipgloss.NewStyle().Foreground(pal.Danger).Render(fmt.Sprintf("    ✖ Error: %s\n", exp.PreviewError)))
			} else if exp.PreviewText != "" {
				previewLines := strings.Split(exp.PreviewText, "\n")
				maxPreviewLines := totalHeight - 9
				if maxPreviewLines < 5 {
					maxPreviewLines = 5
				}
				if len(previewLines) > maxPreviewLines {
					previewLines = previewLines[:maxPreviewLines]
					previewLines = append(previewLines, lipgloss.NewStyle().Foreground(pal.Muted).Render("    ... (truncated) ..."))
				}
				for _, line := range previewLines {
					right.WriteString("  " + line + "\n")
				}
			} else {
				right.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render("    (Empty or binary file)\n"))
			}
		}
	} else {
		right.WriteString("\n  Select a file or directory to preview.\n")
	}

	rightStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Border).
		Width(rightColWidth).
		Height(totalHeight).
		Padding(0, 1).
		Render(right.String())

	return lipgloss.JoinHorizontal(lipgloss.Top, leftStyle, rightStyle)
}

func (m DashboardModel) renderSystemView(pal theme.Palette, totalWidth, totalHeight int) string {
	snap := m.sysSnapshot
	panelWidth := (totalWidth / 2) - 2
	if panelWidth < 38 {
		panelWidth = 38
	}

	// Left Box: CPU, Memory, Disk
	var left strings.Builder
	innerW := panelWidth - 6
	if innerW < 30 {
		innerW = 30
	}

	left.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Primary).Render(" ⚡ CPU Performance\n"))
	barW := max(innerW-22, 12)
	cpuBar := renderProgressBar(snap.CPUPercent, barW, pal.Primary, pal.Border)
	left.WriteString(fmt.Sprintf("   Load : %5.1f%%  %s\n", snap.CPUPercent, cpuBar))
	spark := system.FormatSparkline(snap.CPUHistory, barW)
	left.WriteString(fmt.Sprintf("   Trend: %s\n\n", lipgloss.NewStyle().Foreground(pal.Accent).Bold(true).Render(spark)))

	left.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Primary).Render(" 🧠 Memory (RAM)\n"))
	memBar := renderProgressBar(snap.Memory.UsedPercent, barW, pal.Secondary, pal.Border)
	left.WriteString(fmt.Sprintf("   Usage: %5.1f%%  %s\n", snap.Memory.UsedPercent, memBar))
	left.WriteString(fmt.Sprintf("   Pool : %.1f GB / %.1f GB  ", snap.Memory.UsedGB(), snap.Memory.TotalGB()))
	left.WriteString(lipgloss.NewStyle().Foreground(pal.Success).Bold(true).Render(fmt.Sprintf("(%.1f GB Free)\n\n", snap.Memory.FreeGB())))

	left.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Primary).Render(" 💾 Storage Disks\n"))
	for _, d := range snap.Disks {
		dBar := renderProgressBar(d.UsedPercent, max(barW-6, 10), pal.Highlight, pal.Border)
		left.WriteString(fmt.Sprintf("   [%s] %5.1f%% %s %.0fGB/%.0fGB\n",
			d.DriveLetter, d.UsedPercent, dBar, d.UsedGB(), d.TotalGB()))
	}
	if len(snap.Disks) == 0 {
		left.WriteString("   No fixed disk partitions detected.\n")
	}

	leftStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Border).
		Width(panelWidth).
		Height(totalHeight).
		Padding(0, 1).
		Render(left.String())

	// Right Box: Process Table
	var right strings.Builder
	right.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Primary).
		Render(fmt.Sprintf(" 📋 Active Processes (%d total • %d threads)\n", snap.ProcessCount, snap.ThreadCount)))

	right.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).
		Render(fmt.Sprintf("   %-7s %-7s %-8s %s\n", "PID", "PPID", "THREADS", "EXECUTABLE")))
	right.WriteString(lipgloss.NewStyle().Foreground(pal.Border).
		Render("  " + strings.Repeat("─", panelWidth-6) + "\n"))

	maxProcs := totalHeight - 5
	if maxProcs < 5 {
		maxProcs = 5
	}
	for idx, p := range snap.TopProcesses {
		if idx >= maxProcs {
			break
		}
		pName := trunc(p.Name, panelWidth-28)
		right.WriteString(fmt.Sprintf("   %-7d %-7d %-8d %s\n", p.PID, p.PPID, p.ThreadCount, pName))
	}

	rightStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Border).
		Width(panelWidth).
		Height(totalHeight).
		Padding(0, 1).
		Render(right.String())

	return lipgloss.JoinHorizontal(lipgloss.Top, leftStyle, rightStyle)
}

func (m DashboardModel) renderPluginsView(pal theme.Palette, totalWidth, totalHeight int) string {
	listWidth := (totalWidth / 2) - 2
	if listWidth < 38 {
		listWidth = 38
	}

	// Left Box: Plugin List
	var left strings.Builder
	left.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Primary).
		Render(fmt.Sprintf(" 🔌 Plugins (%d)\n", len(m.plugins))))

	left.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).
		Render(fmt.Sprintf("   %-18s %-10s %s\n", "NAME", "STATUS", "PID")))
	left.WriteString(lipgloss.NewStyle().Foreground(pal.Border).
		Render("  " + strings.Repeat("─", listWidth-6) + "\n"))

	if len(m.plugins) == 0 {
		left.WriteString("\n  No plugins found.\n  Place .py, .js, .bat, or .ps1 in ./plugins or %AppData%\\Dashboard\\plugins\n")
	} else {
		for i, p := range m.plugins {
			statusPill := lipgloss.NewStyle().Foreground(pal.Muted).Render("○ Stopped")
			switch p.Status {
			case plugin.StatusRunning:
				statusPill = lipgloss.NewStyle().Foreground(pal.Success).Bold(true).Render("● Running")
			case plugin.StatusStarting:
				statusPill = lipgloss.NewStyle().Foreground(pal.Warning).Render("◌ Starting")
			case plugin.StatusError:
				statusPill = lipgloss.NewStyle().Foreground(pal.Danger).Render("✖ Error")
			}

			pName := trunc(p.Name, 18)
			pidStr := "-"
			if p.PID > 0 {
				pidStr = fmt.Sprintf("%d", p.PID)
			}

			if i == m.selectedPlugin {
				rowText := fmt.Sprintf(" ▸ %-18s %-10s %s", pName, statusPill, pidStr)
				left.WriteString(lipgloss.NewStyle().
					Background(pal.Secondary).
					Foreground(pal.Foreground).
					Bold(true).
					Render(rowText) + "\n")
			} else {
				left.WriteString(fmt.Sprintf("   %-18s %-10s %s\n", pName, statusPill, pidStr))
			}
		}
	}

	leftStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Border).
		Width(listWidth).
		Height(totalHeight).
		Padding(0, 1).
		Render(left.String())

	// Right Box: Plugin Inspector
	var right strings.Builder
	innerW := listWidth - 6
	if innerW < 30 {
		innerW = 30
	}

	if len(m.plugins) > 0 && m.selectedPlugin < len(m.plugins) {
		p := m.plugins[m.selectedPlugin]
		right.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Primary).
			Render(fmt.Sprintf(" 🔌 %s\n\n", p.Name)))

		right.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).
			Render(" ┌─ Runtime Diagnostics " + strings.Repeat("─", max(innerW-23, 2)) + "┐\n"))
		right.WriteString(fmt.Sprintf(" │  Runtime : %s\n", p.Extension))
		right.WriteString(fmt.Sprintf(" │  Path    : %s\n", truncPath(p.Path, innerW-14)))
		if p.PipePath != "" {
			right.WriteString(fmt.Sprintf(" │  Pipe    : %s\n", trunc(p.PipePath, innerW-14)))
		}
		right.WriteString(fmt.Sprintf(" │  Status  : %s\n", p.Status))
		if p.ErrorMsg != "" {
			right.WriteString(fmt.Sprintf(" │  Error   : %s\n", lipgloss.NewStyle().Foreground(pal.Danger).Render(trunc(p.ErrorMsg, innerW-14))))
		}
		right.WriteString(fmt.Sprintf(" │  Updated : %s\n", p.LastUpdated.Format("15:04:05")))
		right.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).
			Render(" └" + strings.Repeat("─", innerW) + "┘\n\n"))

		btnStart := lipgloss.NewStyle().Bold(true).Background(pal.Secondary).Foreground(pal.Foreground).Padding(0, 1).Render("[s] Start")
		btnStop := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[x] Stop")
		btnReload := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[r] Reload")
		right.WriteString("  " + btnStart + "  " + btnStop + "  " + btnReload + "\n\n")

		right.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Highlight).
			Render("  Rendered Plugin Output:\n"))
		if p.LastRender != "" {
			right.WriteString("  " + p.LastRender + "\n")
		} else {
			right.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).
				Render("  (Plugin idle or waiting for data. Press [s] to launch)\n"))
		}
	} else {
		right.WriteString("\n  Select a plugin from the list to view diagnostics.\n")
	}

	rightStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Border).
		Width(listWidth).
		Height(totalHeight).
		Padding(0, 1).
		Render(right.String())

	return lipgloss.JoinHorizontal(lipgloss.Top, leftStyle, rightStyle)
}

func trunc(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-2] + ".."
}

func truncPath(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 6 {
		return s[:maxLen]
	}
	return "..." + s[len(s)-(maxLen-3):]
}

func renderProgressBar(percent float64, width int, filledColor, emptyColor lipgloss.Color) string {
	if width <= 0 {
		width = 10
	}
	filledCount := int((percent / 100.0) * float64(width))
	if filledCount < 0 {
		filledCount = 0
	}
	if filledCount > width {
		filledCount = width
	}
	emptyCount := width - filledCount

	filledStr := strings.Repeat("█", filledCount)
	emptyStr := strings.Repeat("░", emptyCount)

	return lipgloss.NewStyle().Foreground(filledColor).Render(filledStr) +
		lipgloss.NewStyle().Foreground(emptyColor).Render(emptyStr)
}

func main() {
	// Command-line flag parsing
	tokenFlag := flag.String("token", "", "Authenticate using a GitHub Personal Access Token (PAT)")
	logoutFlag := flag.Bool("logout", false, "Clear saved GitHub authentication token")
	versionFlag := flag.Bool("version", false, "Print dashboard version and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("Terminal Dashboard v%s (Windows Native)\n", version)
		return
	}

	if *logoutFlag {
		if err := auth.DeleteToken(); err != nil {
			fmt.Fprintf(os.Stderr, "Logout error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Successfully logged out from GitHub. Stored token deleted.")
		return
	}

	if *tokenFlag != "" {
		tok, user, err := auth.SaveTokenString(*tokenFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "GitHub authentication failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Successfully authenticated as @%s! Token saved to %s.\n", user, filepath.Join(os.Getenv("APPDATA"), "Dashboard", "github_token.json"))
		_ = tok
		return
	}

	// Initialize Windows terminal features
	if err := term.EnableWindowsVirtualTerminal(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}
	if err := term.EnableMouseInput(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}

	// Initialize config directory
	if _, err := config.EnsureDirectory(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}

	// Start the TUI
	p := tea.NewProgram(initialModel(), tea.WithAltScreen(), tea.WithMouseAllMotion())
	defer p.ReleaseTerminal()

	// Handle SIGINT/SIGTERM for cleanup
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		_ = term.DisableMouseInput()
		p.Kill()
	}()

	// Run the program
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
}
