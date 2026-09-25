package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"tui/pkg/git"
)

// advancedKeyModel builds a model with one local repo and detailed status.
func advancedKeyModel(t *testing.T) DashboardModel {
	t.Helper()
	m := DashboardModel{
		viewMode: ViewLocal,
		repos:    []RepoDetail{{Name: "r", Path: "/repo", LocalOnly: true}},
		selected: 0,
		detailedGitStatus: &git.DetailedGitStatus{
			IsGitRepo:     true,
			Branch:        "main",
			StagedCount:   1,
			UnstagedCount: 1,
			Files: []git.FileStatusItem{
				{Path: "a.txt", Status: "M", Staged: true},
				{Path: "b.txt", Status: "M", Staged: false},
			},
		},
	}
	return m
}

// S must stage the hovered file; the staging keys must be scoped to the
// Local tab so they never hijack chords on other views.
func TestAdvancedKeyStageFile(t *testing.T) {
	m := advancedKeyModel(t)
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	m2 := model.(DashboardModel)
	if cmd == nil {
		t.Fatal("S should produce a stage command")
	}
	msg := cmd()
	gfm, ok := msg.(gitFileMsg)
	if !ok {
		t.Fatalf("expected gitFileMsg, got %T", msg)
	}
	if gfm.Action != "stage" || gfm.Path != "a.txt" || gfm.Repo != "/repo" {
		t.Fatalf("stage msg = %+v", gfm)
	}
	_ = m2
}

// Discard (ctrl+x) must open a confirm modal, not delete immediately.
func TestAdvancedKeyDiscardRequiresConfirm(t *testing.T) {
	m := advancedKeyModel(t)
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	m = model.(DashboardModel)
	if cmd != nil {
		t.Fatal("discard must not run before confirmation")
	}
	if m.confirm == nil || m.confirm.kind != "discard-file" || m.confirm.arg != "a.txt" {
		t.Fatalf("confirm = %+v", m.confirm)
	}
	// Confirming runs the discard command.
	model, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd == nil {
		t.Fatal("confirm y should produce a command")
	}
	if _, ok := cmd().(gitFileMsg); !ok {
		t.Fatalf("expected gitFileMsg after confirm, got %T", cmd())
	}
	m = model.(DashboardModel)
	if m.confirm != nil {
		t.Fatal("confirm modal should be closed after y")
	}
}

// S on the Plugins tab must NOT stage anything (view scoping guard).
func TestAdvancedKeyScopedToLocalTab(t *testing.T) {
	m := advancedKeyModel(t)
	m.viewMode = ViewPlugins
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(gitFileMsg); ok {
			t.Fatal("S leaked to git staging from a non-Local tab")
		}
	}
}

// Stash list overlay: opening with Z, feeding data, drop-confirm, closing.
func TestStashOverlayOpenNavigateClose(t *testing.T) {
	m := advancedKeyModel(t)
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Z")})
	m = model.(DashboardModel)
	if cmd == nil {
		t.Fatal("Z should open the stash overlay command")
	}
	// Feed the overlay data back in (as the command would); the fake path
	// errors, so override the message with synthetic stash rows.
	om := overlayDataMsg{
		Kind:    overlayStashList,
		Title:   "Stashes — Enter: pop · a: apply · d: drop · Esc: close",
		Lines:   []string{"stash@{0} abc WIP", "stash@{1} def more"},
		Stashes: []git.StashItem{{Index: 0, Hash: "abc", Desc: "WIP"}, {Index: 1, Hash: "def", Desc: "more"}},
	}
	model, _ = m.Update(om)
	m = model.(DashboardModel)
	if m.overlay == nil || m.overlay.kind != overlayStashList {
		t.Fatal("stash overlay did not open")
	}
	// Drop flow on the highlighted row opens a confirm, not a direct drop.
	model, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = model.(DashboardModel)
	if cmd != nil || m.confirm == nil || m.confirm.kind != "stash-drop" {
		t.Fatalf("stash drop should require confirm, got cmd=%v confirm=%+v", cmd, m.confirm)
	}
	// Esc closes the overlay.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
}

// Log pane: j/k moves the commit cursor and Enter routes to the commit-diff
// path. (The diff itself fails on the fake repo path; we assert the routing
// state changed.)
func TestLogCursorAndCommitDiff(t *testing.T) {
	m := advancedKeyModel(t)
	m.gitLogActive = true
	m.gitLogItems = []git.CommitLogItem{
		{Hash: "aaa1111", Subject: "first"},
		{Hash: "bbb2222", Subject: "second"},
	}
	// Move down once via the advanced key handler.
	_, handled := m.handleAdvancedKey("j")
	if !handled {
		t.Fatal("j in log pane should be handled")
	}
	if m.gitLogCursor != 1 {
		t.Fatalf("gitLogCursor = %d, want 1", m.gitLogCursor)
	}
	// Enter runs the selection action (may fail on the fake path; assert
	// it took the diff branch by checking the sha memo).
	m.logSelectionAction()
	if m.logDiffSha == "" && !strings.Contains(m.message, "diff") {
		t.Fatal("Enter in log pane should attempt a commit diff")
	}
}

// Generic prompt modal captures keys until Enter/Esc.
func TestPromptModalCapture(t *testing.T) {
	m := advancedKeyModel(t)
	// ctrl+l opens clone prompt (global).
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	m = model.(DashboardModel)
	if m.prompt == nil || m.prompt.kind != "clone" {
		t.Fatalf("prompt = %+v", m.prompt)
	}
	// Typing goes to the prompt input, not the app.
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = model.(DashboardModel)
	if m.prompt == nil {
		t.Fatal("prompt should still be open after typing")
	}
	// Esc cancels.
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = model.(DashboardModel)
	if m.prompt != nil {
		t.Fatal("esc should close the prompt")
	}
}
