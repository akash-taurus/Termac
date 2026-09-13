package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

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

// ScanDirectory scans a directory for git repositories
// Returns all found repositories, up to a maximum depth
func ScanDirectory(rootDir string, maxDepth int) ([]LocalRepository, error) {
	var repos []LocalRepository

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Check if this is a git repository
			if isGitRepo(path) {
				_, err := OpenRepository(path)
				if err != nil {
					// Still add it but mark as error
					repos = append(repos, LocalRepository{
						Path:      path,
						HasError:  true,
						WorkDir:   path,
						Message:   err.Error(),
					})
					return nil
				}

				// Get repository info
				info, err := GetRepositoryInfo(path)
				if err != nil {
					repos = append(repos, LocalRepository{
						Path:      path,
						HasError:  true,
						Message:   err.Error(),
					})
					return nil
				}

				repos = append(repos, *info)
			}
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
		switch fileStatus.Worktree {
		case libgit.Unmodified: // Skip unmodified files
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
		}
	}

	if len(changes) == 0 {
		return "Modified", nil
	}
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

// ParseRemoteURL parses a git remote URL and extracts the host and repo name
func ParseRemoteURL(url string) (host, repoName string) {
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
	cmd := exec.Command("git", "init")
	cmd.Dir = dirPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git init failed: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// GitStageAll runs `git add -A` in the repository
func GitStageAll(repoPath string) error {
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
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

	cmd := exec.Command("git", "commit", "-m", message)
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return "", fmt.Errorf("git commit failed: %s", trimmed)
	}
	return trimmed, nil
}

// GitPush pushes current branch to remote
func GitPush(repoPath string) (string, error) {
	cmd := exec.Command("git", "push")
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return "", fmt.Errorf("%s", trimmed)
	}
	if trimmed == "" {
		trimmed = "Everything up-to-date"
	}
	return trimmed, nil
}

// GitPull pulls latest changes from remote
func GitPull(repoPath string) (string, error) {
	cmd := exec.Command("git", "pull")
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return "", fmt.Errorf("%s", trimmed)
	}
	return trimmed, nil
}

// GitFetch fetches changes from remote
func GitFetch(repoPath string) (string, error) {
	cmd := exec.Command("git", "fetch")
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return "", fmt.Errorf("%s", trimmed)
	}
	if trimmed == "" {
		trimmed = "Fetch complete"
	}
	return trimmed, nil
}

// GitDiff returns the git diff output
func GitDiff(repoPath string) (string, error) {
	cmd := exec.Command("git", "diff", "HEAD")
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(out))
	if err != nil || trimmed == "" {
		cmd2 := exec.Command("git", "diff")
		cmd2.Dir = repoPath
		out2, _ := cmd2.CombinedOutput()
		trimmed = strings.TrimSpace(string(out2))
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
	cmd := exec.Command("git", "log", fmt.Sprintf("-n%d", maxCount), "--pretty=format:%h|%an|%cr|%s")
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
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

	cmd := exec.Command("git", "status", "--porcelain=v1", "-b")
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
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
							res.Ahead, _ = strconv.Atoi(strings.TrimPrefix(item, "ahead "))
						} else if strings.HasPrefix(item, "behind ") {
							res.Behind, _ = strconv.Atoi(strings.TrimPrefix(item, "behind "))
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
	checkCmd := exec.Command("git", "remote", "get-url", remoteName)
	checkCmd.Dir = repoPath
	if err := checkCmd.Run(); err == nil {
		setCmd := exec.Command("git", "remote", "set-url", remoteName, remoteURL)
		setCmd.Dir = repoPath
		out, err := setCmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
		}
		return nil
	}

	addCmd := exec.Command("git", "remote", "add", remoteName, remoteURL)
	addCmd.Dir = repoPath
	out, err := addCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// GitBranchName gets the active branch name, defaulting to "main"
func GitBranchName(repoPath string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	branch := strings.TrimSpace(string(out))
	if err != nil || branch == "" || branch == "HEAD" {
		return "main"
	}
	return branch
}

// GitEnsureBranch sets or renames the branch name (e.g. main)
func GitEnsureBranch(repoPath, branchName string) error {
	cmd := exec.Command("git", "branch", "-M", branchName)
	cmd.Dir = repoPath
	_ = cmd.Run()
	return nil
}

// GitPushUpstream pushes a branch to remote and configures tracking
func GitPushUpstream(repoPath, remoteOrURL, branchName string) (string, error) {
	if branchName == "" {
		branchName = GitBranchName(repoPath)
	}
	cmd := exec.Command("git", "push", "-u", remoteOrURL, branchName)
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return trimmed, fmt.Errorf("%s", trimmed)
	}
	return trimmed, nil
}