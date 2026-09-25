package github

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPullRequestDiff verifies the diff media-type request and body pass-through.
func TestPullRequestDiff(t *testing.T) {
	var gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		if r.URL.Path != "/repos/o/r/pulls/7" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Write([]byte("diff --git a/f b/f\n+++ b/f\n@@ -1 +1 @@\n-a\n+b\n"))
	}))
	defer srv.Close()
	apiBaseURL = srv.URL
	defer func() { apiBaseURL = "https://api.github.com" }()

	c := NewClient(nil)
	d, err := c.PullRequestDiff("o", "r", 7)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if gotAccept != "application/vnd.github.v3.diff" {
		t.Fatalf("Accept = %q", gotAccept)
	}
	if len(d) == 0 || d[0] != 'd' {
		t.Fatalf("diff body = %q", d)
	}
}

// TestSetPullRequestState covers merge (PUT) and close (PATCH) request shapes.
func TestSetPullRequestState(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		switch r.Method {
		case http.MethodPut:
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"merged":true}`))
		case http.MethodPatch:
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"state":"closed"}`))
		}
	}))
	defer srv.Close()
	apiBaseURL = srv.URL
	defer func() { apiBaseURL = "https://api.github.com" }()

	c := NewClient(nil)
	res, err := c.SetPullRequestState("o", "r", 3, PRActionMerge)
	if err != nil || gotMethod != http.MethodPut || res != "merged" {
		t.Fatalf("merge: res=%q err=%v method=%s", res, err, gotMethod)
	}
	if gotPath != "/repos/o/r/pulls/3/merge" {
		t.Fatalf("merge path = %q", gotPath)
	}
	res, err = c.SetPullRequestState("o", "r", 3, PRActionClose)
	if err != nil || gotMethod != http.MethodPatch || res != "closed" {
		t.Fatalf("close: res=%q err=%v method=%s", res, err, gotMethod)
	}
}

// TestSetPullRequestStateError surfaces the GitHub error message.
func TestSetPullRequestStateError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte(`{"message":"Pull Request is not mergeable"}`))
	}))
	defer srv.Close()
	apiBaseURL = srv.URL
	defer func() { apiBaseURL = "https://api.github.com" }()

	c := NewClient(nil)
	_, err := c.SetPullRequestState("o", "r", 3, PRActionMerge)
	if err == nil || err.Error() != "GitHub API error (405): Pull Request is not mergeable" {
		t.Fatalf("err = %v", err)
	}
}

// TestCreatePullRequest verifies payload marshaling and error extraction.
func TestCreatePullRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/o/r/pulls" {
			t.Errorf("req = %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"number":9,"title":"t","state":"open","html_url":"http://x/9","user":{"login":"u"}}`))
	}))
	defer srv.Close()
	apiBaseURL = srv.URL
	defer func() { apiBaseURL = "https://api.github.com" }()

	c := NewClient(nil)
	pr, err := c.CreatePullRequest("o", "r", "t", "feature", "main", "body")
	if err != nil {
		t.Fatalf("create PR: %v", err)
	}
	if pr.Number != 9 || pr.User.Login != "u" {
		t.Fatalf("pr = %+v", pr)
	}

	// Error path: validation failure with errors[] detail.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"message":"Validation Failed","errors":[{"message":"No commits between main and feature"}]}`))
	}))
	defer srv2.Close()
	apiBaseURL = srv2.URL
	if _, err := c.CreatePullRequest("o", "r", "t", "feature", "main", ""); err == nil ||
		err.Error() != "Validation Failed: No commits between main and feature" {
		t.Fatalf("err = %v", err)
	}
}

// TestListIssuesFiltersPRs ensures pull requests are dropped from /issues.
func TestListIssuesFiltersPRs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[
			{"number":1,"title":"real issue","state":"open","user":{"login":"a"}},
			{"number":2,"title":"a pr","state":"open","user":{"login":"b"},"pull_request":{"html_url":"http://x"}}
		]`))
	}))
	defer srv.Close()
	apiBaseURL = srv.URL
	defer func() { apiBaseURL = "https://api.github.com" }()

	c := NewClient(nil)
	issues, err := c.ListIssues("o", "r")
	if err != nil {
		t.Fatalf("list issues: %v", err)
	}
	if len(issues) != 1 || issues[0].Number != 1 || issues[0].Title != "real issue" {
		t.Fatalf("issues = %+v", issues)
	}
}
