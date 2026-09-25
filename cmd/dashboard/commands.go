package main

// Async tea.Cmd constructors and their message types: system polling,
// repository scanning, GitHub auth/login/device flow, git actions, publish
// flow, folder picking, and browser opening.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"tui/pkg/auth"
	"tui/pkg/git"
	"tui/pkg/github"
	"tui/pkg/shell"
)

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

// githubPollDeviceTokenCommand waits for user confirmation in browser.
// ctx cancels polling when the user closes the auth modal.
func githubPollDeviceTokenCommand(ctx context.Context, code *auth.DeviceCodeResponse) tea.Cmd {
	return func() tea.Msg {
		tok, err := auth.PollForDeviceTokenWithContext(ctx, "", code)
		if err != nil {
			// Cancelled by the user: the modal is already closed, so report
			// nothing instead of a spurious authentication error.
			if errors.Is(err, context.Canceled) {
				return nil
			}
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
		repos, err := client.GetRepositoriesAll(500)
		if err != nil && len(repos) == 0 {
			return githubReposMsg{Err: err}
		}
		// Surface a partial result: a later-page failure still shows what loaded.
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

func gitPushCmd(repoPath, token string) tea.Cmd {
	return func() tea.Msg {
		out, err := git.GitPush(repoPath)
		if err == nil {
			return gitActionMsg{Action: "push", Repo: repoPath, Output: out}
		}
		if !git.IsGitAuthFailure(out + "\n" + err.Error()) {
			return gitActionMsg{Action: "push", Repo: repoPath, Output: out, Err: err}
		}
		// The remote rejected our credentials. Plain `git push` uses
		// git's own credential store, so first check the dashboard's
		// stored token: if it is missing or dead, say so plainly
		// instead of surfacing raw git stderr.
		if strings.TrimSpace(token) == "" {
			return gitActionMsg{Action: "push", Repo: repoPath, Output: out, Err: fmt.Errorf("remote rejected git credentials and no GitHub token is stored (press [l] to log in, or configure git credentials for this remote): %v", err)}
		}
		if valid, verr := auth.ValidateToken(&auth.Token{AccessToken: token}); verr != nil {
			if !errors.Is(verr, auth.ErrRateLimited) {
				return gitActionMsg{Action: "push", Repo: repoPath, Output: out, Err: fmt.Errorf("remote rejected git credentials and the stored GitHub token could not be verified (%v). Check your connection and retry", verr)}
			}
			// Rate-limited: validity unknown, proceed to retry with stored token.
		} else if !valid {
			return gitActionMsg{Action: "push", Repo: repoPath, Output: out, Err: fmt.Errorf("remote rejected git credentials and the stored GitHub token is invalid or expired (press [l] to provide a fresh Personal Access Token): %v", err)}
		}
		// Stored token is good: retry once over HTTPS with header auth
		// (SSH remotes cannot use it, so guide instead of retrying).
		remoteURL, rerr := git.GetRemoteURL(repoPath, "origin")
		if rerr != nil {
			return gitActionMsg{Action: "push", Repo: repoPath, Output: out, Err: fmt.Errorf("push rejected and remote origin is unreadable (%v): %v", rerr, err)}
		}
		if !strings.HasPrefix(remoteURL, "https://") {
			return gitActionMsg{Action: "push", Repo: repoPath, Output: out, Err: fmt.Errorf("push rejected over non-HTTPS remote %q (stored GitHub token cannot be applied; check SSH keys or remote permissions): %v", remoteURL, err)}
		}
		branch := git.GitBranchName(repoPath)
		pushOut, pushErr := git.GitPushUpstreamAuth(repoPath, "origin", branch, token)
		if pushErr != nil {
			return gitActionMsg{Action: "push", Repo: repoPath, Output: pushOut, Err: fmt.Errorf("push failed even with a valid GitHub token (check collaborator access on this repo): %v", pushErr)}
		}
		if strings.TrimSpace(pushOut) == "" {
			pushOut = "Everything up-to-date"
		}
		return gitActionMsg{Action: "push", Repo: repoPath, Output: pushOut}
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

func openBrowserURL(targetURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", targetURL)
	case "darwin":
		cmd = exec.Command("open", targetURL)
	default:
		cmd = exec.Command("xdg-open", targetURL)
	}
	return cmd.Start()
}

type publishResultMsg struct {
	Repo   *github.Repo
	Output string
	Err    error
}

func publishAndPushCmd(token, userName, repoPath, repoName, description string, isPrivate bool) tea.Cmd {
	return func() tea.Msg {
		if token == "" {
			return publishResultMsg{Err: fmt.Errorf("GitHub authentication required. Please press [l] to provide a Personal Access Token (PAT)")}
		}
		// NOTE: no prefix ban on ghu_/gho_ here. OAuth/device-flow tokens
		// with 'repo' scope can create repos; scope is checked below when
		// the API exposes it, and POST /user/repos is attempted otherwise.
		// A real scope failure surfaces from the API and routes back to
		// PAT login via publishResultMsg handling.
		// Live-validate before touching the local repo: a revoked,
		// expired, or under-scoped token must fail here — not after git
		// init/commit/branch mutations and a cryptic API error. A plain
		// GET /user check is not enough: it returns 200 even for tokens
		// without repo-creation permission, so classic PAT scopes are
		// inspected via the X-OAuth-Scopes header as well.
		// Only block when scopes are provably insufficient (non-empty list
		// missing the needed scope). An empty/unknown list means the API
		// did not expose scopes — never block on that; POST /user/repos
		// is the ground truth and its error paths guide to PAT login.
		scopes, scopesPresent, serr := auth.GetTokenScopes(&auth.Token{AccessToken: token})
		if serr != nil {
			if strings.Contains(serr.Error(), "invalid or revoked") || strings.Contains(serr.Error(), "expired") {
				return publishResultMsg{Err: fmt.Errorf("stored GitHub token is invalid or expired (%v). Please press [l] to provide a fresh Personal Access Token (PAT) with 'repo' scope", serr)}
			}
			return publishResultMsg{Err: fmt.Errorf("could not verify GitHub authentication (%v). Check your connection and try again", serr)}
		}
		if scopesPresent && len(scopes) > 0 && !auth.HasRepoCreateScope(scopes, isPrivate) {
			granted := strings.Join(scopes, ", ")
			need := "'repo'"
			if !isPrivate {
				need = "'repo' (or 'public_repo' for a public repository)"
			}
			return publishResultMsg{Err: fmt.Errorf("stored GitHub token lacks the required scope to create this repository (granted: %s; needs %s). Please press [l] to provide a classic Personal Access Token with the %s scope", granted, need, need)}
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
			setName := exec.Command("git", "config", "user.name", authorName)
			setName.Dir = repoPath
			_ = setName.Run()
			setEmail := exec.Command("git", "config", "user.email", authorName+"@users.noreply.github.com")
			setEmail.Dir = repoPath
			_ = setEmail.Run()
		}

		// 3. Ensure repository has at least one commit before pushing
		if !git.GitHasCommits(repoPath) {
			entries, _ := os.ReadDir(repoPath)
			hasFiles := false
			for _, e := range entries {
				if e.Name() != ".git" {
					hasFiles = true
					break
				}
			}
			if !hasFiles {
				readmePath := filepath.Join(repoPath, "README.md")
				_ = os.WriteFile(readmePath, []byte(fmt.Sprintf("# %s\n\nCreated with Terminal Dashboard.\n", repoName)), 0644)
			}
			_ = git.GitStageAll(repoPath)
			_, _ = git.GitCommit(repoPath, fmt.Sprintf("Initial commit for %s", repoName))
		} else {
			// Commit any pending uncommitted/untracked changes
			status, err := git.GitStatusDetailed(repoPath)
			if err == nil && (!status.IsClean || len(status.Files) > 0) {
				_ = git.GitStageAll(repoPath)
				_, _ = git.GitCommit(repoPath, fmt.Sprintf("Update files for %s", repoName))
			}
		}

		_ = git.GitEnsureBranch(repoPath, "main")

		// 4. Create repository on GitHub via API (or connect if already exists)
		client := github.NewClient(&token)
		ghRepo, err := client.CreateRepository(github.CreateRepoRequest{
			Name:        repoName,
			Description: description,
			Private:     isPrivate,
			AutoInit:    false,
		})
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "already exists") {
				if existing, getErr := client.GetRepository(userName, repoName); getErr == nil {
					ghRepo = existing
					err = nil
				}
			}
			if err != nil {
				// Name only the token TYPE (prefix), never the secret, so
				// the user can tell which stored credential was used.
				return publishResultMsg{Err: fmt.Errorf("GitHub repository creation failed (active token: %s): %w", describeTokenType(token), err)}
			}
		}

		// 5. Add or set remote origin
		if err := git.GitSetRemote(repoPath, "origin", ghRepo.CloneURL); err != nil {
			return publishResultMsg{Err: fmt.Errorf("failed to configure remote origin: %w", err)}
		}

		// 6. Push local commits to remote with authentication.
		// Use header-based auth so the token never lands in argv or .git/config.
		branch := git.GitBranchName(repoPath)
		if branch == "" {
			branch = "main"
		}

		out, err := git.GitPushUpstreamAuth(repoPath, "origin", branch, token)

		if err != nil {
			return publishResultMsg{
				Repo: ghRepo,
				Err:  fmt.Errorf("connected repository %s, but push failed: %s (%v)", ghRepo.FullName, out, err),
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
