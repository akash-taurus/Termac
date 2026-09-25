package main

import "testing"

// switchView must preserve each repo view's list across switches and clear the
// incoming list when it was never loaded, instead of leaking rows across tabs.
// Previously the number keys skipped stashing, unlike tab/#1/#2.
func TestSwitchViewStashesAndRestores(t *testing.T) {
	local := []RepoDetail{{Name: "local-1", Path: "/l1", LocalOnly: true}}
	m := DashboardModel{
		viewMode:      ViewLocal,
		repos:         local,
		selected:      0,
		localRepos:    local,
		localSelected: 0,
	}

	if m.switchView(ViewSystem); m.viewMode != ViewSystem {
		t.Fatalf("viewMode = %v, want ViewSystem", m.viewMode)
	}

	// Returning to Local restores the stashed list.
	m.switchView(ViewLocal)
	if len(m.repos) != 1 || m.repos[0].Name != "local-1" {
		t.Fatalf("local list not restored: %+v", m.repos)
	}

	// GitHub was never loaded: switching there must clear the list rather than
	// showing the local rows.
	m.switchView(ViewGitHub)
	if m.repos != nil {
		t.Fatalf("GitHub view should start empty, got %+v", m.repos)
	}

	// And the local list survives a round trip through GitHub.
	m.switchView(ViewLocal)
	if len(m.repos) != 1 || m.repos[0].Name != "local-1" {
		t.Fatalf("local list lost after GitHub round trip: %+v", m.repos)
	}
}
