package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newTestRepo creates a bare "remote" and a work repo with one commit.
func newTestRepo(t *testing.T) (workDir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}
	root := t.TempDir()
	workDir = filepath.Join(root, "work")
	gitTestRun(t, "", "init", "-b", "main", workDir)
	// Deterministic behavior across developer machines: no CRLF rewriting,
	// and allow empty commits for history-focused tests.
	gitTestRun(t, workDir, "config", "core.autocrlf", "false")
	gitTestRun(t, workDir, "config", "commit.gpgsign", "false")
	gitTestRun(t, workDir, "config", "user.email", "test@example.com")
	gitTestRun(t, workDir, "config", "user.name", "Test User")
	writeFile := func(name, content string) {
		if err := os.WriteFile(filepath.Join(workDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	writeFile("a.txt", "line1\nline2\nline3\n")
	writeFile("b.txt", "hello\n")
	gitTestRun(t, workDir, "add", "-A")
	gitTestRun(t, workDir, "commit", "-m", "initial commit")
	return workDir
}

func commitAll(t *testing.T, dir, msg string) {
	t.Helper()
	gitTestRun(t, dir, "add", "-A")
	out, err := exec.Command("git", "-C", dir, "commit", "--allow-empty", "-m", msg).CombinedOutput()
	if err != nil {
		t.Fatalf("git commit -m %s failed: %v\n%s", msg, err, out)
	}
}

func TestStageUnstageFile(t *testing.T) {
	dir := newTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("brand new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Stage a modified tracked file.
	if err := GitStageFile(dir, "a.txt"); err != nil {
		t.Fatalf("stage a.txt: %v", err)
	}
	st, _ := GitStatusDetailed(dir)
	if st.StagedCount == 0 {
		t.Fatal("expected staged change after GitStageFile")
	}

	// Stage the untracked file.
	if err := GitStageFile(dir, "new.txt"); err != nil {
		t.Fatalf("stage new.txt: %v", err)
	}
	st, _ = GitStatusDetailed(dir)
	if st.StagedCount < 2 {
		t.Fatalf("expected >=2 staged files, got %d", st.StagedCount)
	}

	// Unstage both.
	if err := GitUnstageFile(dir, "a.txt"); err != nil {
		t.Fatalf("unstage a.txt: %v", err)
	}
	if err := GitUnstageFile(dir, "new.txt"); err != nil {
		t.Fatalf("unstage new.txt (added file): %v", err)
	}
	st, _ = GitStatusDetailed(dir)
	if st.StagedCount != 0 {
		t.Fatalf("expected 0 staged files, got %d", st.StagedCount)
	}
	// Working tree changes must survive unstaging.
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); err != nil {
		t.Fatal("unstage removed the working-tree file")
	}
}

func TestUnstageAll(t *testing.T) {
	dir := newTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "c.txt"), []byte("c\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = GitStageFile(dir, "a.txt")
	_ = GitStageFile(dir, "c.txt")
	if err := GitUnstageAll(dir); err != nil {
		t.Fatalf("unstage all: %v", err)
	}
	st, _ := GitStatusDetailed(dir)
	if st.StagedCount != 0 {
		t.Fatalf("expected 0 staged after unstage all, got %d", st.StagedCount)
	}
}

func TestDiscardFile(t *testing.T) {
	dir := newTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("doomed edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := GitDiscardFile(dir, "a.txt"); err != nil {
		t.Fatalf("discard a.txt: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	if string(data) != "line1\nline2\nline3\n" {
		t.Fatalf("a.txt not restored: %q", string(data))
	}

	// Discard an untracked file removes it.
	if err := os.WriteFile(filepath.Join(dir, "junk.txt"), []byte("junk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := GitDiscardFile(dir, "junk.txt"); err != nil {
		t.Fatalf("discard junk.txt: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "junk.txt")); !os.IsNotExist(err) {
		t.Fatal("junk.txt should have been deleted")
	}
}

func TestBranches(t *testing.T) {
	dir := newTestRepo(t)
	if err := GitCreateBranch(dir, "feature/x", true); err != nil {
		t.Fatalf("create+switch: %v", err)
	}
	if got := GitBranchName(dir); got != "feature/x" {
		t.Fatalf("branch = %q, want feature/x", got)
	}
	commitAll(t, dir, "work on feature")

	// Switch back to main.
	if _, err := GitCheckout(dir, "main", false); err != nil {
		t.Fatalf("checkout main: %v", err)
	}
	if got := GitBranchName(dir); got != "main" {
		t.Fatalf("branch = %q, want main", got)
	}
	branches, err := GitListBranches(dir)
	if err != nil {
		t.Fatalf("list branches: %v", err)
	}
	if len(branches) < 2 {
		t.Fatalf("expected >=2 branches, got %d", len(branches))
	}
	if !branches[0].IsHead || branches[0].Name != "main" {
		t.Fatalf("current branch not first: %+v", branches[0])
	}

	// feature/x is a LOCAL branch whose name contains a slash. It must not be
	// reported as remote (regression: remote detection used to look for "/"
	// in the short name, which blocked checkout/delete of such branches).
	var feat *BranchInfo
	for i := range branches {
		if branches[i].Name == "feature/x" {
			feat = &branches[i]
		}
	}
	if feat == nil {
		t.Fatalf("feature/x missing from branch list: %+v", branches)
	}
	if feat.IsRemote {
		t.Errorf("feature/x classified as remote; want local (IsRemote=false)")
	}

	// Delete the feature branch. It holds an unmerged commit relative to
	// main, so the safe -d refuses — force is required (and expected).
	if err := GitDeleteBranch(dir, "feature/x", true); err != nil {
		t.Fatalf("force delete branch: %v", err)
	}
	// Cannot delete the checked-out branch.
	if err := GitDeleteBranch(dir, "main", false); err == nil {
		t.Fatal("deleting checked-out branch should fail")
	}
}

func TestAmend(t *testing.T) {
	dir := newTestRepo(t)
	commitAll(t, dir, "to be amended")

	// Amend message only.
	if _, err := GitAmend(dir, "amended message", false); err != nil {
		t.Fatalf("amend: %v", err)
	}
	logs, _ := GitLog(dir, 1)
	if len(logs) != 1 || logs[0].Subject != "amended message" {
		t.Fatalf("subject after amend = %+v", logs)
	}

	// Amend with staged changes folded in.
	if err := os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("extra\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = GitStageAll(dir)
	if _, err := GitAmend(dir, "", true); err != nil {
		t.Fatalf("amend with staged: %v", err)
	}
	st, _ := GitStatusDetailed(dir)
	if !st.IsClean {
		t.Fatalf("expected clean tree after amend --no-edit with staged, got %+v", st)
	}
}

func TestRevert(t *testing.T) {
	dir := newTestRepo(t)
	commitAll(t, dir, "add file to revert")
	if err := os.WriteFile(filepath.Join(dir, "temp.txt"), []byte("temp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, dir, "commit temp")
	logs, _ := GitLog(dir, 1)
	sha := logs[0].Hash

	if _, err := GitRevert(dir, sha); err != nil {
		t.Fatalf("revert: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "temp.txt")); !os.IsNotExist(err) {
		t.Fatal("temp.txt should be gone after revert")
	}
}

func TestReflog(t *testing.T) {
	dir := newTestRepo(t)
	commitAll(t, dir, "second commit")
	items, err := GitReflog(dir, 10)
	if err != nil {
		t.Fatalf("reflog: %v", err)
	}
	if len(items) < 2 {
		t.Fatalf("expected >=2 reflog entries, got %d", len(items))
	}
	if items[0].Index != 1 || items[0].Desc == "" || items[0].Hash == "" {
		t.Fatalf("reflog entry malformed: %+v", items[0])
	}
	// --date=relative renders the selector date as e.g. "10 seconds ago".
	if items[0].Date == "" {
		t.Fatalf("reflog date missing: %+v", items[0])
	}
}

func TestStash(t *testing.T) {
	dir := newTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("wip changes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := GitStashPush(dir, "wip", false); err != nil {
		t.Fatalf("stash push: %v", err)
	}
	st, _ := GitStatusDetailed(dir)
	if !st.IsClean {
		t.Fatal("tree should be clean after stash push")
	}
	items, err := GitStashList(dir)
	if err != nil || len(items) != 1 {
		t.Fatalf("stash list = %v, %v", items, err)
	}
	if items[0].Index != 0 || !strings.Contains(items[0].Desc, "wip") {
		t.Fatalf("stash item malformed: %+v", items[0])
	}
	// Pop restores the change.
	if _, err := GitStashPop(dir, 0, false); err != nil {
		t.Fatalf("stash pop: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	if string(data) != "wip changes\n" {
		t.Fatalf("stash pop did not restore content: %q", string(data))
	}
	if items, _ := GitStashList(dir); len(items) != 0 {
		t.Fatal("stash should be empty after pop")
	}
	// Drop path: push then drop.
	_, _ = GitStashPush(dir, "again", false)
	if _, err := GitStashDrop(dir, 0); err != nil {
		t.Fatalf("stash drop: %v", err)
	}
	if items, _ := GitStashList(dir); len(items) != 0 {
		t.Fatal("stash should be empty after drop")
	}
}

func TestRemotes(t *testing.T) {
	dir := newTestRepo(t)
	if err := GitSetRemote(dir, "upstream", "https://example.com/upstream.git"); err != nil {
		t.Fatalf("set remote: %v", err)
	}
	remotes, err := GitListRemotes(dir)
	if err != nil {
		t.Fatalf("list remotes: %v", err)
	}
	found := map[string]string{}
	for _, r := range remotes {
		found[r.Name] = r.URL
	}
	if found["upstream"] != "https://example.com/upstream.git" {
		t.Fatalf("upstream remote missing: %+v", remotes)
	}
	if err := GitRemoveRemote(dir, "upstream"); err != nil {
		t.Fatalf("remove remote: %v", err)
	}
	if remotes, _ := GitListRemotes(dir); len(remotes) != 0 {
		t.Fatal("remote should be gone")
	}
}

func TestFetchPrune(t *testing.T) {
	dir := newTestRepo(t)
	// No remotes configured: fetch --all --prune must still succeed (no-op).
	if _, err := GitFetchPrune(dir); err != nil {
		t.Fatalf("fetch prune with no remotes: %v", err)
	}
}

func TestClone(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}
	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	gitTestRun(t, "", "init", "-b", "main", seed)
	gitTestRun(t, seed, "config", "core.autocrlf", "false")
	gitTestRun(t, seed, "config", "user.email", "t@e.com")
	gitTestRun(t, seed, "config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(seed, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, seed, "add", "-A")
	gitTestRun(t, seed, "commit", "-m", "c1")

	parent := filepath.Join(root, "clones")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := GitClone(parent, seed, "")
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	if filepath.Base(got) != "seed" {
		t.Fatalf("clone dir = %q, want seed", got)
	}
	if data, err := os.ReadFile(filepath.Join(got, "f.txt")); err != nil || string(data) == "" {
		t.Fatalf("cloned content wrong: %v %q", err, string(data))
	}
	// Refuses to overwrite an existing directory.
	if _, err := GitClone(parent, seed, "seed"); err == nil {
		t.Fatal("clone into existing dir should fail")
	}
}

func TestSyncAll(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}
	root := t.TempDir()
	remote := filepath.Join(root, "origin.git")
	gitTestRun(t, "", "init", "--bare", "-b", "main", remote)

	repoA := filepath.Join(root, "repoA")
	gitTestRun(t, "", "init", "-b", "main", repoA)
	gitTestRun(t, repoA, "config", "user.email", "t@e.com")
	gitTestRun(t, repoA, "config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(repoA, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, repoA, "add", "-A")
	gitTestRun(t, repoA, "commit", "-m", "c1")
	gitTestRun(t, repoA, "remote", "add", "origin", remote)
	gitTestRun(t, repoA, "push", "-u", "origin", "main")

	// repoB has no remote/upstream.
	repoB := filepath.Join(root, "repoB")
	gitTestRun(t, "", "init", "-b", "main", repoB)
	gitTestRun(t, repoB, "config", "user.email", "t@e.com")
	gitTestRun(t, repoB, "config", "user.name", "T")

	results := GitSyncAll(root, 1)
	byName := map[string]SyncResult{}
	for _, r := range results {
		byName[r.Name] = r
	}
	if r, ok := byName["repoA"]; !ok || r.Err != nil || r.Skipped {
		t.Fatalf("repoA sync = %+v", r)
	}
	if r, ok := byName["repoB"]; !ok || !r.Skipped {
		t.Fatalf("repoB should be skipped (no upstream): %+v", r)
	}
}

func TestFileHistory(t *testing.T) {
	dir := newTestRepo(t)
	commitAll(t, dir, "second")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("line1 v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, dir, "touch a.txt again")
	items, err := GitFileHistory(dir, "a.txt", 10)
	if err != nil {
		t.Fatalf("file history: %v", err)
	}
	if len(items) < 2 {
		t.Fatalf("expected >=2 commits for a.txt, got %d", len(items))
	}
	if items[0].Subject != "touch a.txt again" {
		t.Fatalf("newest commit = %+v", items[0])
	}
}

func TestBlame(t *testing.T) {
	dir := newTestRepo(t)
	lines, err := GitBlame(dir, "a.txt")
	if err != nil {
		t.Fatalf("blame: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 blame lines, got %d", len(lines))
	}
	if lines[0].LineNumber != 1 || lines[2].LineNumber != 3 {
		t.Fatalf("line numbers wrong: %+v", lines)
	}
	if lines[0].Author != "Test User" {
		t.Fatalf("author = %q", lines[0].Author)
	}
	if lines[0].Hash == "" {
		t.Fatal("hash missing on blame line")
	}
}

func TestDiffRangeAndCommit(t *testing.T) {
	dir := newTestRepo(t)
	logs, _ := GitLog(dir, 1)
	base := logs[0].Hash
	// Make a real change so the second commit has content to diff.
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("hello v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, dir, "second commit")

	d, err := GitDiffRange(dir, base, "HEAD")
	if err != nil {
		t.Fatalf("diff range: %v", err)
	}
	if !strings.Contains(d, "b.txt") {
		t.Fatalf("range diff missing b.txt: %q", d)
	}

	d, err = GitDiffCommit(dir, "HEAD")
	if err != nil {
		t.Fatalf("diff commit: %v", err)
	}
	if !strings.Contains(d, "b.txt") {
		t.Fatalf("commit diff missing b.txt: %q", d)
	}
}

func TestSplitHunksAndStageHunk(t *testing.T) {
	dir := newTestRepo(t)
	// Two separate regions of the same file.
	content := "ONE\nline2\nline3\n"
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	extra := strings.Repeat("pad\n", 15) // distance the second edit
	if err := os.WriteFile(filepath.Join(dir, "pad.txt"), []byte(extra), 0o644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, dir, "add pad")

	edited := "ONE-edit\nline2\nline3\n" + extra + "TAIL-edit\n"
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	// a.txt now: modified top + appended bottom (pad.txt content lives in
	// a separate file; append to a.txt itself).
	f, _ := os.OpenFile(filepath.Join(dir, "a.txt"), os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString("TAIL2\n")
	_ = f.Close()

	hunks, err := GitSplitHunks(dir, "a.txt")
	if err != nil {
		t.Fatalf("split hunks: %v", err)
	}
	if len(hunks) < 1 {
		t.Fatal("expected at least one hunk")
	}
	for i, h := range hunks {
		if !strings.HasPrefix(h.Header, "@@") {
			t.Fatalf("hunk %d header invalid: %q", i, h.Header)
		}
	}
	// Stage only the first hunk.
	if err := GitStageHunk(dir, "a.txt", 0); err != nil {
		t.Fatalf("stage hunk 0: %v", err)
	}
	st, _ := GitStatusDetailed(dir)
	if st.StagedCount == 0 {
		t.Fatal("expected the hunk to be staged")
	}
}

func TestMergeRebaseConflicts(t *testing.T) {
	dir := newTestRepo(t)
	// Create a divergent branch.
	if err := GitCreateBranch(dir, "side", true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("side version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, dir, "side change")
	if _, err := GitCheckout(dir, "main", false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("main version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, dir, "main change")

	// Conflicting merge: both modified a.txt differently.
	out, err := GitMerge(dir, "side", false, false)
	if err == nil {
		t.Fatalf("expected conflict, merged cleanly: %q", out)
	}
	conflicts, err := GitConflictFiles(dir)
	if err != nil {
		t.Fatalf("conflict files: %v", err)
	}
	if len(conflicts) == 0 {
		t.Fatal("expected conflict entries")
	}
	if conflicts[0].Path != "a.txt" {
		t.Fatalf("conflict path = %q", conflicts[0].Path)
	}
	if !GitHasConflicts(dir) {
		t.Fatal("GitHasConflicts should be true")
	}
	// Abort restores pre-merge state.
	if _, err := GitAbortCurrent(dir, "merge"); err != nil {
		t.Fatalf("abort merge: %v", err)
	}
	if GitHasConflicts(dir) {
		t.Fatal("conflicts should be gone after abort")
	}

	// Rebase path: make side diverge more, then rebase side onto main.
	// Both branches edit a.txt, so the rebase conflicts — assert that, then
	// abort to verify the rebase-abort path too.
	if _, err := GitCheckout(dir, "side", false); err != nil {
		t.Fatal(err)
	}
	if _, err := GitRebase(dir, "main"); err == nil {
		t.Fatal("expected conflicting rebase to fail")
	}
	if !GitHasConflicts(dir) {
		t.Fatal("expected conflicts during rebase")
	}
	if _, err := GitAbortCurrent(dir, "rebase"); err != nil {
		t.Fatalf("abort rebase: %v", err)
	}
	if GitHasConflicts(dir) {
		t.Fatal("conflicts should be gone after rebase abort")
	}

	// A clean rebase: from main, branch with an independent file rebases
	// onto main (side's a.txt edits stay out of this branch).
	if _, err := GitCheckout(dir, "main", false); err != nil {
		t.Fatal(err)
	}
	if err := GitCreateBranch(dir, "clean", true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "only-side.txt"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, dir, "side only file")
	if _, err := GitRebase(dir, "main"); err != nil {
		t.Fatalf("clean rebase: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "only-side.txt")); err != nil {
		t.Fatal("side file missing after rebase")
	}
}

// A file that is staged AND then modified again must appear once in Files
// (porcelain "MM"), not twice.
func TestStatusDetailedOneRowPerFile(t *testing.T) {
	dir := newTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := GitStageFile(dir, "a.txt"); err != nil {
		t.Fatalf("stage: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("staged\nthen more\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := GitStatusDetailed(dir)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	var rows []FileStatusItem
	for _, f := range st.Files {
		if f.Path == "a.txt" {
			rows = append(rows, f)
		}
	}
	if len(rows) != 1 {
		t.Fatalf("a.txt appeared %d times in Files, want 1: %+v", len(rows), st.Files)
	}
	if rows[0].Status != "MM" || !rows[0].Staged {
		t.Fatalf("merged row = %+v, want status MM and Staged=true", rows[0])
	}
	if st.StagedCount != 1 || st.UnstagedCount != 1 {
		t.Fatalf("counts = staged %d / unstaged %d, want 1/1", st.StagedCount, st.UnstagedCount)
	}
}

// GitReset must actually move HEAD for every mode. Regression: the argument
// list contained a "--" separator, which made git treat the target as a
// pathspec — --mixed silently reset nothing and --soft/--hard failed with
// "Cannot do <mode> reset with paths".
func TestResetModesMoveHEAD(t *testing.T) {
	dir := newTestRepo(t)
	commitAll(t, dir, "second")
	commitAll(t, dir, "third")

	countCommits := func() int {
		t.Helper()
		logs, err := GitLog(dir, 50)
		if err != nil {
			t.Fatalf("git log: %v", err)
		}
		return len(logs)
	}
	if got := countCommits(); got != 3 {
		t.Fatalf("precondition: %d commits, want 3", got)
	}

	if _, err := GitReset(dir, "HEAD~2", ResetHard); err != nil {
		t.Fatalf("reset --hard HEAD~2: %v", err)
	}
	if got := countCommits(); got != 1 {
		t.Fatalf("after reset --hard HEAD~2: %d commits, want 1", got)
	}

	commitAll(t, dir, "fourth")
	if _, err := GitReset(dir, "HEAD~1", ResetSoft); err != nil {
		t.Fatalf("reset --soft HEAD~1: %v", err)
	}
	if got := countCommits(); got != 1 {
		t.Fatalf("after reset --soft HEAD~1: %d commits, want 1", got)
	}

	commitAll(t, dir, "fifth")
	if _, err := GitReset(dir, "HEAD~1", ResetMixed); err != nil {
		t.Fatalf("reset --mixed HEAD~1: %v", err)
	}
	if got := countCommits(); got != 1 {
		t.Fatalf("after reset --mixed HEAD~1: %d commits, want 1", got)
	}

	// A flag-like target must be refused, never passed through to git.
	if _, err := GitReset(dir, "-x", ResetMixed); err == nil {
		t.Fatal("reset with a flag-like target should be refused")
	}
}
