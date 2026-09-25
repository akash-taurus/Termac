package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// makeRepos builds n repos with names offset by start so entries are distinct.
func makeRepos(n, start int) []Repo {
	repos := make([]Repo, n)
	for i := range repos {
		repos[i] = Repo{Name: fmt.Sprintf("repo-%d", start+i), FullName: fmt.Sprintf("user/repo-%d", start+i)}
	}
	return repos
}

// GetRepositoriesAll must follow pagination until a short page is returned.
func TestGetRepositoriesAll_Paginates(t *testing.T) {
	orig := apiBaseURL
	defer func() { apiBaseURL = orig }()

	hits := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		hits[page]++
		w.Header().Set("Content-Type", "application/json")
		switch page {
		case "1":
			_ = json.NewEncoder(w).Encode(makeRepos(100, 0))
		case "2":
			_ = json.NewEncoder(w).Encode(makeRepos(25, 100))
		default:
			_ = json.NewEncoder(w).Encode([]Repo{})
		}
	}))
	defer srv.Close()
	apiBaseURL = srv.URL

	got, err := NewClient(nil).GetRepositoriesAll(500)
	if err != nil {
		t.Fatalf("GetRepositoriesAll failed: %v", err)
	}
	if len(got) != 125 {
		t.Fatalf("got %d repos, want 125", len(got))
	}
	if hits["2"] != 1 {
		t.Fatalf("expected page 2 to be fetched once, got %d", hits["2"])
	}
	// A short page ends pagination: page 3 must never be requested.
	if hits["3"] != 0 {
		t.Fatalf("expected pagination to stop after a short page, page 3 fetched %d time(s)", hits["3"])
	}
}

// maxRepos must stop the walk early without fetching further pages.
func TestGetRepositoriesAll_RespectsCap(t *testing.T) {
	orig := apiBaseURL
	defer func() { apiBaseURL = orig }()

	hits := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		hits[page]++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(makeRepos(100, 0))
	}))
	defer srv.Close()
	apiBaseURL = srv.URL

	got, err := NewClient(nil).GetRepositoriesAll(100)
	if err != nil {
		t.Fatalf("GetRepositoriesAll failed: %v", err)
	}
	if len(got) != 100 {
		t.Fatalf("got %d repos, want 100", len(got))
	}
	if hits["2"] != 0 {
		t.Fatalf("cap reached but page 2 was fetched %d time(s)", hits["2"])
	}
}

// A failure on the first page yields no repos and the error.
func TestGetRepositoriesAll_FirstPageError(t *testing.T) {
	orig := apiBaseURL
	defer func() { apiBaseURL = orig }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()
	apiBaseURL = srv.URL

	got, err := NewClient(nil).GetRepositoriesAll(500)
	if err == nil {
		t.Fatal("expected error")
	}
	if len(got) != 0 {
		t.Fatalf("expected no repos on first-page error, got %d", len(got))
	}
}
