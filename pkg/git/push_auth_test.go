package git

import (
	"strings"
	"testing"
)

func TestIsGitAuthFailure(t *testing.T) {
	authCases := []string{
		"remote: Invalid username or password.\nfatal: Authentication failed for 'https://github.com/o/r.git/'",
		"fatal: could not read Username for 'https://github.com': No such device or address",
		"ERROR: Permission denied (publickey).\nfatal: Could not read from remote repository.",
		"remote: Permission to org/repo.git denied to user.",
		"fatal: unable to access 'https://github.com/o/r.git/': The requested URL returned error: 403",
		"fatal: unable to access 'https://github.com/o/r.git/': The requested URL returned error: 401",
		"remote: Bad credentials",
		"remote: Your token has expired. Please re-authenticate.",
	}
	for _, out := range authCases {
		if !IsGitAuthFailure(out) {
			t.Errorf("expected auth failure for %q", firstLine(out))
		}
	}

	nonAuthCases := []string{
		"Everything up-to-date",
		"To https://github.com/o/r.git\n   abc1234..def5678  main -> main",
		"error: failed to push some refs to 'https://github.com/o/r.git'\nhint: Updates were rejected because the remote contains work that you do not have locally",
		"fatal: unable to connect to github.com: Connection timed out",
		"fatal: not a git repository (or any of the parent directories): .git",
		"CONFLICT (content): Merge conflict in main.go",
		"",
	}
	for _, out := range nonAuthCases {
		if IsGitAuthFailure(out) {
			t.Errorf("expected non-auth output for %q", firstLine(out))
		}
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func TestEmbedTokenInHTTPSURL(t *testing.T) {
	const token = "ghp_test123"

	got := EmbedTokenInHTTPSURL("https://github.com/org/repo.git", token)
	want := "https://x-access-token:ghp_test123@github.com/org/repo.git"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// Only the scheme prefix is rewritten, never later occurrences.
	got = EmbedTokenInHTTPSURL("https://github.com/org/https://x.git", token)
	if !strings.HasPrefix(got, "https://x-access-token:ghp_test123@github.com/") {
		t.Errorf("unexpected rewrite: %q", got)
	}

	unchanged := []struct{ url, token string }{
		{"git@github.com:org/repo.git", token},  // SSH untouched
		{"/local/path/repo", token},             // local path untouched
		{"", token},                             // empty URL untouched
		{"https://github.com/org/repo.git", ""}, // empty token untouched
	}
	for _, tc := range unchanged {
		if got := EmbedTokenInHTTPSURL(tc.url, tc.token); got != tc.url {
			t.Errorf("EmbedTokenInHTTPSURL(%q, %q) = %q, want unchanged", tc.url, tc.token, got)
		}
	}
}
