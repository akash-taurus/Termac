package main

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"tui/pkg/theme"
)

// A failed git action must be classified as an error explicitly, rather than
// relying on the status bar guessing severity from the message text.
func TestStatusLevelErrorOnGitFailure(t *testing.T) {
	m := DashboardModel{
		repos:    []RepoDetail{{Name: "alpha", Path: "/repos/alpha", LocalOnly: true}},
		selected: 0,
	}
	model, _ := m.Update(gitActionMsg{Action: "push", Repo: "/repos/alpha", Err: errors.New("boom")})
	m = model.(DashboardModel)

	if m.statusLevel != statusError {
		t.Fatalf("statusLevel = %v, want statusError", m.statusLevel)
	}
}

// Severity must reset each cycle so a later informational message cannot
// inherit a previous error's level.
func TestStatusLevelResetsBetweenMessages(t *testing.T) {
	m := DashboardModel{
		repos:    []RepoDetail{{Name: "alpha", Path: "/repos/alpha", LocalOnly: true}},
		selected: 0,
	}
	model, _ := m.Update(gitActionMsg{Action: "push", Repo: "/repos/alpha", Err: errors.New("boom")})
	m = model.(DashboardModel)
	if m.statusLevel != statusError {
		t.Fatalf("precondition: statusLevel = %v, want statusError", m.statusLevel)
	}

	// Any subsequent message re-enters Update and resets the level to info.
	model, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = model.(DashboardModel)
	if m.statusLevel != statusInfo {
		t.Fatalf("statusLevel did not reset: got %v, want statusInfo", m.statusLevel)
	}
}

// Each severity must render a distinct, recognizable icon.
func TestStatusIconForLevels(t *testing.T) {
	pal := theme.Default
	cases := []struct {
		level statusLevel
		glyph string
	}{
		{statusInfo, "ℹ"},
		{statusSuccess, "✔"},
		{statusWarn, "▲"},
		{statusError, "✖"},
		{statusLoading, "◌"},
	}
	for _, tc := range cases {
		got := statusIconFor(tc.level, pal)
		if !strings.Contains(got, tc.glyph) {
			t.Errorf("statusIconFor(%v) = %q, want it to contain %q", tc.level, got, tc.glyph)
		}
	}
}
