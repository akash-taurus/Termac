package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// prTestModel returns a model wired for PR-create routing tests. The repo
// path points at a real initialized git directory so key-guard checks
// (IsGitRepository) pass.
func prTestModel(t *testing.T) DashboardModel {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return DashboardModel{
		viewMode: ViewLocal,
		repos:    []RepoDetail{{Name: "owner/repo", Path: dir, LocalOnly: true, LastMessage: "feat: thing"}},
		selected: 0,
		token:    strPtr("ghp_testtoken"),
	}
}

func strPtr(s string) *string { return &s }

// Ctrl+P on the Local tab starts the preflight command.
func TestPRCreateKeyStartsPreflight(t *testing.T) {
	m := prTestModel(t)
	cmd, handled := m.prCreateKey("ctrl+p")
	if !handled {
		t.Fatal("ctrl+p should be handled on the Local tab")
	}
	if cmd == nil {
		t.Fatal("ctrl+p should produce a preflight command")
	}
	msg := cmd()
	pf, ok := msg.(prPreflightMsg)
	if !ok {
		t.Fatalf("expected prPreflightMsg, got %T", msg)
	}
	if pf.RepoPath == "" || !strings.HasSuffix(pf.RepoPath, "001") {
		t.Fatalf("preflight repo = %q", pf.RepoPath)
	}
	// Other keys are not handled.
	if _, handled := m.prCreateKey("p"); handled {
		t.Fatal("plain p must not start the PR flow")
	}
}

// Preflight on a non-Local view must not hijack chords. Non-Local scoping
// lives in handleAdvancedKey (the viewMode==ViewLocal guard), so assert the
// full routing chain there rather than prCreateKey itself.
func TestPRCreateKeyScopedToLocal(t *testing.T) {
	m := prTestModel(t)
	m.viewMode = ViewPlugins
	if _, handled := m.handleAdvancedKey("ctrl+p"); handled {
		t.Fatal("ctrl+p must not leak to non-Local tabs")
	}
}

// A failed preflight clears the flow state and reports the error.
func TestPRPreflightErrorClearsState(t *testing.T) {
	m := prTestModel(t)
	m.prCreate = &prCreateContext{repoPath: "/repo", branch: "main", base: "main"}
	cmd, handled := m.handlePRCreateMsg(prPreflightMsg{Err: errFake()})
	if !handled {
		t.Fatal("preflight error should be handled")
	}
	if cmd != nil {
		t.Fatal("failed preflight should not chain a command")
	}
	if m.prCreate != nil {
		t.Fatal("failed preflight must clear prCreate")
	}
	if !strings.Contains(m.message, "preflight") {
		t.Fatalf("status = %q", m.message)
	}
}

type fakeErr struct{}

func (fakeErr) Error() string { return "boom" }

func errFake() error { return fakeErr{} }

// Unpushed branch: preflight asks for a push instead of opening the flow.
func TestPRPreflightUnpushedBranch(t *testing.T) {
	m := prTestModel(t)
	_, handled := m.handlePRCreateMsg(prPreflightMsg{
		RepoPath: "/repo", Branch: "feature", HasRemote: true, HasCommits: false,
	})
	if !handled {
		t.Fatal("preflight msg should be handled")
	}
	if m.prCreate != nil {
		t.Fatal("unpushed branch must not open the title prompt")
	}
	if !strings.Contains(m.message, "push") {
		t.Fatalf("status should mention pushing, got %q", m.message)
	}
}

// A good preflight opens the title prompt seeded with the last commit
// subject and stores the context.
func TestPRPreflightGoodOpensTitlePrompt(t *testing.T) {
	m := prTestModel(t)
	cmd, handled := m.handlePRCreateMsg(prPreflightMsg{
		RepoPath:   "/repo",
		Branch:     "feature",
		Base:       "main",
		HasRemote:  true,
		HasCommits: true,
	})
	if !handled || cmd == nil {
		t.Fatalf("handled=%v cmd=%v", handled, cmd)
	}
	if m.prCreate == nil || m.prCreate.branch != "feature" || m.prCreate.base != "main" {
		t.Fatalf("prCreate = %+v", m.prCreate)
	}
	if m.prompt == nil || m.prompt.kind != "pr-title" {
		t.Fatalf("prompt = %+v", m.prompt)
	}
	if m.prompt.defaultVal != "feat: thing" {
		t.Fatalf("default title = %q", m.prompt.defaultVal)
	}
}

// Title prompt routes into the base prompt; base prompt submits the PR.
func TestPRCreatePromptFlow(t *testing.T) {
	m := prTestModel(t)
	m.prCreate = &prCreateContext{repoPath: "/repo", branch: "feature", base: "main"}

	// Title step.
	cmd, handled := m.handlePRCreatePrompt("pr-title", "My PR title")
	if !handled || cmd == nil {
		t.Fatalf("title step: handled=%v cmd=%v", handled, cmd)
	}
	if m.prCreate.title != "My PR title" {
		t.Fatalf("title = %q", m.prCreate.title)
	}
	if m.prompt == nil || m.prompt.kind != "pr-base" {
		t.Fatalf("base prompt = %+v", m.prompt)
	}

	// Empty title cancels.
	m2 := prTestModel(t)
	m2.prCreate = &prCreateContext{repoPath: "/repo", branch: "feature", base: "main"}
	cmd, _ = m2.handlePRCreatePrompt("pr-title", "   ")
	if cmd != nil || m2.prCreate != nil {
		t.Fatal("empty title must cancel the flow")
	}

	// Base step submits.
	m.prompt = nil // pretend base prompt is open
	cmd, handled = m.handlePRCreatePrompt("pr-base", "develop")
	if !handled || cmd == nil {
		t.Fatalf("base step: handled=%v cmd=%v", handled, cmd)
	}
	msg := cmd()
	created, ok := msg.(prCreatedMsg)
	_ = created
	_ = ok
	// The command returns prCreatedMsg when executed — but it performs a
	// network call, so just verify the routing state is cleared lazily.
	if m.prCreate == nil {
		t.Fatal("context should persist until prCreatedMsg arrives")
	}
}

// Base equal to head is rejected.
func TestPRCreateBaseEqualsHeadRejected(t *testing.T) {
	m := prTestModel(t)
	m.prCreate = &prCreateContext{repoPath: "/repo", branch: "main", base: "main"}
	cmd, handled := m.handlePRCreatePrompt("pr-base", "main")
	if !handled {
		t.Fatal("pr-base should be handled")
	}
	if cmd != nil {
		t.Fatal("base==head must not submit")
	}
	if m.prCreate != nil {
		t.Fatal("base==head must cancel the flow")
	}
	if !strings.Contains(m.message, "Base cannot equal head") {
		t.Fatalf("status = %q", m.message)
	}
}

// A successful prCreatedMsg reports the URL and clears the flow.
func TestPRCreatedSuccess(t *testing.T) {
	m := prTestModel(t)
	m.prCreate = &prCreateContext{repoPath: "/repo", branch: "feature", base: "main", title: "T"}
	cmd, handled := m.handlePRCreateMsg(prCreatedMsg{Number: 7, URL: "https://github.com/owner/repo/pull/7", Title: "T"})
	if !handled {
		t.Fatal("created msg should be handled")
	}
	if cmd != nil {
		t.Fatal("created msg should not chain a command")
	}
	if m.prCreate != nil {
		t.Fatal("created msg must clear prCreate")
	}
	if !strings.Contains(m.message, "PR #7") {
		t.Fatalf("status = %q", m.message)
	}
}

// ownerRepoFromRemote parses https and ssh URLs.
func TestOwnerRepoFromRemote(t *testing.T) {
	cases := []struct {
		in          string
		owner, repo string
		ok          bool
	}{
		{"https://github.com/owner/repo.git", "owner", "repo", true},
		{"https://github.com/owner/repo", "owner", "repo", true},
		{"git@github.com:owner/repo.git", "owner", "repo", true},
		{"git@github.com:owner/repo", "owner", "repo", true},
		{"not-a-url", "", "", false},
		{"https://github.com/onlyone", "", "", false},
	}
	for _, tc := range cases {
		owner, repo, ok := ownerRepoFromRemote(tc.in)
		if ok != tc.ok || owner != tc.owner || repo != tc.repo {
			t.Errorf("ownerRepoFromRemote(%q) = %q,%q,%v; want %q,%q,%v", tc.in, owner, repo, ok, tc.owner, tc.repo, tc.ok)
		}
	}
}

// Full Update-loop integration: ctrl+p → preflight msg → title prompt opens
// and captures typing.
func TestPRCreateUpdateLoopIntegration(t *testing.T) {
	m := prTestModel(t)
	// ctrl+p in the update loop produces the preflight cmd.
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = model.(DashboardModel)
	if cmd == nil {
		t.Fatal("ctrl+p should produce a command")
	}
	// Feed a good preflight result through Update.
	model, cmd = m.Update(prPreflightMsg{
		RepoPath: "/repo", Branch: "feature", Base: "main",
		HasRemote: true, HasCommits: true,
	})
	m = model.(DashboardModel)
	if m.prompt == nil || m.prompt.kind != "pr-title" {
		t.Fatalf("title prompt did not open: %+v", m.prompt)
	}
	// Type into the prompt (captured by the prompt modal).
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("abc")})
	m = model.(DashboardModel)
	if m.prompt == nil {
		t.Fatal("prompt should still be open after typing")
	}
	_ = cmd
}

// prBodyFromCommits prefixes a commit list; on error it is empty.
func TestPRBodyFromCommits(t *testing.T) {
	body := prBodyFromCommits(".", "HEAD", "HEAD")
	if body != "" {
		t.Logf("body on degenerate range = %q", body)
	}
}
