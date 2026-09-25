package git

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	libgit "github.com/go-git/go-git/v5"
)

// LocalRepository represents a local git repository
type LocalRepository struct {
	Path     string
	Remote   string
	Branch   string
	WorkDir  string
	HEAD     string
	Message  string
	HasError bool
}

// gitTimeout bounds every git subprocess so a credential prompt can never hang the UI.
const gitTimeout = 30 * time.Second

func runGit(repoPath string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoPath
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	return cmd.CombinedOutput()
}

// ScanDirectory scans a directory for git repositories
// Returns all found repositories, up to a maximum depth
func ScanDirectory(rootDir string, maxDepth int) ([]LocalRepository, error) {
	if maxDepth <= 0 {
		maxDepth = 3
	}
	baseDepth := strings.Count(filepath.Clean(rootDir), string(filepath.Separator))
	var repos []LocalRepository

	err := filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			return nil
		}
		depth := strings.Count(filepath.Clean(path), string(filepath.Separator)) - baseDepth
		if depth > maxDepth {
			return filepath.SkipDir
		}
		name := d.Name()
		// Skip heavy/irrelevant dirs.
		if name == ".git" || name == "node_modules" || name == ".hg" || name == ".svn" {
			return filepath.SkipDir
		}
		// Check if this is a git repository
		if isGitRepo(path) {
			info, err := GetRepositoryInfo(path)
			if err != nil {
				repos = append(repos, LocalRepository{
					Path:     path,
					HasError: true,
					WorkDir:  path,
					Message:  err.Error(),
				})
			} else {
				repos = append(repos, *info)
			}
			return filepath.SkipDir
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan directory %s: %w", rootDir, err)
	}

	return repos, nil
}

// isGitRepo checks if a directory is a git repository
func isGitRepo(path string) bool {
	gitDir := filepath.Join(path, ".git")
	_, err := os.Stat(gitDir)
	return err == nil
}

// OpenRepository opens a git repository at the given path
func OpenRepository(path string) (*libgit.Repository, error) {
	return libgit.PlainOpen(path)
}

// GetCurrentBranch returns the current branch name
func GetCurrentBranch(repo *libgit.Repository) (string, error) {
	ref, err := repo.Head()
	if err != nil {
		return "", err
	}
	return ref.Name().Short(), nil
}

// GetCommitMessage gets the last commit message
func GetCommitMessage(repo *libgit.Repository) (string, error) {
	ref, err := repo.Head()
	if err != nil {
		return "", err
	}
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return "", err
	}
	return commit.Message, nil
}

// GetLastCommit gets the last commit author and message
func GetLastCommit(repo *libgit.Repository) (author string, message string, err error) {
	ref, err := repo.Head()
	if err != nil {
		return "", "", err
	}
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return "", "", err
	}
	return commit.Author.Name, commit.Message, nil
}

// GetStatus returns the working tree status
func GetStatus(repo *libgit.Repository) (string, error) {
	w, err := repo.Worktree()
	if err != nil {
		return "", err
	}
	status, err := w.Status()
	if err != nil {
		return "", err
	}

	if status.IsClean() {
		return "Clean", nil
	}

	// Check whether there are any commits yet
	if _, err := repo.Head(); err != nil {
		return "No commits yet", nil
	}

	var changes []string
	for path, fileStatus := range status {
		// Consider staged changes too: a file staged but untouched in worktree
		// reports Worktree=Unmodified with Staging set.
		wt := fileStatus.Worktree
		st := fileStatus.Staging
		switch wt {
		case libgit.Unmodified:
			if st == libgit.Unmodified {
				continue
			}
			changes = append(changes, fmt.Sprintf("%s: Staged", path))
		case libgit.Untracked:
			changes = append(changes, fmt.Sprintf("%s: Untracked", path))
		case libgit.Modified:
			changes = append(changes, fmt.Sprintf("%s: Modified", path))
		case libgit.Added:
			changes = append(changes, fmt.Sprintf("%s: Added", path))
		case libgit.Deleted:
			changes = append(changes, fmt.Sprintf("%s: Deleted", path))
		case libgit.Renamed:
			changes = append(changes, fmt.Sprintf("%s: Renamed", path))
		case libgit.Copied:
			changes = append(changes, fmt.Sprintf("%s: Copied", path))
		default:
			if st != libgit.Unmodified {
				changes = append(changes, fmt.Sprintf("%s: Staged", path))
			}
		}
	}

	if len(changes) == 0 {
		return "Modified", nil
	}
	sort.Strings(changes)
	return strings.Join(changes, "; "), nil
}

// GetDiffSummary gets a summary of uncommitted changes
func GetDiffSummary(repo *libgit.Repository) (string, error) {
	w, err := repo.Worktree()
	if err != nil {
		return "", err
	}
	status, err := w.Status()
	if err != nil {
		return "", err
	}
	if !status.IsClean() {
		return "Has uncommitted changes", nil
	}
	return "Clean", nil
}

// ParseRemoteURL parses a git remote URL and extracts the host and repo name.
// Credentials (user:token@) are stripped so they never appear in the UI.
func ParseRemoteURL(raw string) (host, repoName string) {
	trimmed := strings.TrimSpace(raw)
	// Strip credentials via net/url when possible.
	if u, err := url.Parse(trimmed); err == nil && u.Host != "" {
		host = u.Host
		repoName = strings.TrimSuffix(strings.TrimPrefix(u.Path, "/"), ".git")
		return host, repoName
	}
	url := trimmed
	// Remove "git@" prefix (SSH format)
	if strings.HasPrefix(url, "git@") {
		url = strings.TrimPrefix(url, "git@")
		// Handle SSH format: git@host:repo.git
		parts := strings.SplitN(url, ":", 2)
		if len(parts) == 2 {
			host = parts[0]
			repoName = strings.TrimSuffix(parts[1], ".git")
		}
		return
	}

	// Handle HTTPS/HTTP format
	if strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://") {
		url = strings.TrimPrefix(url, "https://")
		url = strings.TrimPrefix(url, "http://")
		parts := strings.Split(url, "/")
		if len(parts) >= 2 {
			host = parts[0]
			repoName = strings.TrimSuffix(strings.Join(parts[1:], "/"), ".git")
		}
		return
	}

	// Handle GitHub CLI format
	if strings.HasPrefix(url, "github.com/") {
		url = strings.TrimPrefix(url, "github.com/")
		parts := strings.SplitN(url, "/", 2)
		if len(parts) == 2 {
			host = "github.com"
			repoName = strings.TrimSuffix(parts[1], ".git")
		}
		return
	}

	return
}

// GetRemoteHost returns the git remote hosting service
func GetRemoteHost(repoPath string) (string, error) {
	repo, err := OpenRepository(repoPath)
	if err != nil {
		return "", err
	}

	r, err := repo.Remote("origin")
	if err != nil {
		return "", err
	}

	config := r.Config()
	if len(config.URLs) > 0 {
		host, _ := ParseRemoteURL(config.URLs[0])
		if host == "" {
			return "local", nil
		}
		return host, nil
	}

	return "local", nil
}

// GetRepositoryInfo extracts repository metadata from a local git repo
func GetRepositoryInfo(repoPath string) (*LocalRepository, error) {
	repo, err := OpenRepository(repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open repository: %w", err)
	}

	// Get current branch
	branch, err := GetCurrentBranch(repo)
	if err != nil {
		branch = "unknown"
	}

	// Get last commit message
	message, err := GetCommitMessage(repo)
	if err != nil {
		message = "unknown"
	}

	// Get status
	status, err := GetStatus(repo)
	if err != nil {
		status = "unknown"
	}
	_ = status // status is used to determine if the repo has changes

	// Get remote host
	host, err := GetRemoteHost(repoPath)
	if err != nil {
		host = "unknown"
	}

	return &LocalRepository{
		Path:     repoPath,
		Remote:   host,
		Branch:   branch,
		Message:  message,
		HasError: false,
		WorkDir:  repoPath,
		HEAD:     branch,
	}, nil
}

// GetFileStatusMap returns a map of relative file path -> status flag (e.g. "M", "A", "?", "D")
func GetFileStatusMap(repoPath string) (map[string]string, error) {
	repo, err := OpenRepository(repoPath)
	if err != nil {
		return nil, err
	}
	w, err := repo.Worktree()
	if err != nil {
		return nil, err
	}
	status, err := w.Status()
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for path, fileStatus := range status {
		var flag string
		switch fileStatus.Worktree {
		case libgit.Untracked:
			flag = "?"
		case libgit.Modified:
			flag = "M"
		case libgit.Added:
			flag = "A"
		case libgit.Deleted:
			flag = "D"
		case libgit.Renamed:
			flag = "R"
		case libgit.Copied:
			flag = "C"
		default:
			switch fileStatus.Staging {
			case libgit.Modified:
				flag = "M"
			case libgit.Added:
				flag = "A"
			case libgit.Deleted:
				flag = "D"
			}
		}
		if flag != "" {
			result[filepath.ToSlash(path)] = flag
		}
	}
	return result, nil
}

// IsGitRepository checks if a directory contains a .git repository
func IsGitRepository(path string) bool {
	return isGitRepo(path)
}

// GitInit initializes a new git repository in the given directory
func GitInit(dirPath string) error {
	out, err := runGit(dirPath, "init")
	if err != nil {
		return fmt.Errorf("git init failed: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// GitStageAll runs `git add -A` in the repository
func GitStageAll(repoPath string) error {
	out, err := runGit(repoPath, "add", "-A")
	if err != nil {
		return fmt.Errorf("git add failed: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// GitCommit creates a commit with the specified message
func GitCommit(repoPath string, message string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", fmt.Errorf("commit message cannot be empty")
	}

	out, err := runGit(repoPath, "commit", "-m", message)
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return "", fmt.Errorf("git commit failed: %s: %w", trimmed, err)
	}
	return trimmed, nil
}

// GitPush pushes current branch to remote
func GitPush(repoPath string) (string, error) {
	out, err := runGit(repoPath, "push")
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return "", fmt.Errorf("git push failed: %s: %w", trimmed, err)
	}
	if trimmed == "" {
		trimmed = "Everything up-to-date"
	}
	return trimmed, nil
}

// GitPull pulls latest changes from remote
func GitPull(repoPath string) (string, error) {
	out, err := runGit(repoPath, "pull")
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return "", fmt.Errorf("git pull failed: %s: %w", trimmed, err)
	}
	return trimmed, nil
}

// GitFetch fetches changes from remote
func GitFetch(repoPath string) (string, error) {
	out, err := runGit(repoPath, "fetch")
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return "", fmt.Errorf("git fetch failed: %s: %w", trimmed, err)
	}
	if trimmed == "" {
		trimmed = "Fetch complete"
	}
	return trimmed, nil
}

// GitDiff returns the git diff output
func GitDiff(repoPath string) (string, error) {
	if !isGitRepo(repoPath) {
		return "", fmt.Errorf("not a git repository: %s", repoPath)
	}
	out, err := runGit(repoPath, "diff", "--no-color", "HEAD", "--")
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		// git diff HEAD fails with non-zero when no HEAD yet; try plain diff.
		out2, err2 := runGit(repoPath, "diff", "--no-color", "--")
		if err2 != nil {
			return "", fmt.Errorf("git diff failed: %w", err2)
		}
		trimmed = strings.TrimSpace(string(out2))
	}
	if len(trimmed) > 200000 {
		trimmed = trimmed[:200000] + "\n... (truncated: diff too large) ..."
	}
	if trimmed == "" {
		trimmed = "(No uncommitted changes to display)"
	}
	return trimmed, nil
}

// CommitLogItem represents a parsed commit entry
type CommitLogItem struct {
	Hash    string
	Author  string
	Date    string
	Subject string
}

// GitLog retrieves recent commits
func GitLog(repoPath string, maxCount int) ([]CommitLogItem, error) {
	if maxCount <= 0 {
		maxCount = 10
	}
	if maxCount > 100 {
		maxCount = 100
	}
	out, err := runGit(repoPath, "log", fmt.Sprintf("-n%d", maxCount), "--pretty=format:%h|%an|%cr|%s")
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var items []CommitLogItem
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		parts := strings.SplitN(l, "|", 4)
		if len(parts) == 4 {
			items = append(items, CommitLogItem{
				Hash:    parts[0],
				Author:  parts[1],
				Date:    parts[2],
				Subject: parts[3],
			})
		}
	}
	return items, nil
}

// FileStatusItem represents an individual changed/untracked file
type FileStatusItem struct {
	Path   string
	Status string // "M", "A", "?", "D", "R"
	Staged bool
}

// DetailedGitStatus contains full breakdown of working directory status
type DetailedGitStatus struct {
	IsGitRepo      bool
	Branch         string
	Upstream       string
	Ahead          int
	Behind         int
	StagedCount    int
	UnstagedCount  int
	UntrackedCount int
	IsClean        bool
	Files          []FileStatusItem
}

// GitStatusDetailed parses git status --porcelain=v1 -b
func GitStatusDetailed(repoPath string) (*DetailedGitStatus, error) {
	if !isGitRepo(repoPath) {
		return &DetailedGitStatus{IsGitRepo: false}, nil
	}

	out, err := runGit(repoPath, "status", "--porcelain=v1", "-b")
	if err != nil {
		return nil, fmt.Errorf("git status error: %s (%w)", strings.TrimSpace(string(out)), err)
	}

	res := &DetailedGitStatus{
		IsGitRepo: true,
		Branch:    "HEAD",
		IsClean:   true,
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}

		if strings.HasPrefix(line, "##") {
			header := strings.TrimPrefix(line, "##")
			header = strings.TrimSpace(header)
			if strings.Contains(header, "...") {
				parts := strings.SplitN(header, "...", 2)
				res.Branch = parts[0]
				rest := parts[1]
				if idx := strings.Index(rest, " ["); idx != -1 {
					res.Upstream = rest[:idx]
					bracket := rest[idx+2:]
					bracket = strings.TrimSuffix(bracket, "]")
					for _, item := range strings.Split(bracket, ",") {
						item = strings.TrimSpace(item)
						if strings.HasPrefix(item, "ahead ") {
							if n, err := strconv.Atoi(strings.TrimPrefix(item, "ahead ")); err == nil {
								res.Ahead = n
							}
						} else if strings.HasPrefix(item, "behind ") {
							if n, err := strconv.Atoi(strings.TrimPrefix(item, "behind ")); err == nil {
								res.Behind = n
							}
						}
					}
				} else {
					res.Upstream = rest
				}
			} else {
				words := strings.Fields(header)
				if len(words) > 0 {
					res.Branch = words[len(words)-1]
				}
			}
			continue
		}

		res.IsClean = false
		if len(line) >= 3 {
			stagedChar := line[0]
			worktreeChar := line[1]
			filePath := strings.TrimSpace(line[3:])
			// Strip quotes git adds for paths with spaces/specials.
			filePath = strings.Trim(filePath, `"`)
			// Rename format: "old -> new"; track the new path.
			if idx := strings.Index(filePath, " -> "); idx >= 0 {
				filePath = strings.TrimSpace(filePath[idx+4:])
				filePath = strings.Trim(filePath, `"`)
			}

			if stagedChar == '?' && worktreeChar == '?' {
				res.UntrackedCount++
				res.Files = append(res.Files, FileStatusItem{Path: filePath, Status: "?", Staged: false})
			} else {
				if stagedChar != ' ' && stagedChar != '?' {
					res.StagedCount++
					res.Files = append(res.Files, FileStatusItem{Path: filePath, Status: string(stagedChar), Staged: true})
				}
				if worktreeChar != ' ' && worktreeChar != '?' {
					res.UnstagedCount++
					res.Files = append(res.Files, FileStatusItem{Path: filePath, Status: string(worktreeChar), Staged: false})
				}
			}
		}
	}

	return res, nil
}

// GitSetRemote adds or updates a git remote
func GitSetRemote(repoPath, remoteName, remoteURL string) error {
	if !validRemoteName(remoteName) {
		return fmt.Errorf("invalid remote name %q", remoteName)
	}
	out, err := runGit(repoPath, "remote", "get-url", "--", remoteName)
	if err == nil {
		_ = out
		setOut, err := runGit(repoPath, "remote", "set-url", remoteName, remoteURL)
		if err != nil {
			return fmt.Errorf("%s: %w", strings.TrimSpace(string(setOut)), err)
		}
		return nil
	}

	addOut, err := runGit(repoPath, "remote", "add", remoteName, remoteURL)
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(addOut)), err)
	}
	return nil
}

func validRemoteName(s string) bool {
	if s == "" || len(s) > 100 {
		return false
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}

// GitBranchName gets the active branch name, defaulting to "main"
func GitBranchName(repoPath string) string {
	out, err := runGit(repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	branch := strings.TrimSpace(string(out))
	if err != nil || branch == "" || branch == "HEAD" {
		return "main"
	}
	return branch
}

// GitEnsureBranch sets or renames the branch name (e.g. main)
func GitEnsureBranch(repoPath, branchName string) error {
	branchName = strings.TrimSpace(branchName)
	if branchName == "" || strings.ContainsAny(branchName, " ~^:?*[]\\") || strings.HasPrefix(branchName, "-") {
		return fmt.Errorf("invalid branch name %q", branchName)
	}
	out, err := runGit(repoPath, "branch", "-M", branchName)
	if err != nil {
		return fmt.Errorf("git branch failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// GitPushUpstream pushes a branch to remote and configures tracking.
// remoteOrURL must be a remote NAME (e.g. "origin"); token-embedded URLs are
// rejected to avoid leaking secrets into .git/config and process listings.
// Use GitPushUpstreamAuth for authenticated pushes.
func GitPushUpstream(repoPath, remoteOrURL, branchName string) (string, error) {
	if strings.Contains(remoteOrURL, "@") && strings.Contains(remoteOrURL, "://") {
		return "", fmt.Errorf("refusing to push with credentialed URL; use GitPushUpstreamAuth")
	}
	if branchName == "" {
		branchName = GitBranchName(repoPath)
	}
	out, err := runGit(repoPath, "push", "-u", "--", remoteOrURL, branchName)
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return trimmed, fmt.Errorf("git push failed: %s: %w", trimmed, err)
	}
	return trimmed, nil
}

// GitPushUpstreamAuth pushes using an Authorization header so the token never
// appears in argv (.ps visible) or .git/config (persisted upstream). The
// remote URL on disk stays clean; only this invocation carries the secret.
func GitPushUpstreamAuth(repoPath, remoteName, branchName, token string) (string, error) {
	if branchName == "" {
		branchName = GitBranchName(repoPath)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return GitPushUpstream(repoPath, remoteName, branchName)
	}
	header := "Authorization: Bearer " + token
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-c", "http.extraHeader="+header, "push", "-u", "--", remoteName, branchName)
	cmd.Dir = repoPath
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return trimmed, fmt.Errorf("git push failed: %s: %w", trimmed, err)
	}
	return trimmed, nil
}

// GitHasCommits checks whether the repository has at least one commit (HEAD resolves)
func GitHasCommits(repoPath string) bool {
	out, err := runGit(repoPath, "rev-parse", "--verify", "HEAD")
	_ = out
	return err == nil
}

// gitAuthFailureMarkers are case-insensitive substrings typical of git
// remote authentication/authorization rejections (HTTPS credential or
// token problems, revoked PATs, missing permissions).
var gitAuthFailureMarkers = []string{
	"authentication failed",
	"invalid username",
	"invalid credentials",
	"could not read username",
	"could not read password",
	"permission denied",
	"remote: permission",
	"access denied",
	"account suspended",
	"token expired",
	"token has expired",
	"invalid token",
	"bad credentials",
	"logon failed",
	"returned error: 401",
	"returned error: 403",
	"error: 401",
	"error: 403",
	"http 401",
	"http 403",
	"status 401",
	"status 403",
}

// IsGitAuthFailure reports whether combined git command output looks like a
// remote authentication/authorization rejection rather than e.g. a conflict,
// missing branch, or network error.
func IsGitAuthFailure(output string) bool {
	lowered := strings.ToLower(output)
	for _, marker := range gitAuthFailureMarkers {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}

// EmbedTokenInHTTPSURL returns rawURL with an x-access-token credential
// embedded for HTTPS remotes. Non-HTTPS URLs (SSH, local paths) and empty
// tokens are returned unchanged.
// Deprecated: embedding tokens leaks into ps and .git/config. Prefer
// GitPushUpstreamAuth which uses http.extraHeader.
func EmbedTokenInHTTPSURL(rawURL, token string) string {
	if token == "" {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return rawURL
	}
	u.User = url.UserPassword("x-access-token", token)
	return u.String()
}

// GetRemoteURL returns the configured URL for a git remote (e.g. "origin").
func GetRemoteURL(repoPath, remoteName string) (string, error) {
	if !validRemoteName(remoteName) {
		return "", fmt.Errorf("invalid remote name %q", remoteName)
	}
	out, err := runGit(repoPath, "remote", "get-url", "--", remoteName)
	if err != nil {
		return "", fmt.Errorf("no remote %q: %s", remoteName, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
