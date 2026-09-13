package auth

import (
	"os"
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
	os.Setenv("GITHUB_TOKEN", "ghp_test_mock_token_env")
	defer os.Unsetenv("GITHUB_TOKEN")

	tok, err := LoadToken()
	if err != nil {
		t.Fatalf("expected LoadToken to succeed from env, got: %v", err)
	}
	if tok.AccessToken != "ghp_test_mock_token_env" {
		t.Errorf("got access token %q, want %q", tok.AccessToken, "ghp_test_mock_token_env")
	}
}
