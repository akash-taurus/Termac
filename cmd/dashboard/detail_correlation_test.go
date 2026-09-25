package main

import (
	"testing"

	"tui/pkg/git"
)

// A repoDetailMsg must update the repo it belongs to (matched by path), never
// whichever row happens to be highlighted when the response lands. Detail
// requests are fired on every navigation, so responses can arrive out of
// order; without the path match, one repo's branch/status could overwrite
// another's.
func TestRepoDetailMsgUpdatesMatchingRepoOnly(t *testing.T) {
	m := DashboardModel{
		repos: []RepoDetail{
			{Name: "alpha", Path: "/repos/alpha", Branch: "stale-alpha", LocalOnly: true},
			{Name: "beta", Path: "/repos/beta", Branch: "stale-beta", LocalOnly: true},
		},
		selected: 1, // user moved to beta while alpha's detail was in flight
	}

	model, _ := m.Update(repoDetailMsg{
		Detail:    RepoDetail{Name: "alpha", Path: "/repos/alpha", Branch: "fresh-alpha", LocalOnly: true},
		GitStatus: &git.DetailedGitStatus{IsClean: true},
	})
	m = model.(DashboardModel)

	if m.repos[0].Branch != "fresh-alpha" {
		t.Fatalf("alpha row not updated: got %q want %q", m.repos[0].Branch, "fresh-alpha")
	}
	if m.repos[1].Branch != "stale-beta" {
		t.Fatalf("beta row clobbered by alpha's late response: got %q", m.repos[1].Branch)
	}
	// The shared status pane belongs to the selected repo (beta); a non-selected
	// repo's detail must not overwrite it.
	if m.detailedGitStatus != nil {
		t.Fatal("non-selected response overwrote the selected repo's status pane")
	}
}

// A response for a repo that is no longer in the list must be dropped instead
// of mutating an unrelated row.
func TestRepoDetailMsgDropsUnknownRepo(t *testing.T) {
	m := DashboardModel{
		repos:    []RepoDetail{{Name: "alpha", Path: "/repos/alpha", Branch: "keep", LocalOnly: true}},
		selected: 0,
	}
	model, _ := m.Update(repoDetailMsg{Detail: RepoDetail{Name: "gone", Path: "/repos/gone", Branch: "x"}})
	m = model.(DashboardModel)

	if len(m.repos) != 1 {
		t.Fatalf("repo list mutated: %d entries", len(m.repos))
	}
	if m.repos[0].Branch != "keep" {
		t.Fatalf("unrelated row changed: %q", m.repos[0].Branch)
	}
}

// The selected repo's response still updates both the row and the status pane.
func TestRepoDetailMsgUpdatesSelectedRepo(t *testing.T) {
	m := DashboardModel{
		repos: []RepoDetail{
			{Name: "alpha", Path: "/repos/alpha", Branch: "stale-alpha", LocalOnly: true},
			{Name: "beta", Path: "/repos/beta", Branch: "stale-beta", LocalOnly: true},
		},
		selected: 0,
	}
	status := &git.DetailedGitStatus{IsClean: true, Branch: "main"}
	model, _ := m.Update(repoDetailMsg{
		Detail:    RepoDetail{Name: "alpha", Path: "/repos/alpha", Branch: "fresh-alpha", LocalOnly: true},
		GitStatus: status,
	})
	m = model.(DashboardModel)

	if m.repos[0].Branch != "fresh-alpha" {
		t.Fatalf("selected row not updated: got %q", m.repos[0].Branch)
	}
	if m.detailedGitStatus != status {
		t.Fatal("selected repo's status pane not updated")
	}
}
