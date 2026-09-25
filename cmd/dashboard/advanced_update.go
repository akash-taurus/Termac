package main

// Update-path logic for the advanced git features: message cases, the
// generic confirm/prompt modal handling, overlay navigation, and the
// keybindings that route into the new commands.

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"tui/pkg/git"
)

// ---------- Update: message cases ----------

// handleAdvancedMsg returns (model, cmd, handled). It processes every
// advanced-feature message; false means "not mine" and falls through to the
// legacy switch in Update. Value receiver keeps the tea.Model contract
// consistent with the rest of Update (values, not pointers).
func (m DashboardModel) handleAdvancedMsg(msg tea.Msg) (DashboardModel, tea.Cmd, bool) {
	switch msg := msg.(type) {

	case gitFileMsg:
		if msg.Err != nil {
			m.setStatusf(statusError, "git %s %s: %v", msg.Action, msg.Path, msg.Err)
		} else {
			verb := map[string]string{"stage": "Staged", "unstage": "Unstaged", "unstage-all": "Unstaged all", "discard": "Discarded"}[msg.Action]
			m.setStatusf(statusSuccess, "%s %s", verb, msg.Path)
		}
		return m, m.refreshRepoDetails(), true

	case stashMsg:
		if msg.Err != nil {
			m.setStatusf(statusError, "stash %s: %v", msg.Action, msg.Err)
		} else {
			m.setStatusf(statusSuccess, "Stash %s: %s", msg.Action, firstLine(msg.Output))
		}
		var cmd tea.Cmd
		if msg.Action == "pop" || msg.Action == "apply" || msg.Action == "drop" {
			cmd = m.refreshRepoDetails()
		}
		// Refresh an open stash overlay.
		if m.overlay != nil && m.overlay.kind == overlayStashList {
			return m, tea.Batch(cmd, m.openStashOverlay()), true
		}
		return m, cmd, true

	case branchMsg:
		if msg.Err != nil {
			m.setStatusf(statusError, "branch %s: %v", msg.Action, msg.Err)
		} else {
			m.setStatusf(statusSuccess, "Branch %s: %s", msg.Action, firstLine(msg.Output))
		}
		var cmd tea.Cmd
		switch msg.Action {
		case "checkout":
			cmd = m.reloadAfterBranchSwitch()
		case "create", "delete":
			if m.overlay != nil && m.overlay.kind == overlayBranchList {
				cmd = branchListCmd(m.activeRepoPath())
			} else {
				cmd = m.refreshRepoDetails()
			}
		}
		return m, cmd, true

	case historyActionMsg:
		if msg.Err != nil {
			m.setStatusf(statusError, "%s: %v", msg.Action, msg.Err)
		} else {
			m.setStatusf(statusSuccess, "%s: %s", msg.Action, firstLine(msg.Output))
		}
		return m, m.refreshRepoDetails(), true

	case mergeRebaseMsg:
		if msg.Err != nil {
			if len(msg.Conflicts) > 0 {
				m.setStatusf(statusError, "%s conflicted in %d file(s) — press [C] to list, [A] to abort", msg.Action, len(msg.Conflicts))
			} else {
				m.setStatusf(statusError, "%s: %v", msg.Action, msg.Err)
			}
		} else {
			m.setStatusf(statusSuccess, "%s: %s", msg.Action, firstLine(msg.Output))
		}
		return m, m.refreshRepoDetails(), true

	case hunkMsg:
		if msg.Err != nil {
			m.setStatusf(statusError, "stage hunk %d: %v", msg.Index+1, msg.Err)
		} else {
			m.setStatusf(statusSuccess, "Staged hunk %d of %s", msg.Index+1, msg.Path)
		}
		return m, m.refreshRepoDetails(), true

	case cloneMsg:
		m.cloning = false
		if msg.Err != nil {
			m.setStatusf(statusError, "clone %s: %v", msg.URL, msg.Err)
		} else {
			m.setStatusf(statusSuccess, "Cloned into %s — press [o] and paste that path to open it", msg.Dir)
		}
		return m, nil, true

	case syncProgressMsg:
		m.setStatusf(statusLoading, "Syncing %d/%d: %s…", msg.Done, msg.Total, msg.Current)
		return m, nil, true

	case syncDoneMsg:
		m.syncRunning = false
		m.syncResults = msg.Results
		m.syncOverlay = true
		failed, skipped := 0, 0
		for _, r := range msg.Results {
			if r.Err != nil {
				failed++
			}
			if r.Skipped {
				skipped++
			}
		}
		m.setStatusf(statusSuccess, "Synced %d repos: %d ok, %d skipped (no upstream), %d failed",
			len(msg.Results), len(msg.Results)-failed-skipped, skipped, failed)
		return m, nil, true

	case prActionMsg:
		if msg.Err != nil {
			m.setStatusf(statusError, "PR #%d %s: %v", msg.Number, msg.Action, msg.Err)
		} else {
			m.setStatusf(statusSuccess, "PR #%d %s: %s", msg.Number, msg.Action, msg.Output)
		}
		return m, nil, true

	case issuesMsg:
		if msg.Err != nil {
			m.setStatusf(statusError, "issues: %v", msg.Err)
			return m, nil, true
		}
		lines := make([]string, 0, len(msg.Issues))
		for _, i := range msg.Issues {
			lines = append(lines, fmt.Sprintf("#%-4d %-8s %s", i.Number, i.State, i.Title))
		}
		if len(lines) == 0 {
			lines = []string{"(No open issues)"}
		}
		m.overlay = &overlayPane{
			kind:   overlayIssues,
			title:  fmt.Sprintf("Open issues: %s — Esc: close", msg.Repo),
			lines:  lines,
			issues: msg.Issues,
		}
		return m, nil, true

	case overlayDataMsg:
		if msg.Err != nil {
			m.setStatusf(statusError, "%v", msg.Err)
			return m, nil, true
		}
		switch msg.Kind {
		case overlayBranchList:
			m.overlay = &overlayPane{kind: msg.Kind, title: msg.Title, lines: msg.Lines, branches: msg.Branches}
		case overlayStashList:
			m.overlay = &overlayPane{kind: msg.Kind, title: msg.Title, lines: msg.Lines, stashes: msg.Stashes}
		case overlayReflog, overlayFileHistory, overlayBlame:
			m.overlay = &overlayPane{kind: msg.Kind, title: msg.Title, lines: msg.Lines}
		case overlayNone:
			// hunk data: enter hunk mode in the diff pane
			if len(msg.Hunks) > 0 {
				m.hunkMode = true
				m.hunkCursor = 0
				m.overlay = &overlayPane{kind: overlayNone, path: msg.Path, title: fmt.Sprintf("Hunks: %s — s: stage hunk · Esc: exit hunk mode", msg.Path), hunks: msg.Hunks}
				// Render the hunk bodies as the overlay content.
				lines := make([]string, 0, len(msg.Hunks))
				for i, h := range msg.Hunks {
					marker := " "
					if i == m.hunkCursor {
						marker = ">"
					}
					lines = append(lines, marker+" "+h.Header)
				}
				m.overlay.lines = lines
			} else {
				m.setStatus(statusWarn, "No hunks to stage")
			}
		}
		return m, nil, true
	}
	return m, nil, false
}

// ---------- overlay + modal helpers ----------

// activeRepoPath returns the selected local repo's path ("" when none).
func (m *DashboardModel) activeRepoPath() string {
	if m.viewMode != ViewLocal || m.selected < 0 || m.selected >= len(m.repos) {
		return ""
	}
	return m.repos[m.selected].Path
}

func (m *DashboardModel) activeRepoName() string {
	if m.selected < 0 || m.selected >= len(m.repos) {
		return ""
	}
	return m.repos[m.selected].Name
}

// selectedFile returns the path of the file row under the detail cursor.
func (m *DashboardModel) selectedFile() (string, bool) {
	if m.detailedGitStatus == nil || len(m.detailedGitStatus.Files) == 0 {
		return "", false
	}
	if m.fileCursor < 0 || m.fileCursor >= len(m.detailedGitStatus.Files) {
		m.fileCursor = 0
	}
	return m.detailedGitStatus.Files[m.fileCursor].Path, true
}

func (m *DashboardModel) hunkPath() string {
	p, _ := m.selectedFile()
	return p
}

// openStashOverlay loads stash entries into an overlay (returns the Cmd).
func (m *DashboardModel) openStashOverlay() tea.Cmd {
	repo := m.activeRepoPath()
	if repo == "" {
		return nil
	}
	return func() tea.Msg {
		items, err := git.GitStashList(repo)
		if err != nil {
			return overlayDataMsg{Kind: overlayStashList, Err: err}
		}
		lines := make([]string, 0, len(items))
		for _, s := range items {
			lines = append(lines, fmt.Sprintf("stash@{%d}  %s  %s", s.Index, s.Hash, s.Desc))
		}
		if len(lines) == 0 {
			lines = []string{"(No stashes)"}
		}
		return overlayDataMsg{Kind: overlayStashList, Title: "Stashes — Enter: pop · a: apply · d: drop · Esc: close", Lines: lines, Stashes: items}
	}
}

// reloadAfterBranchSwitch re-reads everything for the selected repo after a
// checkout: details, status, logs.
func (m *DashboardModel) reloadAfterBranchSwitch() tea.Cmd {
	repo := m.activeRepoPath()
	if repo == "" {
		return nil
	}
	m.overlay = nil
	return repoDetailCommand(context.Background(), m.repos[m.selected])
}

// refreshRepoDetails refreshes the selected repo's status/log pane.
func (m *DashboardModel) refreshRepoDetails() tea.Cmd {
	if m.selected < 0 || m.selected >= len(m.repos) {
		return nil
	}
	return repoDetailCommand(context.Background(), m.repos[m.selected])
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// ---------- Generic confirm / prompt modals ----------

// openConfirm shows the generic confirmation modal.
func (m *DashboardModel) openConfirm(c confirmAction) {
	m.confirm = &c
}

// openPrompt shows the generic input modal with a prefilled value.
func (m *DashboardModel) openPrompt(p promptAction) tea.Cmd {
	m.prompt = &p
	m.promptInput = textinput.New()
	m.promptInput.Placeholder = p.title
	m.promptInput.CharLimit = 200
	m.promptInput.Width = 52
	m.promptInput.SetValue(p.defaultVal)
	m.promptInput.Focus()
	return textinput.Blink
}

// runConfirm executes the confirmed action.
func (m *DashboardModel) runConfirm() tea.Cmd {
	c := m.confirm
	m.confirm = nil
	if c == nil {
		return nil
	}
	repo := c.repoPath
	switch c.kind {
	case "discard-file":
		return gitFileCmd("discard", repo, c.arg)
	case "reset-hard":
		return historyActionCmd("reset-hard", repo, c.arg, git.ResetHard)
	case "reset-mixed":
		return historyActionCmd("reset-mixed", repo, c.arg, git.ResetMixed)
	case "reset-soft":
		return historyActionCmd("reset-soft", repo, c.arg, git.ResetSoft)
	case "revert":
		return historyActionCmd("revert", repo, c.arg, "")
	case "checkout-sha":
		return historyActionCmd("checkout-sha", repo, c.arg, "")
	case "stash-drop":
		return stashCmd("drop", repo, indexOfStash(m.overlay, c.arg), "", false)
	case "delete-branch":
		return branchCmd("delete", repo, c.arg, false)
	case "checkout-force":
		return branchCmd("checkout", repo, c.arg, false)
	case "abort-merge":
		return mergeRebaseCmd("abort-merge", repo, "")
	case "abort-rebase":
		return mergeRebaseCmd("abort-rebase", repo, "")
	case "pr-merge":
		return m.prActionFromOverlay("merge", c.arg)
	case "pr-close":
		return m.prActionFromOverlay("close", c.arg)
	}
	return nil
}

// indexOfStash resolves a stash ref string ("0") to its index.
func indexOfStash(overlay *overlayPane, ref string) int {
	if overlay == nil {
		return 0
	}
	n := 0
	if _, err := fmt.Sscanf(ref, "%d", &n); err == nil {
		if n >= 0 && n < len(overlay.stashes) {
			return overlay.stashes[n].Index
		}
	}
	return 0
}

// prActionFromOverlay maps a PR number (stored in confirm.arg) to an action.
func (m *DashboardModel) prActionFromOverlay(action, number string) tea.Cmd {
	n := 0
	if _, err := fmt.Sscanf(number, "%d", &n); err != nil {
		return nil
	}
	full := m.activeRepoName()
	owner, repo, ok := splitRepoName(full)
	if !ok {
		m.setStatusf(statusWarn, "Not a GitHub repo: %s", full)
		return nil
	}
	return prActionCmd(m.token, owner, repo, n, action)
}

// runPrompt executes the submitted prompt action. PR-create prompt kinds are
// routed first; unknown kinds fall through to the generic switch.
func (m *DashboardModel) runPrompt(value string) tea.Cmd {
	p := m.prompt
	m.prompt = nil
	if p == nil {
		return nil
	}
	value = strings.TrimSpace(value)

	// PR-create flow owns its prompt kinds while the context is active.
	if cmd, handled := m.handlePRCreatePrompt(p.kind, value); handled {
		return cmd
	}

	repo := p.repoPath
	switch p.kind {
	case "branch-create":
		return branchCmd("create", repo, value, true)
	case "branch-delete":
		m.openConfirm(confirmAction{title: "Delete branch " + value + "? (unmerged commits lost)", kind: "delete-branch", repoPath: repo, arg: value})
		return nil
	case "stash-push":
		return stashCmd("push", repo, 0, value, true)
	case "clone":
		if value == "" {
			m.setStatus(statusWarn, "Clone URL cannot be empty")
			return nil
		}
		m.cloning = true
		m.setStatusf(statusLoading, "Cloning %s…", value)
		parent, _ := os.Getwd()
		return cloneCmd(parent, value, "")
	case "merge":
		return mergeRebaseCmd("merge", repo, value)
	case "rebase":
		return mergeRebaseCmd("rebase", repo, value)
	}
	return nil
}

// ---------- Update hooks ----------

// advancedUpdateMsg is called at the top of Update for every message.
func (m DashboardModel) advancedUpdateMsg(msg tea.Msg) (DashboardModel, tea.Cmd, bool) {
	// Prompt modal captures all keys when open.
	if m.prompt != nil {
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "esc":
				m.prompt = nil
				m.promptInput.Blur()
				return m, nil, true
			case "enter":
				val := m.promptInput.Value()
				m.promptInput.Blur()
				return m, m.runPrompt(val), true
			}
			var cmd tea.Cmd
			m.promptInput, cmd = m.promptInput.Update(key)
			return m, cmd, true
		}
	}

	// Confirm modal: y/n/enter/esc.
	if m.confirm != nil {
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "y", "Y", "enter":
				return m, m.runConfirm(), true
			case "n", "N", "esc":
				m.confirm = nil
				return m, nil, true
			}
			return m, nil, true // swallow everything else
		}
		return m, nil, true
	}

	// Overlay navigation captures keys. Non-key messages MUST keep flowing:
	// overlay actions (pop/apply/drop/checkout) dispatch commands while the
	// overlay stays open, and swallowing their result messages silently
	// dropped the operation's status refresh (and left the overlay stale).
	if m.overlay != nil && m.overlay.kind != overlayNone {
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "esc", "q":
				m.overlay = nil
				return m, nil, true
			case "up", "k":
				if m.overlay.cursor > 0 {
					m.overlay.cursor--
				}
				return m, nil, true
			case "down", "j":
				if m.overlay.cursor < len(m.overlay.lines)-1 {
					m.overlay.cursor++
				}
				return m, nil, true
			case "enter":
				return m, m.overlayEnter(), true
			case "d":
				return m, m.overlayDelete(), true
			case "a":
				return m, m.overlayApply(), true
			}
			// Any other key is swallowed so it cannot trigger a global shortcut
			// behind the overlay.
			return m, nil, true
		}
	}

	// PR-create flow messages (preflight/created).
	if cmd, handled := m.handlePRCreateMsg(msg); handled {
		return m, cmd, true
	}

	// Normal messages: try the advanced switch first.
	model, cmd, handled := m.handleAdvancedMsg(msg)
	return model, cmd, handled
}

// compile-time interface checks for the update hooks above.
var (
	_ func(DashboardModel, tea.Msg) (DashboardModel, tea.Cmd, bool) = DashboardModel.advancedUpdateMsg
)

// overlayEnter runs the primary action of the highlighted overlay row.
func (m *DashboardModel) overlayEnter() tea.Cmd {
	repo := m.activeRepoPath()
	o := m.overlay
	if o == nil || o.cursor >= len(o.lines) {
		return nil
	}
	switch o.kind {
	case overlayBranchList:
		if b := o.branches[o.cursor]; !b.IsRemote && !b.IsHead {
			return branchCmd("checkout", repo, b.Name, false)
		}
	case overlayStashList:
		if o.cursor < len(o.stashes) {
			return stashCmd("pop", repo, o.stashes[o.cursor].Index, "", false)
		}
	case overlayReflog:
		if o.cursor < len(o.lines) {
			sha := strings.Fields(o.lines[o.cursor])
			if len(sha) > 1 {
				m.openConfirm(confirmAction{title: "Checkout " + sha[1] + "? (detached HEAD)", kind: "checkout-sha", repoPath: repo, arg: sha[1]})
			}
		}
	case overlayFileHistory:
		if o.cursor < len(o.lines) {
			fields := strings.Fields(o.lines[o.cursor])
			if len(fields) > 0 {
				sha := fields[0]
				m.overlay = nil
				out, err := git.GitDiffCommit(repo, sha)
				if err != nil {
					m.setStatusf(statusError, "diff %s: %v", sha, err)
					return nil
				}
				m.gitDiffText = out
				m.gitDiffActive = true
				m.setStatusf(statusSuccess, "Commit diff %s — [d]/[Esc] closes", sha)
			}
		}
	case overlayIssues:
		// Issues are read-only here; Enter does nothing yet.
		return nil
	}
	return nil
}

// overlayDelete runs the "delete/drop" action of the highlighted row.
func (m *DashboardModel) overlayDelete() tea.Cmd {
	repo := m.activeRepoPath()
	o := m.overlay
	if o == nil {
		return nil
	}
	switch o.kind {
	case overlayStashList:
		if o.cursor < len(o.stashes) {
			s := o.stashes[o.cursor]
			m.openConfirm(confirmAction{title: fmt.Sprintf("Drop stash@{%d} (%s)?", s.Index, s.Desc), kind: "stash-drop", repoPath: repo, arg: fmt.Sprintf("%d", o.cursor)})
		}
		return nil
	case overlayBranchList:
		if o.cursor < len(o.branches) {
			b := o.branches[o.cursor]
			if b.IsRemote || b.IsHead {
				m.setStatus(statusWarn, "Cannot delete remote/current branch from here")
				return nil
			}
			m.openConfirm(confirmAction{title: "Delete branch " + b.Name + "?", kind: "delete-branch", repoPath: repo, arg: b.Name})
		}
		return nil
	}
	return nil
}

// overlayApply is the stash "apply (keep entry)" action.
func (m *DashboardModel) overlayApply() tea.Cmd {
	repo := m.activeRepoPath()
	o := m.overlay
	if o != nil && o.kind == overlayStashList && o.cursor < len(o.stashes) {
		return stashCmd("apply", repo, o.stashes[o.cursor].Index, "", false)
	}
	return nil
}

// handleAdvancedKey routes global/Local-tab keys for the new features.
// Returns (cmd, handled).
func (m *DashboardModel) handleAdvancedKey(key string) (tea.Cmd, bool) {
	repo := m.activeRepoPath()

	// Log-pane navigation: when the log view is open, j/k/up/down move the
	// highlighted commit instead of the repo selection.
	if m.gitLogActive && m.viewMode == ViewLocal && len(m.gitLogItems) > 0 {
		switch key {
		case "down", "j":
			if m.gitLogCursor < len(m.gitLogItems)-1 {
				m.gitLogCursor++
			}
			return nil, true
		case "up", "k":
			if m.gitLogCursor > 0 {
				m.gitLogCursor--
			}
			return nil, true
		}
	}

	// Hunk-mode keys take priority when active.
	if m.hunkMode && m.overlay != nil {
		switch key {
		case "n", "down", "j":
			if m.hunkCursor < len(m.overlay.hunks)-1 {
				m.hunkCursor++
				m.refreshHunkOverlayLines()
			}
			return nil, true
		case "p", "up", "k":
			if m.hunkCursor > 0 {
				m.hunkCursor--
				m.refreshHunkOverlayLines()
			}
			return nil, true
		case "s":
			// Target the file the loaded hunks belong to, not whatever the
			// detail-pane cursor happens to be on now.
			hunkPath := m.overlay.path
			if hunkPath == "" {
				hunkPath = m.hunkPath()
			}
			if m.hunkCursor < len(m.overlay.hunks) {
				return stageHunkCmd(repo, hunkPath, m.hunkCursor), true
			}
			return nil, true
		case "esc":
			m.hunkMode = false
			m.overlay = nil
			return nil, true
		}
	}

	// ←/→ move the changed-file cursor in the Local detail pane (↑/↓ still
	// select repos, handled by the legacy switch).
	if m.viewMode == ViewLocal && m.detailedGitStatus != nil && len(m.detailedGitStatus.Files) > 0 {
		switch key {
		case "right":
			m.fileCursor = (m.fileCursor + 1) % len(m.detailedGitStatus.Files)
			return nil, true
		case "left":
			m.fileCursor = (m.fileCursor - 1 + len(m.detailedGitStatus.Files)) % len(m.detailedGitStatus.Files)
			return nil, true
		}
	}

	// Local-tab features only: guard on the view so chords never hijack
	// plugin/system/GitHub-tab actions.
	if m.viewMode == ViewLocal {
		switch key {
		// ----- staging / files -----
		case "S": // stage hovered file
			if p, ok := m.selectedFile(); ok {
				return gitFileCmd("stage", repo, p), true
			}
		case "U": // unstage hovered file
			if p, ok := m.selectedFile(); ok {
				return gitFileCmd("unstage", repo, p), true
			}
		case "ctrl+u": // unstage everything
			return gitFileCmd("unstage-all", repo, "."), true
		case "ctrl+x": // discard hovered file (confirm)
			if p, ok := m.selectedFile(); ok {
				m.openConfirm(confirmAction{title: "Discard ALL changes in " + p + "? Irreversible.", kind: "discard-file", repoPath: repo, arg: p})
				return nil, true
			}

		// ----- stash -----
		case "z":
			if repo != "" {
				return m.openPrompt(promptAction{kind: "stash-push", title: "Stash message (optional)", repoPath: repo}), true
			}
		case "Z":
			if repo != "" {
				return m.openStashOverlay(), true
			}

		// ----- branches / merge / rebase -----
		case "B":
			if repo != "" {
				return branchListCmd(repo), true
			}
		case "ctrl+n":
			if repo != "" {
				return m.openPrompt(promptAction{kind: "branch-create", title: "New branch name", repoPath: repo}), true
			}
		case "M":
			if repo != "" {
				return m.openPrompt(promptAction{kind: "merge", title: "Branch to merge INTO current", repoPath: repo}), true
			}
		case "ctrl+r":
			if repo != "" {
				return m.openPrompt(promptAction{kind: "rebase", title: "Rebase current branch ONTO", repoPath: repo}), true
			}
		case "ctrl+a": // abort in-progress merge/rebase (confirm)
			if repo != "" {
				m.openConfirm(confirmAction{title: "Abort the in-progress merge or rebase?", kind: "abort-merge", repoPath: repo})
				return nil, true
			}

		// ----- history / log pane -----
		case "enter":
			if m.gitLogActive {
				return m.logSelectionAction(), true
			}

		// ----- hunk mode -----
		case "h":
			if m.gitDiffActive && repo != "" {
				if p, ok := m.selectedFile(); ok {
					return hunksCmd(repo, p), true
				}
			}

		// ----- reflog -----
		case "ctrl+g":
			if repo != "" {
				return reflogCmd(repo), true
			}

		// ----- file history / blame from detail file list -----
		case "ctrl+y":
			if repo != "" {
				if p, ok := m.selectedFile(); ok {
					return fileHistoryCmd(repo, p), true
				}
			}
		case "ctrl+b":
			if repo != "" {
				if p, ok := m.selectedFile(); ok {
					return blameCmd(repo, p), true
				}
			}

		// ----- fetch --prune -----
		case "ctrl+f":
			if repo != "" {
				return func() tea.Msg {
					out, err := git.GitFetchPrune(repo)
					return historyActionMsg{Action: "fetch-prune", Output: out, Err: err, Repo: repo}
				}, true
			}

		// ----- create PR from current branch -----
		case "ctrl+p":
			return m.prCreateKey(key)
		}
	}

	// Global chords (any view).
	switch key {

	// ----- sync all -----
	case "ctrl+s":
		if !m.syncRunning {
			m.syncRunning = true
			m.setStatus(statusLoading, "Syncing all local repositories (pull --ff-only)…")
			home, _ := os.UserHomeDir()
			return syncAllCmd(context.Background(), home), true
		}

	// ----- clone -----
	case "ctrl+l":
		return m.openPrompt(promptAction{kind: "clone", title: "Repository URL to clone"}), true

	// ----- issues (GitHub tab) -----
	case "I":
		if m.viewMode == ViewGitHub && m.selected >= 0 && m.selected < len(m.repos) {
			if owner, name, ok := splitRepoName(m.repos[m.selected].Name); ok {
				return issuesCmd(m.token, owner, name), true
			}
		}

	// ----- PR actions (GitHub tab) -----
	case "ctrl+m":
		if m.viewMode == ViewGitHub && m.selected >= 0 && m.selected < len(m.repos) {
			if pr := m.selectedPR(); pr != nil {
				m.openConfirm(confirmAction{title: fmt.Sprintf("Merge PR #%d (%s)?", pr.Number, pr.Title), kind: "pr-merge", repoPath: "", arg: fmt.Sprintf("%d", pr.Number)})
				return nil, true
			}
		}
	case "ctrl+e":
		if m.viewMode == ViewGitHub && m.selected >= 0 && m.selected < len(m.repos) {
			if pr := m.selectedPR(); pr != nil {
				m.openConfirm(confirmAction{title: fmt.Sprintf("Close PR #%d (%s)?", pr.Number, pr.Title), kind: "pr-close", repoPath: "", arg: fmt.Sprintf("%d", pr.Number)})
				return nil, true
			}
		}
	}
	return nil, false
}

// githubPullRequestRef is a minimal PR reference for confirm dialogs.
type githubPullRequestRef struct {
	Number int
	Title  string
}

// refreshHunkOverlayLines re-renders the hunk selector rows after a cursor
// move.
func (m *DashboardModel) refreshHunkOverlayLines() {
	if m.overlay == nil {
		return
	}
	lines := make([]string, 0, len(m.overlay.hunks))
	for i, h := range m.overlay.hunks {
		marker := " "
		if i == m.hunkCursor {
			marker = ">"
		}
		lines = append(lines, marker+" "+h.Header)
	}
	m.overlay.lines = lines
}

// selectedPR returns the PR under the GitHub detail cursor, if loaded.
func (m *DashboardModel) selectedPR() *githubPullRequestRef {
	if m.selected < 0 || m.selected >= len(m.repos) {
		return nil
	}
	repo := m.repos[m.selected]
	if len(repo.OpenPRList) == 0 {
		return nil
	}
	pr := repo.OpenPRList[0] // detail pane lists PRs; first is highlighted
	return &githubPullRequestRef{Number: pr.Number, Title: pr.Title}
}

// logSelectionAction runs on Enter in the log pane: revert/reset/diff the
// highlighted commit.
func (m *DashboardModel) logSelectionAction() tea.Cmd {
	repo := m.activeRepoPath()
	if repo == "" || m.gitLogCursor >= len(m.gitLogItems) {
		return nil
	}
	item := m.gitLogItems[m.gitLogCursor]
	sha := item.Hash
	// Cycle: first Enter = show commit diff, Enter again = reset menu.
	if m.logDiffSha == sha {
		// Second Enter on same sha: offer reset options via confirm cycle.
		switch m.logActionStep {
		case 0:
			m.logActionStep = 1
			m.openConfirm(confirmAction{title: "Reset --mixed to " + sha + "? (unstage, keep files)", kind: "reset-mixed", repoPath: repo, arg: sha})
		case 1:
			m.logActionStep = 2
			m.openConfirm(confirmAction{title: "Reset --hard to " + sha + "? DELETES all changes after it!", kind: "reset-hard", repoPath: repo, arg: sha})
		default:
			m.logActionStep = 0
			m.logDiffSha = ""
			m.setStatus(statusInfo, "Reset cycle cancelled")
		}
		return nil
	}
	m.logDiffSha = sha
	m.logActionStep = 0
	out, err := git.GitDiffCommit(repo, sha)
	if err != nil {
		m.setStatusf(statusError, "diff %s: %v", sha, err)
		return nil
	}
	m.gitDiffText = out
	m.gitDiffActive = true
	m.setStatusf(statusInfo, "Commit %s diff — Enter again for reset options", sha)
	return nil
}
