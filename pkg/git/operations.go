package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// This file extends the local git feature set: per-file staging, discard,
// amend, branch management, checkout/revert/reset, reflog, stash, remote
// management, clone, fetch --prune, batch sync, file history, blame,
// commit-range diffs, hunk staging, and merge/rebase with conflict listing.
// All subprocess calls share runGit's 30s timeout and GIT_TERMINAL_PROMPT=0.

// ---------- Per-file staging / unstaging / discard ----------

// GitStageFile stages a single path (git add -- <path>).
func GitStageFile(repoPath, path string) error {
	out, err := runGit(repoPath, "add", "--", path)
	if err != nil {
		return fmt.Errorf("git add %s failed: %s: %w", path, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// GitUnstageFile unstages a single path, keeping working-tree changes
// (git restore --staged -- <path>; falls back to git rm --cached for
// files that are newly added and not yet in HEAD).
func GitUnstageFile(repoPath, path string) error {
	out, err := runGit(repoPath, "restore", "--staged", "--", path)
	if err != nil {
		// New files known only to the index: restore --staged fails.
		out2, err2 := runGit(repoPath, "rm", "--cached", "--quiet", "--", path)
		if err2 != nil {
			return fmt.Errorf("git unstage %s failed: %s (%s): %w", path,
				strings.TrimSpace(string(out)), strings.TrimSpace(string(out2)), err)
		}
	}
	return nil
}

// GitUnstageAll unstages everything while keeping working-tree changes.
func GitUnstageAll(repoPath string) error {
	out, err := runGit(repoPath, "restore", "--staged", ".")
	if err != nil {
		return fmt.Errorf("git unstage all failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// GitDiscardFile irreversibly discards working-tree changes for one path.
// Untracked files are removed; tracked files are restored from the index.
// The caller MUST confirm before invoking: data loss is not recoverable.
func GitDiscardFile(repoPath, path string) error {
	out, err := runGit(repoPath, "checkout", "--", path)
	if err != nil {
		// Untracked or newly added: remove it from the worktree instead.
		out2, err2 := runGit(repoPath, "clean", "-f", "--", path)
		if err2 != nil {
			return fmt.Errorf("git discard %s failed: %s (%s): %w", path,
				strings.TrimSpace(string(out)), strings.TrimSpace(string(out2)), err)
		}
	}
	return nil
}

// GitAmend amends the last commit, optionally folding currently staged
// changes into it. An empty message keeps the existing one.
func GitAmend(repoPath, message string, includeStaged bool) (string, error) {
	args := []string{"commit", "--amend", "--allow-empty"}
	if strings.TrimSpace(message) != "" {
		args = append(args, "-m", message)
	} else {
		args = append(args, "--no-edit")
	}
	if !includeStaged {
		args = append(args, "--only")
	}
	out, err := runGit(repoPath, args...)
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return trimmed, fmt.Errorf("git amend failed: %s: %w", trimmed, err)
	}
	return trimmed, nil
}

// ---------- Branch management ----------

// BranchInfo describes a local or remote-tracking branch.
type BranchInfo struct {
	Name     string // short name, e.g. "feature/x" or "origin/feature/x"
	IsRemote bool   // true for remotes/origin/... style entries
	IsHead   bool   // current branch
}

// GitListBranches returns local and remote-tracking branches, current first.
//
// Remote detection uses the FULL refname (refs/remotes/...), not a slash in
// the short name: local branches commonly contain slashes (feature/x,
// bugfix/y) and were previously misclassified as remote, which made them
// impossible to check out or delete from the branch overlay.
func GitListBranches(repoPath string) ([]BranchInfo, error) {
	out, err := runGit(repoPath, "branch", "-a", "--format=%(HEAD)%00%(refname)%00%(refname:short)")
	if err != nil {
		return nil, fmt.Errorf("git branch failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	var branches []BranchInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x00", 3)
		if len(parts) != 3 || strings.TrimSpace(parts[2]) == "" {
			continue
		}
		ref := strings.TrimSpace(parts[1])
		b := BranchInfo{Name: strings.TrimSpace(parts[2])}
		b.IsHead = strings.TrimSpace(parts[0]) == "*"
		b.IsRemote = strings.HasPrefix(ref, "refs/remotes/")
		if b.IsRemote && strings.HasSuffix(ref, "/HEAD") {
			continue // origin/HEAD is a symref pointer, not a real branch
		}
		branches = append(branches, b)
	}
	// Current branch first, then alphabetical.
	sort.SliceStable(branches, func(i, j int) bool {
		if branches[i].IsHead != branches[j].IsHead {
			return branches[i].IsHead
		}
		return branches[i].Name < branches[j].Name
	})
	return branches, nil
}

// GitCheckout switches branches (or checks out a commit SHA, leaving HEAD
// detached). force skips local-change verification (checkout -f).
// Note: no "--" here — it would make git treat the target as a pathspec
// instead of a branch/ref.
func GitCheckout(repoPath, target string, force bool) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", fmt.Errorf("checkout target cannot be empty")
	}
	if strings.HasPrefix(target, "-") {
		return "", fmt.Errorf("invalid checkout target %q", target)
	}
	args := []string{"checkout"}
	if force {
		args = append(args, "-f")
	}
	args = append(args, target)
	out, err := runGit(repoPath, args...)
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return trimmed, fmt.Errorf("git checkout %s failed: %s: %w", target, trimmed, err)
	}
	return trimmed, nil
}

// GitCreateBranch creates (and optionally switches to) a new branch.
func GitCreateBranch(repoPath, name string, switchTo bool) error {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, " ~^:?*[]\\") || strings.HasPrefix(name, "-") {
		return fmt.Errorf("invalid branch name %q", name)
	}
	if _, err := runGit(repoPath, "branch", "--", name); err != nil {
		return fmt.Errorf("git branch %s failed: %w", name, err)
	}
	if switchTo {
		if _, err := GitCheckout(repoPath, name, false); err != nil {
			return fmt.Errorf("git checkout %s failed: %w", name, err)
		}
	}
	return nil
}

// GitDeleteBranch deletes a branch; force uses -D instead of -d.
// Refuses to delete the currently checked-out branch.
func GitDeleteBranch(repoPath, name string, force bool) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("branch name cannot be empty")
	}
	if name == GitBranchName(repoPath) {
		return fmt.Errorf("cannot delete the checked-out branch %q", name)
	}
	flag := "-d"
	if force {
		flag = "-D"
	}
	out, err := runGit(repoPath, "branch", flag, "--", name)
	if err != nil {
		return fmt.Errorf("git branch %s %s failed: %s: %w", flag, name, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// GitRevert reverts an existing commit with a generated message.
func GitRevert(repoPath, sha string) (string, error) {
	sha = strings.TrimSpace(sha)
	if sha == "" {
		return "", fmt.Errorf("commit sha cannot be empty")
	}
	out, err := runGit(repoPath, "revert", "--no-edit", "--", sha)
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return trimmed, fmt.Errorf("git revert %s failed: %s: %w", sha, trimmed, err)
	}
	return trimmed, nil
}

// GitResetMode identifies how far a reset rolls state back.
type GitResetMode string

const (
	ResetSoft  GitResetMode = "--soft"  // move HEAD only
	ResetMixed GitResetMode = "--mixed" // default: unstage, keep worktree
	ResetHard  GitResetMode = "--hard"  // destructive
)

// GitReset resets the current branch to a target commit/branch (default
// HEAD). ResetHard discards working-tree changes — confirm first.
//
// Note: no "--" before the target. With a separator git treats everything
// after it as a pathspec, so `git reset --mixed -- HEAD~1` silently reset
// nothing and `--hard`/`--soft` failed with "Cannot do <mode> reset with
// paths". A leading "-" is rejected so the target can never be read as a
// flag.
func GitReset(repoPath, target string, mode GitResetMode) (string, error) {
	if mode == "" {
		mode = ResetMixed
	}
	target = strings.TrimSpace(target)
	if target == "" {
		target = "HEAD"
	}
	if strings.HasPrefix(target, "-") {
		return "", fmt.Errorf("invalid reset target %q", target)
	}
	out, err := runGit(repoPath, "reset", string(mode), target)
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return trimmed, fmt.Errorf("git reset %s %s failed: %s: %w", mode, target, trimmed, err)
	}
	return trimmed, nil
}

// ---------- Reflog ----------

// ReflogItem is one entry of HEAD history.
type ReflogItem struct {
	Index int // 1 = most recent step back
	Hash  string
	Desc  string // e.g. "commit: fix bug"
	Date  string
}

// GitReflog returns the most recent HEAD movements (newest first).
func GitReflog(repoPath string, maxCount int) ([]ReflogItem, error) {
	if maxCount <= 0 {
		maxCount = 20
	}
	out, err := runGit(repoPath, "reflog", "--date=relative", "--max-count", strconv.Itoa(maxCount))
	if err != nil {
		return nil, fmt.Errorf("git reflog failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	var items []ReflogItem
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		item := ReflogItem{}
		// Format: "<hash> HEAD@{<date>}: <desc>" (tab is absent in default
		// reflog output; the selector and description are ": "-separated).
		if i := strings.IndexByte(l, ' '); i > 0 {
			item.Hash = l[:i]
			rest := l[i+1:]
			if strings.HasPrefix(rest, "HEAD@{") {
				if j := strings.Index(rest, "}"); j >= 0 {
					item.Date = rest[len("HEAD@{"):j]
					rest = rest[j+1:]
				}
			}
			item.Desc = strings.TrimSpace(strings.TrimPrefix(rest, ":"))
		} else {
			item.Hash = l
		}
		items = append(items, item)
	}
	// Number oldest→newest as N..1? No: index 1 = newest (first) entry,
	// matching "go back 1 step" intuition.
	for i := range items {
		items[i].Index = i + 1
	}
	return items, nil
}

// ---------- Stash ----------

// StashItem is one stash entry.
type StashItem struct {
	Index int    // 0-based stash@{N}
	Desc  string // e.g. "On main: WIP"
	Hash  string
}

// GitStashPush stashes changes; includeUntracked adds -u. message optional.
func GitStashPush(repoPath, message string, includeUntracked bool) (string, error) {
	args := []string{"stash", "push"}
	if includeUntracked {
		args = append(args, "-u")
	}
	if strings.TrimSpace(message) != "" {
		args = append(args, "-m", message)
	}
	out, err := runGit(repoPath, args...)
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return trimmed, fmt.Errorf("git stash failed: %s: %w", trimmed, err)
	}
	if strings.Contains(trimmed, "No local changes") {
		return trimmed, fmt.Errorf("nothing to stash: no local changes")
	}
	return trimmed, nil
}

// GitStashList enumerates stash entries.
func GitStashList(repoPath string) ([]StashItem, error) {
	out, err := runGit(repoPath, "stash", "list", "--format=%gd%x00%h%x00%gs")
	if err != nil {
		return nil, fmt.Errorf("git stash list failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	var items []StashItem
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l == "" {
			continue
		}
		parts := strings.SplitN(l, "\x00", 3)
		item := StashItem{}
		if len(parts) > 0 {
			item.Index = parseStashIndex(parts[0])
		}
		if len(parts) > 1 {
			item.Hash = parts[1]
		}
		if len(parts) > 2 {
			item.Desc = parts[2]
		}
		items = append(items, item)
	}
	return items, nil
}

func parseStashIndex(ref string) int {
	// refs like "stash@{0}".
	i := strings.IndexByte(ref, '{')
	j := strings.IndexByte(ref, '}')
	if i >= 0 && j > i {
		if n, err := strconv.Atoi(ref[i+1 : j]); err == nil {
			return n
		}
	}
	return 0
}

// GitStashPop applies stash@{index}; keep=true uses "apply" (entry kept).
func GitStashPop(repoPath string, index int, keep bool) (string, error) {
	ref := fmt.Sprintf("stash@{%d}", index)
	verb := "pop"
	if keep {
		verb = "apply"
	}
	out, err := runGit(repoPath, "stash", verb, ref)
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return trimmed, fmt.Errorf("git stash %s %s failed: %s: %w", verb, ref, trimmed, err)
	}
	return trimmed, nil
}

// GitStashDrop removes a stash entry.
func GitStashDrop(repoPath string, index int) (string, error) {
	ref := fmt.Sprintf("stash@{%d}", index)
	out, err := runGit(repoPath, "stash", "drop", ref)
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return trimmed, fmt.Errorf("git stash drop %s failed: %s: %w", ref, trimmed, err)
	}
	return trimmed, nil
}

// ---------- Remotes ----------

// RemoteInfo describes one configured remote.
type RemoteInfo struct {
	Name string
	URL  string
}

// GitListRemotes returns configured remotes and their fetch URLs.
func GitListRemotes(repoPath string) ([]RemoteInfo, error) {
	out, err := runGit(repoPath, "remote", "-v")
	if err != nil {
		return nil, fmt.Errorf("git remote failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	urls := map[string]string{}
	var order []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(l)
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		if _, ok := urls[name]; !ok {
			order = append(order, name)
		}
		if strings.HasSuffix(l, "(fetch)") {
			urls[name] = fields[1]
		} else if _, ok := urls[name]; !ok {
			urls[name] = fields[1]
		}
	}
	remotes := make([]RemoteInfo, 0, len(order))
	for _, name := range order {
		remotes = append(remotes, RemoteInfo{Name: name, URL: urls[name]})
	}
	return remotes, nil
}

// GitRemoveRemote deletes a remote by name.
func GitRemoveRemote(repoPath, name string) error {
	if !validRemoteName(name) {
		return fmt.Errorf("invalid remote name %q", name)
	}
	out, err := runGit(repoPath, "remote", "remove", "--", name)
	if err != nil {
		return fmt.Errorf("git remote remove %s failed: %s: %w", name, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// GitFetchPrune fetches all remotes and drops stale remote-tracking refs.
func GitFetchPrune(repoPath string) (string, error) {
	out, err := runGit(repoPath, "fetch", "--all", "--prune")
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return trimmed, fmt.Errorf("git fetch --all --prune failed: %s: %w", trimmed, err)
	}
	if trimmed == "" {
		trimmed = "Fetch + prune complete"
	}
	return trimmed, nil
}

// ---------- Clone ----------

// GitCloneTimeout allows long-running clones (big repos, slow networks).
const GitCloneTimeout = 10 * time.Minute

// GitClone clones a repository URL into a local directory. dir empty means
// a subdirectory named after the repo inside parentDir.
func GitClone(parentDir, cloneURL, dir string) (string, error) {
	cloneURL = strings.TrimSpace(cloneURL)
	if cloneURL == "" {
		return "", fmt.Errorf("clone URL cannot be empty")
	}
	if dir == "" {
		dir = strings.TrimSuffix(filepath.Base(cloneURL), ".git")
		if dir == "" || dir == "/" || dir == "." {
			return "", fmt.Errorf("could not derive a directory name from %q; specify one", cloneURL)
		}
	}
	target := filepath.Join(parentDir, dir)
	if fi, err := os.Stat(target); err == nil && fi.IsDir() {
		return "", fmt.Errorf("destination %s already exists", target)
	}

	ctx, cancel := context.WithTimeout(context.Background(), GitCloneTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "clone", "--", cloneURL, target)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return trimmed, fmt.Errorf("clone timed out after %s: %s: %w", GitCloneTimeout, trimmed, err)
		}
		return trimmed, fmt.Errorf("git clone failed: %s: %w", trimmed, err)
	}
	return target, nil
}

// ---------- Batch sync ----------

// SyncResult captures one repo's pull outcome in a SyncAll run.
type SyncResult struct {
	Path    string
	Name    string
	Output  string
	Err     error
	Skipped bool // not a git repo / no upstream
}

// GitSyncAll pulls (fast-forward only, never creates merge commits) in
// every git repository directly under rootDir. Repos without an upstream
// are reported as skipped. Failures do not abort the batch.
func GitSyncAll(rootDir string, maxDepth int) []SyncResult {
	repos, err := ScanDirectory(rootDir, maxDepth)
	if err != nil {
		return []SyncResult{{Path: rootDir, Err: err}}
	}
	results := make([]SyncResult, 0, len(repos))
	for _, r := range repos {
		res := SyncResult{Path: r.Path, Name: filepath.Base(r.Path)}
		// ff-only keeps batch syncs safe: divergent repos are reported,
		// never auto-merged.
		out, err := runGit(r.Path, "pull", "--ff-only")
		res.Output = strings.TrimSpace(string(out))
		if err != nil {
			if strings.Contains(res.Output, "no tracking information") ||
				strings.Contains(res.Output, "There is no tracking information") {
				res.Skipped = true
			} else {
				res.Err = err
			}
		}
		results = append(results, res)
	}
	return results
}

// ---------- File history & blame ----------

// FileHistoryItem is one commit touching a specific file.
type FileHistoryItem struct {
	Hash    string
	Author  string
	Date    string
	Subject string
}

// GitFileHistory lists commits that touched a file (rename-aware).
func GitFileHistory(repoPath, path string, maxCount int) ([]FileHistoryItem, error) {
	if maxCount <= 0 {
		maxCount = 20
	}
	out, err := runGit(repoPath, "log", "--follow", fmt.Sprintf("--max-count=%d", maxCount),
		"--pretty=format:%h|%an|%cr|%s", "--", path)
	if err != nil {
		return nil, fmt.Errorf("git log --follow failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	var items []FileHistoryItem
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(l, "|", 4)
		if len(parts) == 4 {
			items = append(items, FileHistoryItem{
				Hash: parts[0], Author: parts[1], Date: parts[2], Subject: parts[3],
			})
		}
	}
	return items, nil
}

// BlameLine is one annotated line of a file.
type BlameLine struct {
	LineNumber int
	Hash       string
	Author     string
	// Text is the line content as git blame prints it.
	Text string
}

// GitBlame blames a file and returns per-line authorship. LineNumber is
// 1-based and matches the file's real line numbers.
func GitBlame(repoPath, path string) ([]BlameLine, error) {
	out, err := runGit(repoPath, "blame", "--porcelain", "--", path)
	if err != nil {
		return nil, fmt.Errorf("git blame failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	type commitMeta struct {
		author string
	}
	metas := map[string]commitMeta{}
	var lines []BlameLine
	num := 0
	var curHash string
	for _, l := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(l, "author "):
			metas[curHash] = commitMeta{author: strings.TrimPrefix(l, "author ")}
		case strings.HasPrefix(l, "\t"):
			num++
			content := strings.TrimPrefix(l, "\t")
			bl := BlameLine{LineNumber: num, Text: content}
			if parts := strings.Fields(curHash); len(parts) > 0 {
				bl.Hash = shortHash(parts[0])
			}
			if m, ok := metas[curHash]; ok {
				bl.Author = m.author
			}
			lines = append(lines, bl)
			curHash = "" // content consumed; next header starts a new entry
		case l == "":
			// separator between entries; nothing to do
		default:
			// First line of an entry: "<sha> <orig-line> <final-line> [num-lines]"
			if !strings.Contains(l, " ") {
				continue
			}
			fields := strings.Fields(l)
			if len(fields[0]) >= 7 && !strings.HasPrefix(fields[0], "author") &&
				!strings.HasPrefix(fields[0], "committer") && !strings.HasPrefix(fields[0], "summary") &&
				!strings.HasPrefix(fields[0], "boundary") && !strings.HasPrefix(fields[0], "previous") &&
				!strings.HasPrefix(fields[0], "filename") {
				curHash = fields[0]
			}
		}
	}
	return lines, nil
}

func shortHash(hash string) string {
	if len(hash) > 8 {
		return hash[:8]
	}
	return hash
}

// ---------- Commit-range diffs ----------

// GitDiffRange diffs two commits (from..to). Either side may be empty,
// meaning HEAD for `to` and HEAD~1 for `from`.
func GitDiffRange(repoPath, from, to string) (string, error) {
	if strings.TrimSpace(to) == "" {
		to = "HEAD"
	}
	if strings.TrimSpace(from) == "" {
		from = to + "~"
	}
	out, err := runGit(repoPath, "diff", "--no-color", from+".."+to)
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return "", fmt.Errorf("git diff %s..%s failed: %s: %w", from, to, trimmed, err)
	}
	if trimmed == "" {
		trimmed = "(No differences)"
	}
	return trimmed, nil
}

// GitDiffCommit shows one commit's changes versus its first parent.
// No "--" here: it would turn the sha into a pathspec and yield an empty
// diff.
func GitDiffCommit(repoPath, sha string) (string, error) {
	sha = strings.TrimSpace(sha)
	if sha == "" {
		return "", fmt.Errorf("commit sha cannot be empty")
	}
	if strings.HasPrefix(sha, "-") {
		return "", fmt.Errorf("invalid commit sha %q", sha)
	}
	out, err := runGit(repoPath, "show", "--no-color", "--format=", sha)
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return "", fmt.Errorf("git show %s failed: %s: %w", sha, trimmed, err)
	}
	if trimmed == "" {
		trimmed = "(No differences)"
	}
	return trimmed, nil
}

// ---------- Hunk staging ----------

// DiffHunk is one @@-delimited block of a unified diff for a file.
type DiffHunk struct {
	Header string // "@@ -12,7 +12,8 @@ context"
	Body   string // full hunk text including header
}

// GitSplitHunks splits `git diff -- <path>` output into per-hunk pieces
// (hunk 0 is the file header + first hunk; later hunks reuse it when
// piped to `git apply --cached`).
func GitSplitHunks(repoPath, path string) ([]DiffHunk, error) {
	out, err := runGit(repoPath, "diff", "--no-color", "--", path)
	if err != nil {
		return nil, fmt.Errorf("git diff for hunks failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return splitDiffHunks(string(out)), nil
}

func splitDiffHunks(diff string) []DiffHunk {
	var header []string
	var hunks []DiffHunk
	var cur *DiffHunk
	for _, l := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "@@"):
			hunks = append(hunks, DiffHunk{Header: l, Body: l})
			cur = &hunks[len(hunks)-1]
		case cur == nil:
			header = append(header, l)
		default:
			cur.Body += "\n" + l
		}
	}
	// Re-attach the file header to each hunk so each can be applied
	// standalone with `git apply --cached`.
	var fileHeader string
	if len(header) > 0 {
		fileHeader = strings.Join(header, "\n") + "\n"
	}
	for i := range hunks {
		hunks[i].Body = fileHeader + hunks[i].Body + "\n"
	}
	return hunks
}

// GitStageHunk stages a single hunk by piping it to `git apply --cached`.
// The hunk must include the file header (as GitSplitHunks produces).
func GitStageHunk(repoPath, path string, hunkIndex int) error {
	hunks, err := GitSplitHunks(repoPath, path)
	if err != nil {
		return err
	}
	if hunkIndex < 0 || hunkIndex >= len(hunks) {
		return fmt.Errorf("hunk %d out of range (%d hunks in %s)", hunkIndex, len(hunks), path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "apply", "--cached", "--unidiff-zero", "-")
	cmd.Dir = repoPath
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.Stdin = strings.NewReader(hunks[hunkIndex].Body)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git apply --cached failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// ---------- Merge / rebase ----------

// GitMerge merges a branch into the current one. ffOnly refuses merge
// commits; noFF always creates one.
func GitMerge(repoPath, branch string, ffOnly, noFF bool) (string, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "", fmt.Errorf("branch to merge cannot be empty")
	}
	args := []string{"merge"}
	if ffOnly {
		args = append(args, "--ff-only")
	}
	if noFF {
		args = append(args, "--no-ff")
	}
	args = append(args, "--", branch)
	out, err := runGit(repoPath, args...)
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return trimmed, fmt.Errorf("git merge %s failed: %s: %w", branch, trimmed, err)
	}
	return trimmed, nil
}

// GitRebase rebases the current branch onto another.
func GitRebase(repoPath, upstream string) (string, error) {
	upstream = strings.TrimSpace(upstream)
	if upstream == "" {
		return "", fmt.Errorf("rebase upstream cannot be empty")
	}
	out, err := runGit(repoPath, "rebase", "--", upstream)
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return trimmed, fmt.Errorf("git rebase %s failed: %s: %w", upstream, trimmed, err)
	}
	return trimmed, nil
}

// GitAbortCurrent aborts an in-progress merge or rebase. kind is "merge"
// or "rebase".
func GitAbortCurrent(repoPath, kind string) (string, error) {
	switch kind {
	case "merge", "rebase":
	default:
		return "", fmt.Errorf("unknown abort kind %q (want merge or rebase)", kind)
	}
	out, err := runGit(repoPath, kind, "--abort")
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return trimmed, fmt.Errorf("git %s --abort failed: %s: %w", kind, trimmed, err)
	}
	return trimmed, nil
}

// ConflictFile describes one path with unresolved conflicts.
type ConflictFile struct {
	Path   string
	Stages string // raw two-char status, e.g. "UU", "AA"
}

// GitConflictFiles lists unmerged paths (e.g. after a failed merge/rebase).
// Parses `git ls-files -u` (unmerged index entries, one per stage) and
// de-duplicates by path. Empty slice means no conflicts.
func GitConflictFiles(repoPath string) ([]ConflictFile, error) {
	out, err := runGit(repoPath, "ls-files", "--unmerged", "-z")
	if err != nil {
		return nil, fmt.Errorf("git ls-files --unmerged failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	var conflicts []ConflictFile
	seen := map[string]bool{}
	for _, entry := range strings.Split(string(out), "\x00") {
		if strings.TrimSpace(entry) == "" {
			continue
		}
		// Entry format: "<mode> <sha> <stage>\t<path>".
		tab := strings.IndexByte(entry, '\t')
		if tab < 0 || tab == len(entry)-1 {
			continue
		}
		path := entry[tab+1:]
		if !seen[path] {
			conflicts = append(conflicts, ConflictFile{Path: path, Stages: "UU"})
			seen[path] = true
		}
	}
	return conflicts, nil
}

// GitHasConflicts reports whether the repo currently has unmerged paths.
func GitHasConflicts(repoPath string) bool {
	conflicts, err := GitConflictFiles(repoPath)
	return err == nil && len(conflicts) > 0
}
