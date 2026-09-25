package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitTestRun runs a git command in dir and fails the test on error.
func gitTestRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestGitPushUpstreamAuth_UsesRemoteNameAndHidesToken guards the regression
// where the token was embedded in the push URL. Embedding it both widened the
// secret's exposure (it appears in the URL passed to git) and made `-u` record
// the URL, rather than the remote name, as the branch upstream — which breaks
// later plain `git push` / `git pull`.
func TestGitPushUpstreamAuth_UsesRemoteNameAndHidesToken(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	root := t.TempDir()
	remoteDir := filepath.Join(root, "remote.git")
	gitTestRun(t, "", "init", "--bare", remoteDir)

	workDir := filepath.Join(root, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir work: %v", err)
	}
	gitTestRun(t, workDir, "init")
	gitTestRun(t, workDir, "config", "user.email", "test@example.com")
	gitTestRun(t, workDir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(workDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitTestRun(t, workDir, "add", "-A")
	gitTestRun(t, workDir, "commit", "-m", "initial commit")
	gitTestRun(t, workDir, "remote", "add", "origin", remoteDir)
	gitTestRun(t, workDir, "branch", "-M", "main")

	const token = "ghp_ThisIsASecretToken1234567890"
	if _, err := GitPushUpstreamAuth(workDir, "origin", "main", token); err != nil {
		t.Fatalf("GitPushUpstreamAuth failed: %v", err)
	}

	// The upstream must reference the remote NAME, not a credentialed URL.
	upstream := gitTestRun(t, workDir, "config", "--get", "branch.main.remote")
	if upstream != "origin" {
		t.Fatalf("branch.main.remote = %q, want %q", upstream, "origin")
	}

	// The token must never be persisted anywhere in git config.
	cfg := gitTestRun(t, workDir, "config", "--list")
	if strings.Contains(cfg, token) {
		t.Fatalf("token leaked into git config: %s", cfg)
	}
}
