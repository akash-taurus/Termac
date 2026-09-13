package github

import (
	"encoding/json"
	"testing"
)

func TestRepo_UnmarshalJSON(t *testing.T) {
	sample := `[
		{
			"id": 123456,
			"name": "TUI",
			"full_name": "akash-taurus/TUI",
			"owner": {
				"login": "akash-taurus",
				"id": 98765,
				"avatar_url": "https://avatars.githubusercontent.com/u/98765"
			},
			"description": "Terminal Dashboard",
			"html_url": "https://github.com/akash-taurus/TUI",
			"clone_url": "https://github.com/akash-taurus/TUI.git",
			"default_branch": "main",
			"stargazers_count": 42,
			"forks_count": 5,
			"language": "Go",
			"private": false,
			"updated_at": "2026-09-13T10:00:00Z"
		}
	]`

	var repos []Repo
	if err := json.Unmarshal([]byte(sample), &repos); err != nil {
		t.Fatalf("failed to unmarshal repo: %v", err)
	}

	if len(repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(repos))
	}
	r := repos[0]
	if r.Name != "TUI" {
		t.Errorf("got name %q, want 'TUI'", r.Name)
	}
	if r.Owner.Login != "akash-taurus" {
		t.Errorf("got owner login %q, want 'akash-taurus'", r.Owner.Login)
	}
}

func TestPullRequest_UnmarshalJSON(t *testing.T) {
	sample := `[
		{
			"id": 1001,
			"number": 12,
			"title": "Add system metrics",
			"state": "open",
			"user": {
				"login": "akash-taurus",
				"id": 98765
			},
			"html_url": "https://github.com/akash-taurus/TUI/pull/12"
		}
	]`

	var prs []PullRequest
	if err := json.Unmarshal([]byte(sample), &prs); err != nil {
		t.Fatalf("failed to unmarshal PR: %v", err)
	}

	if len(prs) != 1 {
		t.Fatalf("expected 1 PR, got %d", len(prs))
	}
	if prs[0].Number != 12 {
		t.Errorf("got PR number %d, want 12", prs[0].Number)
	}
	if prs[0].User.Login != "akash-taurus" {
		t.Errorf("got user login %q, want 'akash-taurus'", prs[0].User.Login)
	}
}

func TestCreateRepoRequest_JSON(t *testing.T) {
	req := CreateRepoRequest{
		Name:        "awesome-project",
		Description: "A great project",
		Private:     true,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal CreateRepoRequest: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to unmarshal back: %v", err)
	}

	if m["name"] != "awesome-project" {
		t.Errorf("expected name 'awesome-project', got %v", m["name"])
	}
	if m["private"] != true {
		t.Errorf("expected private true, got %v", m["private"])
	}
}
