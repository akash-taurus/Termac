package main

// Update implements the Bubble Tea update loop: modal key handling, global
// shortcuts, and every tea.Msg case.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"tui/pkg/auth"
	"tui/pkg/explorer"
	"tui/pkg/git"
	"tui/pkg/shell"
	"tui/pkg/term"
	"tui/pkg/theme"
)

func (m DashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	// Default to info each cycle so a message assigned without an explicit
	// level never inherits a previous message's severity.
	m.statusLevel = statusInfo

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
			// Bracketed pastes arrive as a single KeyMsg: send them straight
			// to the input so no character is mistaken for a shortcut.
			if msg.Paste {
				m.tokenInput, cmd = m.tokenInput.Update(msg)
				return m, cmd
			}
			switch msg.String() {
			case "esc":
				if m.authCancel != nil {
					m.authCancel()
					m.authCancel = nil
				}
				m.authModalOpen = false
				m.authPolling = false
				m.deviceCode = nil
				m.authError = ""
				m.authNeedsPAT = false
				m.tokenInput.Blur()
				// Publish was waiting on this modal: continue with the
				// current session token instead of dropping the action.
				if m.pendingPublish {
					m.pendingPublish = false
					if m.token != nil && m.selected >= 0 && m.selected < len(m.repos) {
						return m, m.openPublishModal(fmt.Sprintf("Publishing local folder %s to GitHub...", m.repos[m.selected].Name))
					}
				}
				return m, nil

			case "enter":
				val := strings.TrimSpace(m.tokenInput.Value())
				if val != "" {
					m.setStatus(statusLoading, "Verifying GitHub token...")
					m.authError = ""
					return m, githubSubmitTokenCommand(val)
				}
				return m, nil

			case "ctrl+o", "ctrl+b":
				_ = openBrowserURL("https://github.com/settings/tokens/new?scopes=repo,read:org&description=TerminalDashboard")
				m.message = "Opened browser to create Personal Access Token (PAT)..."
				return m, nil

			// Modal actions use Ctrl chords so printable letters always go to
			// the token field. A token can contain b/c/d/x; plain-letter
			// shortcuts made those characters untypable as the first character.
			case "ctrl+d":
				// Initiate Device Flow — refused when a PAT is required,
				// otherwise the user loops: device login can never create repos.
				if m.authNeedsPAT {
					m.authError = "Device-flow login cannot create repositories. Press [Ctrl+O] to open the browser token page (scope: repo), paste the classic PAT above, then press [Enter]."
					m.setStatus(statusWarn, "Personal Access Token (PAT) with 'repo' scope required — device flow cannot create repositories")
					return m, nil
				}
				m.setStatus(statusLoading, "Requesting GitHub device authorization code...")
				m.authError = ""
				return m, githubStartDeviceFlowCommand()

			case "ctrl+e":
				// Check environment first so a freshly exported PAT always
				// wins over a stale stored token. Paste works reliably in
				// PowerShell itself, unlike inside the TUI input where the
				// terminal may swallow the paste chord (Ctrl+V).
				if envTok := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); envTok != "" {
					m.setStatus(statusLoading, "Verifying GITHUB_TOKEN from environment...")
					return m, githubSubmitTokenCommand(envTok)
				}
				if envTok := strings.TrimSpace(os.Getenv("GH_TOKEN")); envTok != "" {
					m.setStatus(statusLoading, "Verifying GH_TOKEN from environment...")
					return m, githubSubmitTokenCommand(envTok)
				}
				// Fall back to validating whatever is already stored.
				if tok, err := auth.LoadToken(); err == nil && strings.TrimSpace(tok.AccessToken) != "" {
					m.setStatus(statusLoading, "Verifying stored token...")
					return m, githubSubmitTokenCommand(strings.TrimSpace(tok.AccessToken))
				}
				m.authError = "No GITHUB_TOKEN or GH_TOKEN found in environment"
				return m, nil

			case "ctrl+x":
				// Clear stored token — helps when a stale device-flow token lingers.
				if m.token != nil && strings.TrimSpace(*m.token) != "" {
					_ = auth.DeleteToken()
					m.token = nil
					m.userName = ""
					m.authError = ""
					m.setStatus(statusSuccess, "Stored token cleared. Paste a new PAT above and press [Enter].")
				} else {
					m.setStatus(statusWarn, "No stored token to clear.")
				}
				return m, nil
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
				m.setStatus(statusLoading, "Opening Windows GUI File Explorer folder dialog...")
				return m, pickFolderCmd()

			case "enter":
				val := strings.TrimSpace(m.folderInput.Value())
				if val == "" {
					val = "."
				}
				absPath, err := filepath.Abs(val)
				if err != nil {
					m.setStatusf(statusError, "Invalid path: %v", err)
					return m, nil
				}
				fi, err := os.Stat(absPath)
				if err != nil || !fi.IsDir() {
					m.setStatusf(statusError, "Directory does not exist: %s", absPath)
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

				m.setStatusf(statusSuccess, "Opened folder: %s", absPath)
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
					m.setStatus(statusWarn, "Commit message cannot be empty")
					return m, nil
				}
				m.commitModalOpen = false
				m.commitInput.Blur()
				if m.selected >= 0 && m.selected < len(m.repos) {
					repoPath := m.repos[m.selected].Path
					m.setStatusf(statusLoading, "Committing changes to %s...", m.repos[m.selected].Name)
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

			case "ctrl+l":
				m.publishModalOpen = false
				m.publishNameInput.Blur()
				m.authModalOpen = true
				m.tokenInput.Focus()
				return m, nil

			case "tab", "ctrl+p":
				m.publishPrivate = !m.publishPrivate
				return m, nil

			case "enter":
				val := strings.TrimSpace(m.publishNameInput.Value())
				if val == "" {
					m.setStatus(statusWarn, "Repository name cannot be empty")
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
					m.setStatusf(statusLoading, "Creating GitHub repo '%s' and pushing...", val)
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
					m.setStatusf(statusError, "Error opening item: %v", err)
				} else if isDir {
					m.setStatusf(statusSuccess, "Entered directory: %s", m.activeExplorer.RelativeCurrentPath())
				} else {
					if m.activeExplorer.Selected >= 0 && m.activeExplorer.Selected < len(m.activeExplorer.Entries) {
						m.message = fmt.Sprintf("Previewing file: %s", m.activeExplorer.Entries[m.activeExplorer.Selected].Name)
					}
				}
				return m, nil

			case "backspace", "left", "h", "H":
				if m.activeExplorer.GoUp() {
					m.setStatusf(statusSuccess, "Navigated up to: %s", m.activeExplorer.RelativeCurrentPath())
				} else {
					m.setStatus(statusWarn, "Already at repository root")
				}
				return m, nil

			case "e", "E":
				if err := m.activeExplorer.OpenInFileExplorer(); err != nil {
					m.setStatusf(statusError, "Failed to launch Windows Explorer: %v", err)
				} else {
					m.setStatusf(statusSuccess, "Opened in Windows File Explorer: %s", m.activeExplorer.CurrentDir)
				}
				return m, nil

			case "p", "P":
				if err := m.activeExplorer.OpenInTerminal(); err != nil {
					m.setStatusf(statusError, "Failed to launch Terminal: %v", err)
				} else {
					m.setStatusf(statusSuccess, "Opened Terminal at: %s", m.activeExplorer.CurrentDir)
				}
				return m, nil

			case "v", "V":
				if err := m.activeExplorer.OpenInVSCode(); err != nil {
					m.setStatusf(statusError, "Failed to launch VS Code: %v", err)
				} else {
					m.setStatusf(statusSuccess, "Launched VS Code: %s", m.activeExplorer.CurrentDir)
				}
				return m, nil

			case "r", "R":
				if err := m.activeExplorer.Refresh(); err != nil {
					m.setStatusf(statusError, "Refresh error: %v", err)
				} else {
					m.setStatusf(statusSuccess, "Refreshed contents of %s", m.activeExplorer.RelativeCurrentPath())
				}
				return m, nil

			case "t", "T":
				m.themeIndex = (m.themeIndex + 1) % len(theme.AvailableThemes)
				newTheme := theme.GetThemeByIndex(m.themeIndex)
				m.setStatusf(statusSuccess, "Switched theme to %s", newTheme.Name)
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
				m.setStatus(statusLoading, "Opening Windows GUI File Explorer folder dialog...")
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
						m.setStatusf(statusError, "Stage error: %v", err)
					} else {
						m.setStatusf(statusSuccess, "Staged all changes in %s (git add -A)", repo.Name)
						return m, repoDetailCommand(context.Background(), repo)
					}
				} else {
					m.setStatus(statusWarn, "Not a Git repository. Press [i] to initialize git.")
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
					m.setStatus(statusWarn, "Not a Git repository. Press [i] to initialize git.")
				}
				return m, nil
			}

		case "n", "N":
			if m.viewMode == ViewLocal && m.selected >= 0 && m.selected < len(m.repos) {
				if m.token == nil || !isPAT(*m.token) {
					if m.token == nil {
						m.setStatus(statusWarn, "GitHub authentication required to create and push repositories. Please login with [l].")
					} else {
						m.setStatus(statusWarn, "A Personal Access Token (PAT) with 'repo' scope is recommended to create repositories. Paste it below, or press [Esc] to continue with the current session token.")
					}
					m.authModalOpen = true
					m.authNeedsPAT = true
					m.pendingPublish = true
					m.tokenInput.Focus()
					return m, textinput.Blink
				}
				return m, m.openPublishModal(fmt.Sprintf("Publishing local folder %s to GitHub...", m.repos[m.selected].Name))
			}

		case "P":
			if m.viewMode == ViewLocal && m.selected >= 0 && m.selected < len(m.repos) {
				repo := m.repos[m.selected]
				if !git.IsGitRepository(repo.Path) || repo.Remote == "" || repo.Remote == "local" {
					if m.token == nil || !isPAT(*m.token) {
						if m.token == nil {
							m.setStatus(statusWarn, "No remote configured. Please authenticate with GitHub [l] to create a new remote repo.")
						} else {
							m.setStatus(statusWarn, "A Personal Access Token (PAT) with 'repo' scope is recommended to create repositories. Paste it below, or press [Esc] to continue with the current session token.")
						}
						m.authModalOpen = true
						m.authNeedsPAT = true
						m.pendingPublish = true
						m.tokenInput.Focus()
						return m, textinput.Blink
					}
					return m, m.openPublishModal("No remote repository configured. Set repository details to create and push:")
				}
				m.setStatusf(statusLoading, "Pushing commits for %s to remote...", repo.Name)
				pushToken := ""
				if m.token != nil {
					pushToken = *m.token
				}
				return m, gitPushCmd(repo.Path, pushToken)
			}

		case "F":
			if m.viewMode == ViewLocal && m.selected >= 0 && m.selected < len(m.repos) {
				repo := m.repos[m.selected]
				if git.IsGitRepository(repo.Path) {
					m.setStatusf(statusLoading, "Pulling latest changes for %s...", repo.Name)
					return m, gitPullCmd(repo.Path)
				} else {
					m.setStatus(statusWarn, "Not a Git repository.")
				}
				return m, nil
			}

		case "i", "I":
			if m.viewMode == ViewLocal && m.selected >= 0 && m.selected < len(m.repos) {
				repo := m.repos[m.selected]
				if !git.IsGitRepository(repo.Path) {
					if err := git.GitInit(repo.Path); err != nil {
						m.setStatusf(statusError, "Git init error: %v", err)
					} else {
						m.setStatusf(statusSuccess, "Initialized Git repository in %s!", repo.Path)
						return m, repoDetailCommand(context.Background(), repo)
					}
				} else {
					m.setStatus(statusWarn, "Already a Git repository.")
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
					m.setStatus(statusWarn, "Not a Git repository.")
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
					m.setStatus(statusWarn, "Not a Git repository.")
				}
				return m, nil
			}

		case "f":
			if m.selected >= 0 && m.selected < len(m.repos) {
				targetPath := m.repos[m.selected].Path
				if fi, err := os.Stat(targetPath); err == nil && fi.IsDir() {
					exp, err := explorer.NewExplorer(targetPath)
					if err != nil {
						m.setStatusf(statusError, "Explorer error: %v", err)
					} else {
						m.activeExplorer = exp
						m.explorerMode = true
						m.message = fmt.Sprintf("Exploring %s (%s) — Press [Esc] to exit", m.repos[m.selected].Name, exp.RelativeCurrentPath())
					}
				} else {
					m.setStatusf(statusError, "Repository path does not exist on disk: %s", targetPath)
				}
				return m, nil
			}

		case "e", "E":
			if m.selected >= 0 && m.selected < len(m.repos) {
				targetPath := m.repos[m.selected].Path
				if fi, err := os.Stat(targetPath); err == nil && fi.IsDir() {
					_ = exec.Command("explorer.exe", targetPath).Start()
					m.setStatusf(statusSuccess, "Opened in Windows File Explorer: %s", targetPath)
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
						// Do not route through cmd.exe: it re-parses metacharacters in
						// the path. Pass it straight to PowerShell as a single argv
						// entry, escaping single quotes for -LiteralPath.
						safe := strings.ReplaceAll(targetPath, "'", "''")
						_ = exec.Command("powershell.exe", "-NoExit", "-Command", fmt.Sprintf("Set-Location -LiteralPath '%s'", safe)).Start()
					}
					m.setStatusf(statusSuccess, "Opened Terminal at: %s", targetPath)
				}
				return m, nil
			}

		// Login / logout work from any view (previously GitHub-tab only,
		// so pressing [l] on the Local tab silently did nothing).
		case "l", "L":
			m.authModalOpen = true
			m.tokenInput.Focus()
			m.tokenInput.Reset()
			m.authError = ""
			m.deviceCode = nil
			return m, textinput.Blink

		case "u", "U":
			_ = auth.DeleteToken()
			m.token = nil
			m.userName = ""
			m.authNeedsPAT = false
			m.pendingPublish = false
			m.githubRepos = nil
			m.githubSelected = 0
			if m.viewMode == ViewGitHub {
				m.repos = nil
				m.selected = 0
			}
			m.setStatus(statusSuccess, "Logged out from GitHub (token deleted).")
			return m, nil

		case "t", "T":
			m.themeIndex = (m.themeIndex + 1) % len(theme.AvailableThemes)
			newTheme := theme.GetThemeByIndex(m.themeIndex)
			m.setStatusf(statusSuccess, "Switched theme to %s", newTheme.Name)
			return m, nil

		case "w", "W":
			exePath, err := os.Executable()
			if err == nil {
				iconPath := filepath.Join(filepath.Dir(exePath), "assets", "icon.ico")
				err = shell.RegisterTerminalProfile(exePath, iconPath)
				if err == nil {
					m.setStatus(statusSuccess, "Successfully added Terminal Dashboard profile to Windows Terminal!")
				} else {
					m.setStatusf(statusWarn, "Windows Terminal profile notice: %v", err)
				}
			}
			return m, nil

		// All view changes go through switchView so every path stashes the
		// outgoing list and restores (or clears) the incoming one identically.
		case "tab":
			cmd = m.switchView(ViewMode((int(m.viewMode) + 1) % 4))
			return m, cmd

		case "shift+tab":
			cmd = m.switchView(ViewMode((int(m.viewMode) + 3) % 4))
			return m, cmd

		case "1":
			cmd = m.switchView(ViewLocal)
			return m, cmd

		case "2":
			cmd = m.switchView(ViewGitHub)
			return m, cmd

		case "3":
			cmd = m.switchView(ViewSystem)
			return m, cmd

		case "4":
			cmd = m.switchView(ViewPlugins)
			return m, cmd

		case "r", "R":
			if m.viewMode == ViewLocal {
				if !m.scanning {
					m.scanning = true
					m.setStatus(statusLoading, "Scanning local repositories...")
					return m, scanCommand(context.Background())
				}
			} else if m.viewMode == ViewGitHub {
				if m.token != nil {
					m.setStatus(statusLoading, "Refreshing GitHub repositories...")
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
				m.setStatus(statusSuccess, "System metrics refreshed.")
				return m, nil
			} else if m.viewMode == ViewPlugins {
				if m.selectedPlugin >= 0 && m.selectedPlugin < len(m.plugins) {
					p := m.plugins[m.selectedPlugin]
					m.setStatusf(statusLoading, "Reloading plugin %s...", p.Name)
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
				m.setStatusf(statusLoading, "Starting plugin %s over Named Pipe...", p.Name)
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
				m.setStatusf(statusLoading, "Stopping plugin %s...", p.Name)
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
					m.setStatusf(statusLoading, "Loading details for %s...", m.repos[m.selected].Name)
					return m, repoDetailCommand(context.Background(), m.repos[m.selected])
				}
			}
			return m, nil
		}

	case githubTokenSubmitMsg:
		if m.authCancel != nil {
			m.authCancel()
			m.authCancel = nil
		}
		if msg.Success {
			m.token = msg.Token
			m.userName = msg.User
			m.authModalOpen = false
			m.authPolling = false
			// Verify token was actually persisted by reloading from disk
			if savedTok, err := auth.LoadToken(); err == nil && savedTok.AccessToken != "" {
				if savedTok.AccessToken != *msg.Token {
					m.authError = "Token save failed: disk has different token"
					m.setStatus(statusError, "Authentication error: token not persisted correctly")
					return m, nil
				}
				m.token = &savedTok.AccessToken
			}
			needsPAT := m.authNeedsPAT
			if msg.Token == nil || !strings.HasPrefix(*msg.Token, "ghu_") {
				m.authNeedsPAT = false
			}
			m.pendingPublish = false
			m.tokenInput.Blur()
			m.tokenInput.Reset()
			tokenType := describeTokenType(*m.token)
			if needsPAT {
				if m.selected >= 0 && m.selected < len(m.repos) {
					return m, m.openPublishModal(fmt.Sprintf("Authenticated as @%s (%s)! Confirm repository name to create & push...", msg.User, tokenType))
				}
				m.setStatusf(statusSuccess, "Authenticated as @%s (%s)! Select a local repo to publish.", msg.User, tokenType)
				return m, nil
			}
			m.setStatusf(statusSuccess, "Successfully authenticated as @%s (%s)! Fetching repositories...", msg.User, tokenType)
			m.viewMode = ViewGitHub
			return m, githubReposCommand(context.Background(), msg.Token)
		}
		m.authError = msg.Err.Error()
		m.setStatusf(statusError, "Authentication error: %v", msg.Err)
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
		m.setStatusf(statusSuccess, "Code %s copied to clipboard! Browser opened to %s", msg.Response.UserCode, msg.Response.VerificationURL)
		pollCtx, cancel := context.WithCancel(context.Background())
		m.authCancel = cancel
		return m, githubPollDeviceTokenCommand(pollCtx, msg.Response)

	case pluginActionMsg:
		if msg.Err != nil {
			m.setStatusf(statusError, "Plugin error (%s): %v", msg.ID, msg.Err)
		} else {
			m.setStatusf(statusSuccess, "Plugin %s active over Named Pipe!", msg.ID)
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
		details := []RepoDetail{}
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
		m.localRepos = details
		m.localSelected = 0
		// Only swap into view when the Local tab is active; a scan
		// finishing while the user is elsewhere must not clobber it.
		if m.viewMode == ViewLocal {
			m.repos = details
			m.selected = 0
		}
		m.setStatusf(statusSuccess, "Found %d local repository(s)", len(details))
		if len(details) > 0 && m.viewMode == ViewLocal {
			return m, repoDetailCommand(context.Background(), details[0])
		}
		return m, nil

	case scanErrorMsg:
		m.scanning = false
		m.setStatusf(statusError, "Scan error: %v", msg.Err)
		return m, nil

	case githubLoginMsg:
		if msg.Success {
			m.token = msg.Token
			m.userName = msg.User
			m.setStatusf(statusSuccess, "GitHub authenticated as @%s! Fetching repositories...", msg.User)
			return m, githubReposCommand(context.Background(), msg.Token)
		}
		// If no token exists, open auth modal so user can enter PAT
		m.setStatus(statusWarn, "GitHub token required. Please login using a Personal Access Token (PAT).")
		m.authModalOpen = true
		m.tokenInput.Focus()
		return m, textinput.Blink

	case githubReposMsg:
		if msg.Err != nil {
			m.setStatusf(statusError, "GitHub error: %v (press [l] to re-authenticate)", msg.Err)
			return m, nil
		}
		m.userName = msg.User
		details := []RepoDetail{}
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
		m.githubRepos = details
		m.githubSelected = 0
		// Only swap into view when the GitHub tab is active; otherwise
		// the fetch was triggered from the publish flow and must not
		// clobber the local list.
		if m.viewMode == ViewGitHub {
			m.repos = details
			m.selected = 0
		}
		m.setStatusf(statusSuccess, "Loaded %d GitHub repository(s) for @%s", len(details), msg.User)
		return m, nil

	case repoDetailMsg:
		// Detail requests are fired on every navigation, so responses can
		// arrive out of order. Match by path so a stale response can never
		// overwrite a different repo's row with another repo's branch/status.
		idx := -1
		for i := range m.repos {
			if m.repos[i].Path == msg.Detail.Path {
				idx = i
				break
			}
		}
		if idx < 0 {
			return m, nil // repo left the list (rescan/switch); drop stale result
		}
		m.repos[idx] = msg.Detail
		// Only the selected repo's detail feeds the shared status/log panes.
		if idx == m.selected {
			m.detailedGitStatus = msg.GitStatus
			m.gitLogItems = msg.GitLogs
			if m.message == "" || strings.HasPrefix(m.message, "Fetching") || strings.HasPrefix(m.message, "Loading") || strings.HasPrefix(m.message, "Updated:") {
				m.message = fmt.Sprintf("Updated: %s", msg.Detail.Name)
			}
		}
		return m, nil

	case gitActionMsg:
		if msg.Err != nil {
			m.setStatusf(statusError, "Git %s failed: %v", msg.Action, msg.Err)
			// Auth-shaped push/pull failures with a token hint should
			// route to re-login, mirroring the publish error path below.
			if (msg.Action == "push" || msg.Action == "pull") && (strings.Contains(msg.Err.Error(), "GitHub token") || strings.Contains(msg.Err.Error(), "[l]")) {
				m.authError = msg.Err.Error()
				m.authModalOpen = true
				m.tokenInput.Focus()
			}
		} else {
			if strings.TrimSpace(msg.Output) != "" {
				firstLine := strings.Split(strings.TrimSpace(msg.Output), "\n")[0]
				m.setStatusf(statusSuccess, "Git %s: %s", msg.Action, firstLine)
			} else {
				m.setStatusf(statusSuccess, "Git %s succeeded!", msg.Action)
			}
		}
		if m.selected >= 0 && m.selected < len(m.repos) {
			return m, repoDetailCommand(context.Background(), m.repos[m.selected])
		}
		return m, nil

	case guiFolderPickMsg:
		if msg.Err != nil {
			m.setStatusf(statusError, "GUI folder picker error: %v", msg.Err)
			return m, nil
		}
		if strings.TrimSpace(msg.Path) == "" {
			m.setStatus(statusWarn, "GUI folder selection canceled.")
			return m, nil
		}

		absPath, err := filepath.Abs(strings.TrimSpace(msg.Path))
		if err != nil {
			m.setStatusf(statusError, "Invalid selected path: %v", err)
			return m, nil
		}
		fi, err := os.Stat(absPath)
		if err != nil || !fi.IsDir() {
			m.setStatusf(statusError, "Directory does not exist: %s", absPath)
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

		m.setStatusf(statusSuccess, "Navigated to folder via GUI Explorer: %s", absPath)
		if m.selected >= 0 && m.selected < len(m.repos) {
			return m, repoDetailCommand(context.Background(), m.repos[m.selected])
		}
		return m, nil

	case publishResultMsg:
		if msg.Err != nil {
			m.setStatusf(statusError, "Publish error: %v", msg.Err)
			if strings.Contains(msg.Err.Error(), "token") || strings.Contains(msg.Err.Error(), "Personal Access Token") || strings.Contains(msg.Err.Error(), "PAT") || strings.Contains(msg.Err.Error(), "scope") {
				m.authError = msg.Err.Error()
				m.authNeedsPAT = true
				m.pendingPublish = true
				m.authModalOpen = true
				m.tokenInput.Focus()
			}
			return m, nil
		} else {
			m.pendingPublish = false
			m.setStatusf(statusSuccess, "🎉 Successfully created & pushed to GitHub: %s! (%s)", msg.Repo.FullName, msg.Repo.Branch)
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

// setStatus records a status message together with its explicit severity.
