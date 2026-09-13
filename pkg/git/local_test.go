package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitActions_Lifecycle(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "git_action_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 1. Initially not a git repo
	if IsGitRepository(tempDir) {
		t.Errorf("expected tempDir to not be a git repo yet")
	}

	statusNonGit, err := GitStatusDetailed(tempDir)
	if err != nil {
		t.Fatalf("GitStatusDetailed on non-git dir returned error: %v", err)
	}
	if statusNonGit.IsGitRepo {
		t.Errorf("expected IsGitRepo to be false")
	}

	// 2. GitInit
	if err := GitInit(tempDir); err != nil {
		t.Fatalf("GitInit failed: %v", err)
	}
	if !IsGitRepository(tempDir) {
		t.Errorf("expected tempDir to be a git repo after GitInit")
	}

	// Configure git user in tempDir so git commit works in CI/testing
	_ = os.WriteFile(filepath.Join(tempDir, "README.md"), []byte("# Hello World\n"), 0644)

	// 3. Stage and Commit
	if err := GitStageAll(tempDir); err != nil {
		t.Fatalf("GitStageAll failed: %v", err)
	}

	statusAfterStage, err := GitStatusDetailed(tempDir)
	if err != nil {
		t.Fatalf("GitStatusDetailed failed: %v", err)
	}
	if statusAfterStage.StagedCount != 1 {
		t.Errorf("expected 1 staged file, got %d", statusAfterStage.StagedCount)
	}

	// Commit with config env
	commitOut, err := GitCommit(tempDir, "initial commit")
	if err != nil {
		// If git identity is not configured globally, configure local and retry
		if strings.Contains(err.Error(), "identity") || strings.Contains(err.Error(), "tell me who you are") {
			cmd := GitInit(tempDir)
			_ = cmd
			// set local config
			f, _ := os.OpenFile(filepath.Join(tempDir, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0644)
			if f != nil {
				_, _ = f.WriteString("[user]\n\tname = Test User\n\temail = test@example.com\n")
				_ = f.Close()
			}
			commitOut, err = GitCommit(tempDir, "initial commit")
		}
	}
	if err != nil {
		t.Fatalf("GitCommit failed: %v", err)
	}
	if commitOut == "" {
		t.Errorf("expected commit output to not be empty")
	}

	// 4. Modify file and check Diff & Status
	_ = os.WriteFile(filepath.Join(tempDir, "README.md"), []byte("# Hello World Modified\n"), 0644)
	diff, err := GitDiff(tempDir)
	if err != nil {
		t.Fatalf("GitDiff failed: %v", err)
	}
	if !strings.Contains(diff, "Modified") {
		t.Errorf("expected diff to contain 'Modified', got %q", diff)
	}

	statusMod, err := GitStatusDetailed(tempDir)
	if err != nil {
		t.Fatalf("GitStatusDetailed failed: %v", err)
	}
	if statusMod.UnstagedCount != 1 {
		t.Errorf("expected 1 unstaged change, got %d", statusMod.UnstagedCount)
	}

	// 5. GitLog
	logs, err := GitLog(tempDir, 5)
	if err != nil {
		t.Fatalf("GitLog failed: %v", err)
	}
	if len(logs) == 0 {
		t.Errorf("expected at least 1 commit log, got 0")
	}
	if logs[0].Subject != "initial commit" {
		t.Errorf("expected subject 'initial commit', got %q", logs[0].Subject)
	}

	// 6. Test GitBranchName and GitSetRemote
	branch := GitBranchName(tempDir)
	if branch == "" {
		t.Errorf("expected non-empty branch name")
	}

	remoteURL := "https://github.com/test-user/test-repo.git"
	if err := GitSetRemote(tempDir, "origin", remoteURL); err != nil {
		t.Fatalf("GitSetRemote failed: %v", err)
	}

	// Update remote to verify set-url works
	newRemoteURL := "https://github.com/test-user/test-repo-updated.git"
	if err := GitSetRemote(tempDir, "origin", newRemoteURL); err != nil {
		t.Fatalf("GitSetRemote update failed: %v", err)
	}
}
