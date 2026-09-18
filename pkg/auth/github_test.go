package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestToken_IsExpired(t *testing.T) {
	// Zero expiry (PAT) should never expire
	pat := &Token{AccessToken: "ghp_12345"}
	if pat.IsExpired() {
		t.Errorf("expected PAT with zero expiry to not be expired")
	}

	// Past expiry
	expired := &Token{
		AccessToken: "gho_old",
		Expiry:      time.Now().Add(-1 * time.Hour),
	}
	if !expired.IsExpired() {
		t.Errorf("expected past token to be expired")
	}

	// Future expiry
	future := &Token{
		AccessToken: "gho_future",
		Expiry:      time.Now().Add(1 * time.Hour),
	}
	if future.IsExpired() {
		t.Errorf("expected future token to not be expired")
	}
}

func TestLoadToken_EnvFallback(t *testing.T) {
	// Isolate from the real user config dir (%APPDATA%\Dashboard on
	// Windows, $XDG_CONFIG_HOME elsewhere) so a real github_token.json
	// on the dev machine can't shadow the env fallback, and the
	// SaveToken side-effect in LoadToken can't pollute real credentials.
	cfgDir := t.TempDir()
	t.Setenv("APPDATA", cfgDir)
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("GITHUB_TOKEN", "ghp_test_mock_token_env")

	tok, err := LoadToken()
	if err != nil {
		t.Fatalf("expected LoadToken to succeed from env, got: %v", err)
	}
	if tok.AccessToken != "ghp_test_mock_token_env" {
		t.Errorf("got access token %q, want %q", tok.AccessToken, "ghp_test_mock_token_env")
	}
}

func TestValidateTokenFormat(t *testing.T) {
	if err := ValidateTokenFormat(""); err == nil {
		t.Error("expected error for empty token")
	}
	if err := ValidateTokenFormat("ghp_" + strings.Repeat("a", 36)); err != nil {
		t.Errorf("expected complete classic PAT accepted, got %v", err)
	}
	if err := ValidateTokenFormat("github_pat_" + strings.Repeat("a", 82)); err != nil {
		t.Errorf("expected complete fine-grained PAT accepted, got %v", err)
	}
	// Lengths are opaque per GitHub docs: near-length tokens must pass
	// offline validation and let the API decide (previously exact 40/93
	// rejected valid pastes).
	if err := ValidateTokenFormat("ghp_0QJ3ZuMOp1QlbvWkE7vP3oy4ET9wx1w33N"); err != nil {
		t.Errorf("expected 38-char classic PAT passed through to API, got %v", err)
	}
	if err := ValidateTokenFormat("ghp_" + strings.Repeat("a", 32)); err != nil {
		t.Errorf("expected 36-char ghp_ passed through to API, got %v", err)
	}
	// Clearly broken input still fails fast offline.
	if err := ValidateTokenFormat("github_pat_short"); err == nil {
		t.Error("expected error for truncated fine-grained PAT")
	}
	// Unknown/future formats pass through to the API, never rejected here.
	if err := ValidateTokenFormat("ghu_sometotallydifferentformat"); err != nil {
		t.Errorf("expected unknown format passed through, got %v", err)
	}

	// SaveTokenString must reject clearly broken tokens without network access.
	if _, _, err := SaveTokenString("ghp_short"); err == nil || !strings.Contains(err.Error(), "short") {
		t.Errorf("expected too-short error, got %v", err)
	}
}

func TestSaveTokenString_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer server.Close()

	oldBase := githubAPIBaseURL
	githubAPIBaseURL = server.URL
	defer func() { githubAPIBaseURL = oldBase }()

	// Complete format so the request reaches the (fake) API.
	if _, _, err := SaveTokenString("ghp_" + strings.Repeat("b", 36)); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("expected guided 401 error, got %v", err)
	}
}

func TestValidateToken(t *testing.T) {
	// Empty and expired tokens are rejected without any network call.
	if _, err := ValidateToken(nil); err == nil {
		t.Error("expected error for nil token")
	}
	if _, err := ValidateToken(&Token{}); err == nil {
		t.Error("expected error for empty token")
	}
	expired := &Token{AccessToken: "gho_old", Expiry: time.Now().Add(-time.Hour)}
	if valid, err := ValidateToken(expired); valid || err == nil {
		t.Errorf("expected expired token rejected, got valid=%v err=%v", valid, err)
	}

	// Live checks against a fake API.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") == "Bearer good-token" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"login":"tester"}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer server.Close()

	oldBase := githubAPIBaseURL
	githubAPIBaseURL = server.URL
	defer func() { githubAPIBaseURL = oldBase }()

	if valid, err := ValidateToken(&Token{AccessToken: "good-token"}); !valid || err != nil {
		t.Errorf("expected good token accepted, got valid=%v err=%v", valid, err)
	}
	if valid, err := ValidateToken(&Token{AccessToken: "revoked-token"}); valid || err != nil {
		t.Errorf("expected revoked token rejected with (false, nil), got valid=%v err=%v", valid, err)
	}
}

func TestGetTokenScopes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer classic-with-repo":
			w.Header().Set("X-OAuth-Scopes", "repo, read:org, user")
			w.WriteHeader(http.StatusOK)
		case "Bearer classic-read-only":
			w.Header().Set("X-OAuth-Scopes", "read:org, user")
			w.WriteHeader(http.StatusOK)
		case "Bearer fine-grained":
			// No scopes header, like real fine-grained PATs.
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer server.Close()

	oldBase := githubAPIBaseURL
	githubAPIBaseURL = server.URL
	defer func() { githubAPIBaseURL = oldBase }()

	scopes, present, err := GetTokenScopes(&Token{AccessToken: "classic-with-repo"})
	if err != nil || !present || !HasRepoCreateScope(scopes, true) {
		t.Errorf("expected repo scope present, got %v present=%v err=%v", scopes, present, err)
	}
	if !HasRepoCreateScope(scopes, false) {
		t.Errorf("expected repo scope to cover public creates, got %v", scopes)
	}

	scopes, present, err = GetTokenScopes(&Token{AccessToken: "classic-read-only"})
	if err != nil || !present {
		t.Fatalf("expected scopes header present, got %v present=%v err=%v", scopes, present, err)
	}
	if HasRepoCreateScope(scopes, true) {
		t.Errorf("read-only scopes must not allow private creates: %v", scopes)
	}
	if HasRepoCreateScope(scopes, false) {
		t.Errorf("read-only scopes must not allow public creates: %v", scopes)
	}

	// public_repo covers public creates but not private ones.
	publicOnly := []string{"public_repo", "read:user"}
	if !HasRepoCreateScope(publicOnly, false) {
		t.Errorf("public_repo should allow public creates: %v", publicOnly)
	}
	if HasRepoCreateScope(publicOnly, true) {
		t.Errorf("public_repo must not allow private creates: %v", publicOnly)
	}

	// Scope matching is case-insensitive and trims spaces.
	if !HasRepoCreateScope([]string{" Repo "}, true) {
		t.Error("expected case/space-insensitive scope match")
	}

	_, _, err = GetTokenScopes(&Token{AccessToken: "fine-grained"})
	if err != nil {
		t.Fatalf("absent scopes header must not error, got %v", err)
	}
	if _, present, _ := GetTokenScopes(&Token{AccessToken: "fine-grained"}); present {
		t.Error("absent scopes header must report present=false")
	}

	if _, _, err := GetTokenScopes(&Token{AccessToken: "bogus"}); err == nil {
		t.Error("expected error for 401 token")
	}
	if _, _, err := GetTokenScopes(nil); err == nil {
		t.Error("expected error for nil token")
	}
}
