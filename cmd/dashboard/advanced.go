package main

// TUI support for the advanced git features: per-file staging, stash,
// branches, checkout/revert/reset, reflog, clone, sync-all, merge/rebase,
// hunk staging, and GitHub PR/issue actions.
//
// Design:
//   - One generic confirm modal + one generic prompt modal serve every
//     destructive/parameterized action, instead of one modal per feature.
//   - Overlay panes (stash list, branch list, reflog, file history, blame,
//     issues) reuse a shared overlayStack with cursor + content + title.
//   - Every long-running operation runs in a tea.Cmd and reports through a
//     dedicated message carrying an explicit status level.

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"tui/pkg/git"
	"tui/pkg/github"
)

// ---------- Model extensions ----------

// confirmAction is what the generic confirm modal will run on "yes".
type confirmAction struct {
	title    string // shown in the modal
	kind     string // discriminator: "discard-file", "reset-hard", "revert", "stash-drop", "delete-branch", "checkout-force", "abort", "pr-close", "pr-merge", "restore-file"
	repoPath string
	arg      string // path, branch, sha, ...
	arg2     string
}

// promptAction is what the generic input modal will run on Enter.
type promptAction struct {
	kind       string // "branch-create", "branch-delete", "stash-push", "clone", "merge", "rebase", "remote-add"
	title      string
	repoPath   string
	defaultVal string
}

// overlayKind enumerates scrollable overlay panes.
type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayStashList
	overlayBranchList
	overlayReflog
	overlayFileHistory
	overlayBlame
	overlayIssues
	overlayConflicts
)

// overlayPane is one open overlay with a cursor over its lines.
type overlayPane struct {
	kind   overlayKind
	title  string
	lines  []string // pre-rendered content lines
	cursor int
	// path is the file whose hunks are loaded in hunk mode. It is recorded
	// when the hunks load so staging always targets that file, even if the
	// detail-pane file cursor moved while hunk mode was open.
	path string
	// items carry selectable payloads for actionable overlays.
	branches  []git.BranchInfo
	stashes   []git.StashItem
	issues    []github.Issue
	conflicts []git.ConflictFile
	hunks     []git.DiffHunk
}

// These fields are embedded in DashboardModel (declared in model.go via the
// block below) and cover all advanced-feature state.
type advancedModelFields struct {
	// per-file staging / hunk staging
	hunkMode   bool // diff pane routes keys to hunk staging
	hunkCursor int
	fileCursor int // selected row in the detail file list
	// overlays
	overlay *overlayPane
	// generic modals
	confirm     *confirmAction
	prompt      *promptAction
	promptInput textinput.Model
	// sync batch
	syncRunning bool
	syncResults []git.SyncResult
	syncOverlay bool
	// clone flow
	cloning bool
}

// advancedFields returns a zero value for embedding.
func advancedFields() advancedModelFields {
	return advancedModelFields{}
}

// ---------- Message types ----------

type gitFileMsg struct {
	Action string // "stage", "unstage", "unstage-all", "discard"
	Path   string
	Repo   string
	Err    error
}

type stashMsg struct {
	Action string // "push", "pop", "drop"
	Output string
	Err    error
	Repo   string
}

type branchMsg struct {
	Action string // "list", "create", "delete", "checkout"
	Output string
	Err    error
	Repo   string
}

type historyActionMsg struct {
	Action string // "revert", "reset-soft", "reset-mixed", "reset-hard", "checkout-sha", "reflog"
	Output string
	Err    error
	Repo   string
}

type mergeRebaseMsg struct {
	Action    string // "merge", "rebase", "abort-merge", "abort-rebase"
	Output    string
	Conflicts []git.ConflictFile
	Err       error
	Repo      string
}

type hunkMsg struct {
	Path  string
	Index int
	Err   error
}

type cloneMsg struct {
	URL string
	Dir string
	Err error
}

type syncProgressMsg struct {
	Done    int
	Total   int
	Current string
}

type syncDoneMsg struct {
	Results []git.SyncResult
	Err     error
}

type prActionMsg struct {
	Number int
	Action string
	Output string
	Err    error
}

type issuesMsg struct {
	Issues []github.Issue
	Err    error
	Repo   string
}

type overlayDataMsg struct {
	Kind     overlayKind
	Title    string
	Lines    []string
	Branches []git.BranchInfo
	Stashes  []git.StashItem
	Hunks    []git.DiffHunk
	// Path is set for hunk-loading responses so hunk mode knows which file
	// the hunks belong to.
	Path string
	Err  error
}

// ---------- Commands ----------

func gitFileCmd(action, repoPath, path string) tea.Cmd {
	return func() tea.Msg {
		var err error
		switch action {
		case "stage":
			err = git.GitStageFile(repoPath, path)
		case "unstage":
			err = git.GitUnstageFile(repoPath, path)
		case "unstage-all":
			err = git.GitUnstageAll(repoPath)
		case "discard":
			err = git.GitDiscardFile(repoPath, path)
		default:
			err = fmt.Errorf("unknown file action %q", action)
		}
		return gitFileMsg{Action: action, Path: path, Repo: repoPath, Err: err}
	}
}

func stashCmd(action, repoPath string, index int, message string, includeUntracked bool) tea.Cmd {
	return func() tea.Msg {
		var out string
		var err error
		switch action {
		case "push":
			out, err = git.GitStashPush(repoPath, message, includeUntracked)
		case "pop":
			out, err = git.GitStashPop(repoPath, index, false)
		case "apply":
			out, err = git.GitStashPop(repoPath, index, true)
		case "drop":
			out, err = git.GitStashDrop(repoPath, index)
		default:
			err = fmt.Errorf("unknown stash action %q", action)
		}
		return stashMsg{Action: action, Output: out, Err: err, Repo: repoPath}
	}
}

func branchListCmd(repoPath string) tea.Cmd {
	return func() tea.Msg {
		branches, err := git.GitListBranches(repoPath)
		if err != nil {
			return branchMsg{Action: "list", Err: err, Repo: repoPath}
		}
		lines := make([]string, 0, len(branches))
		for _, b := range branches {
			marker := "  "
			if b.IsHead {
				marker = "▶ "
			}
			tag := ""
			if b.IsRemote {
				tag = " (remote)"
			}
			lines = append(lines, fmt.Sprintf("%s%s%s", marker, b.Name, tag))
		}
		return overlayDataMsg{Kind: overlayBranchList, Title: "Branches — Enter: switch · n: new · d: delete · Esc: close",
			Lines: lines, Branches: branches}
	}
}

func branchCmd(action, repoPath, name string, switchTo bool) tea.Cmd {
	return func() tea.Msg {
		var out string
		var err error
		switch action {
		case "create":
			err = git.GitCreateBranch(repoPath, name, switchTo)
			out = "created " + name
		case "delete":
			err = git.GitDeleteBranch(repoPath, name, true)
			out = "deleted " + name
		case "checkout":
			out, err = git.GitCheckout(repoPath, name, false)
		default:
			err = fmt.Errorf("unknown branch action %q", action)
		}
		return branchMsg{Action: action, Output: out, Err: err, Repo: repoPath}
	}
}

func historyActionCmd(action, repoPath, sha string, mode git.GitResetMode) tea.Cmd {
	return func() tea.Msg {
		var out string
		var err error
		switch action {
		case "revert":
			out, err = git.GitRevert(repoPath, sha)
		case "reset-soft":
			out, err = git.GitReset(repoPath, sha, git.ResetSoft)
		case "reset-mixed":
			out, err = git.GitReset(repoPath, sha, git.ResetMixed)
		case "reset-hard":
			out, err = git.GitReset(repoPath, sha, git.ResetHard)
		case "checkout-sha":
			out, err = git.GitCheckout(repoPath, sha, false)
		default:
			err = fmt.Errorf("unknown history action %q", action)
		}
		return historyActionMsg{Action: action, Output: out, Err: err, Repo: repoPath}
	}
}

func reflogCmd(repoPath string) tea.Cmd {
	return func() tea.Msg {
		items, err := git.GitReflog(repoPath, 30)
		if err != nil {
			return overlayDataMsg{Kind: overlayReflog, Err: err}
		}
		lines := make([]string, 0, len(items))
		for _, it := range items {
			lines = append(lines, fmt.Sprintf("%2d  %s  %-14s %s", it.Index, it.Hash, truncateStr(it.Date, 14), truncateStr(it.Desc, 44)))
		}
		return overlayDataMsg{Kind: overlayReflog, Title: "Reflog — Enter/R: checkout this state · Shift+R: hard reset here · Esc: close", Lines: lines}
	}
}

func mergeRebaseCmd(action, repoPath, arg string) tea.Cmd {
	return func() tea.Msg {
		msg := mergeRebaseMsg{Action: action, Repo: repoPath}
		var out string
		var err error
		switch action {
		case "merge":
			out, err = git.GitMerge(repoPath, arg, false, false)
		case "rebase":
			out, err = git.GitRebase(repoPath, arg)
		case "abort-merge":
			out, err = git.GitAbortCurrent(repoPath, "merge")
		case "abort-rebase":
			out, err = git.GitAbortCurrent(repoPath, "rebase")
		}
		msg.Output, msg.Err = out, err
		if conflicts, cerr := git.GitConflictFiles(repoPath); cerr == nil {
			msg.Conflicts = conflicts
		}
		return msg
	}
}

func stageHunkCmd(repoPath, path string, index int) tea.Cmd {
	return func() tea.Msg {
		return hunkMsg{Path: path, Index: index, Err: git.GitStageHunk(repoPath, path, index)}
	}
}

func cloneCmd(parentDir, cloneURL, dir string) tea.Cmd {
	return func() tea.Msg {
		got, err := git.GitClone(parentDir, cloneURL, dir)
		return cloneMsg{URL: cloneURL, Dir: got, Err: err}
	}
}

// syncAllCmd runs GitSyncAll in the background.
func syncAllCmd(ctx context.Context, rootDir string) tea.Cmd {
	return func() tea.Msg {
		results := git.GitSyncAll(rootDir, 3)
		return syncDoneMsg{Results: results}
	}
}

func prActionCmd(token *string, owner, repo string, number int, action string) tea.Cmd {
	return func() tea.Msg {
		if token == nil || *token == "" {
			return prActionMsg{Number: number, Action: action, Err: fmt.Errorf("GitHub login required (press [l])")}
		}
		client := github.NewClient(token)
		var out string
		var err error
		switch action {
		case "merge":
			out, err = client.SetPullRequestState(owner, repo, number, github.PRActionMerge)
		case "close":
			out, err = client.SetPullRequestState(owner, repo, number, github.PRActionClose)
		case "reopen":
			out, err = client.SetPullRequestState(owner, repo, number, github.PRActionOpen)
		default:
			err = fmt.Errorf("unknown PR action %q", action)
		}
		return prActionMsg{Number: number, Action: action, Output: out, Err: err}
	}
}

func issuesCmd(token *string, owner, repo string) tea.Cmd {
	return func() tea.Msg {
		client := github.NewClient(token)
		issues, err := client.ListIssues(owner, repo)
		if err != nil {
			return issuesMsg{Err: err, Repo: repo}
		}
		return issuesMsg{Issues: issues, Repo: repo}
	}
}

// fileHistoryCmd loads `git log --follow` for a path into an overlay.
func fileHistoryCmd(repoPath, path string) tea.Cmd {
	return func() tea.Msg {
		items, err := git.GitFileHistory(repoPath, path, 30)
		if err != nil {
			return overlayDataMsg{Kind: overlayFileHistory, Err: err}
		}
		lines := make([]string, 0, len(items))
		for _, it := range items {
			lines = append(lines, fmt.Sprintf("%s  %-10s %-12s %s", it.Hash, it.Author, truncateStr(it.Date, 12), it.Subject))
		}
		return overlayDataMsg{Kind: overlayFileHistory, Title: fmt.Sprintf("History of %s — Enter: diff this commit · v: revert · Ctrl+E: extract this version · Esc: close", path), Lines: lines}
	}
}

// blameCmd loads blame for a path into an overlay.
func blameCmd(repoPath, path string) tea.Cmd {
	return func() tea.Msg {
		lines, err := git.GitBlame(repoPath, path)
		if err != nil {
			return overlayDataMsg{Kind: overlayBlame, Err: err}
		}
		rendered := make([]string, 0, len(lines))
		for _, l := range lines {
			rendered = append(rendered, fmt.Sprintf("%8s %-10s %4d  %s", l.Hash, truncateStr(l.Author, 10), l.LineNumber, l.Text))
		}
		return overlayDataMsg{Kind: overlayBlame, Title: fmt.Sprintf("Blame: %s — Esc: close", path), Lines: rendered}
	}
}

// hunksCmd loads the hunk split for a path and opens hunk mode.
func hunksCmd(repoPath, path string) tea.Cmd {
	return func() tea.Msg {
		hunks, err := git.GitSplitHunks(repoPath, path)
		if err != nil {
			return overlayDataMsg{Kind: overlayNone, Path: path, Err: err}
		}
		return overlayDataMsg{Kind: overlayNone, Path: path, Hunks: hunks}
	}
}

// ---------- helpers ----------

func truncateStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// splitRepoName splits "owner/repo" GitHub full names.
func splitRepoName(full string) (owner, repo string, ok bool) {
	parts := strings.SplitN(full, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
