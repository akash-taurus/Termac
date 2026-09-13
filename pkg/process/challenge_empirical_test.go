//go:build windows

package process

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"
)

// createPythonTreeScripts creates three Python scripts in the given directory:
// parent.py -> child.py -> grandchild.py.
// Each script writes its PID to a file and sleeps until killed.
func createPythonTreeScripts(t *testing.T, dir string) (parentScript, childScript, gcScript string) {
	t.Helper()

	parentScript = filepath.Join(dir, "parent.py")
	childScript = filepath.Join(dir, "child.py")
	gcScript = filepath.Join(dir, "grandchild.py")

	scriptTemplate := `import os, sys, time, subprocess

# argv[1] is the pid file path
pid_path = sys.argv[1]
with open(pid_path, "w") as f:
    f.write(str(os.getpid()))
    f.flush()

# If next script is provided in argv[2], spawn it with remaining args
if len(sys.argv) > 2:
    next_script = sys.argv[2]
    next_args = sys.argv[3:]
    subprocess.Popen([sys.executable, next_script] + next_args)

# Keep running until forcefully killed
while True:
    time.Sleep(1) if hasattr(time, 'Sleep') else time.sleep(1)
`

	if err := os.WriteFile(parentScript, []byte(scriptTemplate), 0600); err != nil {
		t.Fatalf("failed to write parent.py: %v", err)
	}
	if err := os.WriteFile(childScript, []byte(scriptTemplate), 0600); err != nil {
		t.Fatalf("failed to write child.py: %v", err)
	}
	if err := os.WriteFile(gcScript, []byte(scriptTemplate), 0600); err != nil {
		t.Fatalf("failed to write grandchild.py: %v", err)
	}

	return parentScript, childScript, gcScript
}

// spawnPython3TierTree launches a Python 3-tier tree and returns parentCmd and all 3 PIDs.
func spawnPython3TierTree(t *testing.T, workDir string) (parentCmd *exec.Cmd, parentPid, childPid, gcPid int) {
	t.Helper()

	parentPy, childPy, gcPy := createPythonTreeScripts(t, workDir)

	parentPidFile := filepath.Join(workDir, "parent.pid")
	childPidFile := filepath.Join(workDir, "child.pid")
	gcPidFile := filepath.Join(workDir, "grandchild.pid")

	pythonPath, err := exec.LookPath("python")
	if err != nil {
		t.Skip("python interpreter not found in PATH; skipping Python process tree test")
	}

	cmd := exec.Command(pythonPath, parentPy, parentPidFile, childPy, childPidFile, gcPy, gcPidFile)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: CREATE_NO_WINDOW,
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start python parent process: %v", err)
	}

	pPid := waitForPID(t, parentPidFile, 5*time.Second)
	cPid := waitForPID(t, childPidFile, 5*time.Second)
	gPid := waitForPID(t, gcPidFile, 5*time.Second)

	return cmd, pPid, cPid, gPid
}

// TestChallenge_Python_3TierProcessTree tests spawning a 3-tier Python process tree
// (Parent -> Child -> Grandchild), asserting all PIDs are active, killing via KillProcessTree,
// and verifying that ALL processes are terminated with zero zombie leftovers in tasklist.
func TestChallenge_Python_3TierProcessTree(t *testing.T) {
	workDir := t.TempDir()

	cmd, parentPid, childPid, gcPid := spawnPython3TierTree(t, workDir)
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = KillProcessTree(parentPid)
		_ = KillProcessTree(childPid)
		_ = KillProcessTree(gcPid)
	}()

	t.Logf("Empirical Test: Spawned Python Tree: Parent=%d -> Child=%d -> Grandchild=%d", parentPid, childPid, gcPid)

	// Verify all 3 processes are alive via OS handles
	if !isProcessAlive(parentPid) {
		t.Fatalf("expected parent PID %d to be alive", parentPid)
	}
	if !isProcessAlive(childPid) {
		t.Fatalf("expected child PID %d to be alive", childPid)
	}
	if !isProcessAlive(gcPid) {
		t.Fatalf("expected grandchild PID %d to be alive", gcPid)
	}

	// Verify all 3 processes are visible in Windows tasklist
	assertProcessInTaskList(t, parentPid, true)
	assertProcessInTaskList(t, childPid, true)
	assertProcessInTaskList(t, gcPid, true)

	// Terminate parent process tree with KillProcessTree
	startTime := time.Now()
	if err := KillProcessTree(parentPid); err != nil {
		t.Fatalf("KillProcessTree failed for parent PID %d: %v", parentPid, err)
	}
	elapsed := time.Since(startTime)
	t.Logf("KillProcessTree returned in %v", elapsed)

	// Verify parent, child, and grandchild are ALL completely dead
	assertProcessDead(t, parentPid, 5*time.Second)
	assertProcessDead(t, childPid, 5*time.Second)
	assertProcessDead(t, gcPid, 5*time.Second)

	// Verify tasklist confirms ZERO zombie or orphan processes on host system
	assertProcessInTaskList(t, parentPid, false)
	assertProcessInTaskList(t, childPid, false)
	assertProcessInTaskList(t, gcPid, false)

	t.Logf("PASS: 3-tier Python tree (PIDs %d, %d, %d) terminated completely with zero zombies in tasklist.",
		parentPid, childPid, gcPid)
}

// TestChallenge_EmpiricalContrast_GoKillVsKillProcessTree empirically proves that standard
// Go cmd.Process.Kill() leaks descendant processes as orphaned zombies, whereas
// KillProcessTree terminates the entire tree.
func TestChallenge_EmpiricalContrast_GoKillVsKillProcessTree(t *testing.T) {
	workDir := t.TempDir()

	cmd, parentPid, childPid, gcPid := spawnPython3TierTree(t, workDir)
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = KillProcessTree(parentPid)
		_ = KillProcessTree(childPid)
		_ = KillProcessTree(gcPid)
	}()

	t.Logf("Contrast Test: Initial Tree: Parent=%d, Child=%d, Grandchild=%d", parentPid, childPid, gcPid)

	// Execute standard Go (*os.Process).Kill() on parent
	t.Logf("Executing standard Go cmd.Process.Kill() on Parent %d...", parentPid)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("cmd.Process.Kill failed: %v", err)
	}
	_ = cmd.Wait()

	// Verify Parent is dead
	assertProcessDead(t, parentPid, 3*time.Second)
	assertProcessInTaskList(t, parentPid, false)

	// Check Child and Grandchild: under standard Go Kill(), both MUST still be alive!
	childAlive := isProcessAlive(childPid)
	gcAlive := isProcessAlive(gcPid)
	t.Logf("After cmd.Process.Kill(): Parent dead=true, Child alive=%v, Grandchild alive=%v", childAlive, gcAlive)

	if !childAlive || !gcAlive {
		t.Fatalf("BUG in contrast: expected child and grandchild to survive standard Go Kill, got childAlive=%v, gcAlive=%v",
			childAlive, gcAlive)
	}

	// Verify tasklist confirms child and grandchild are leaking as orphans
	assertProcessInTaskList(t, childPid, true)
	assertProcessInTaskList(t, gcPid, true)
	t.Logf("CONFIRMED LEAK: tasklist verifies Child %d and Grandchild %d survived as orphaned zombies.", childPid, gcPid)

	// Now execute KillProcessTree on the remaining tree starting at child
	t.Logf("Now executing KillProcessTree(%d) to clean up leaked orphans...", childPid)
	if err := KillProcessTree(childPid); err != nil {
		t.Fatalf("KillProcessTree failed on surviving child %d: %v", childPid, err)
	}

	// Verify both child and grandchild are now dead and purged from tasklist
	assertProcessDead(t, childPid, 5*time.Second)
	assertProcessDead(t, gcPid, 5*time.Second)
	assertProcessInTaskList(t, childPid, false)
	assertProcessInTaskList(t, gcPid, false)

	t.Logf("PASS: Contrast verified empirically. Standard Kill leaves zombies; KillProcessTree cleans them up 100%%.")
}

// TestChallenge_Batch_3TierProcessTree tests multi-tier process trees created with native Windows batch scripts.
func TestChallenge_Batch_3TierProcessTree(t *testing.T) {
	workDir := t.TempDir()

	parentBat := filepath.Join(workDir, "parent.bat")
	childBat := filepath.Join(workDir, "child.bat")
	gcBat := filepath.Join(workDir, "grandchild.bat")

	// Scripts write their PIDs using python one-liner or powershell, then sleep via ping
	parentContent := fmt.Sprintf("@echo off\r\nstart /B cmd.exe /c \"%s\"\r\nping 127.0.0.1 -n 3600 >nul\r\n", childBat)
	childContent := fmt.Sprintf("@echo off\r\npython -c \"import os; print(os.getppid())\" > \"%s\\child.pid\"\r\nstart /B cmd.exe /c \"%s\"\r\nping 127.0.0.1 -n 3600 >nul\r\n", workDir, gcBat)
	gcContent := fmt.Sprintf("@echo off\r\npython -c \"import os; print(os.getppid())\" > \"%s\\grandchild.pid\"\r\nping 127.0.0.1 -n 3600 >nul\r\n", workDir)

	if err := os.WriteFile(parentBat, []byte(parentContent), 0700); err != nil {
		t.Fatalf("failed to write parent.bat: %v", err)
	}
	if err := os.WriteFile(childBat, []byte(childContent), 0700); err != nil {
		t.Fatalf("failed to write child.bat: %v", err)
	}
	if err := os.WriteFile(gcBat, []byte(gcContent), 0700); err != nil {
		t.Fatalf("failed to write grandchild.bat: %v", err)
	}

	cmd := exec.Command("cmd.exe", "/c", parentBat)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: CREATE_NO_WINDOW,
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start parent.bat: %v", err)
	}

	parentPid := cmd.Process.Pid
	cPid := waitForPID(t, filepath.Join(workDir, "child.pid"), 5*time.Second)
	gPid := waitForPID(t, filepath.Join(workDir, "grandchild.pid"), 5*time.Second)

	defer func() {
		_ = KillProcessTree(parentPid)
		_ = KillProcessTree(cPid)
		_ = KillProcessTree(gPid)
	}()

	t.Logf("Batch Tree spawned: Parent=%d, Child=%d, Grandchild=%d", parentPid, cPid, gPid)

	// Assert alive
	if !isProcessAlive(parentPid) || !isProcessAlive(cPid) || !isProcessAlive(gPid) {
		t.Fatalf("expected all batch processes to be alive (P:%d, C:%d, GC:%d)", parentPid, cPid, gPid)
	}

	// Kill parent tree
	if err := KillProcessTree(parentPid); err != nil {
		t.Fatalf("KillProcessTree failed for batch parent %d: %v", parentPid, err)
	}

	// Verify dead
	assertProcessDead(t, parentPid, 5*time.Second)
	assertProcessDead(t, cPid, 5*time.Second)
	assertProcessDead(t, gPid, 5*time.Second)

	assertProcessInTaskList(t, parentPid, false)
	assertProcessInTaskList(t, cPid, false)
	assertProcessInTaskList(t, gPid, false)

	t.Logf("PASS: Batch tree completely destroyed with zero zombies.")
}

// TestChallenge_Concurrent10_ProcessTrees tests concurrent termination of 10 simultaneous
// multi-tier process trees (total 30 processes: 10 parents, 10 children, 10 grandchildren).
func TestChallenge_Concurrent10_ProcessTrees(t *testing.T) {
	const treeCount = 10
	type treeInfo struct {
		parentCmd *exec.Cmd
		pPid      int
		cPid      int
		gPid      int
	}

	trees := make([]treeInfo, treeCount)
	tempDirs := make([]string, treeCount)

	// Step 1: Spawn all 10 trees in parallel
	var spawnWg sync.WaitGroup
	spawnErrCh := make(chan error, treeCount)

	t.Logf("Spawning %d simultaneous 3-tier process trees (30 processes total)...", treeCount)
	for i := 0; i < treeCount; i++ {
		spawnWg.Add(1)
		go func(idx int) {
			defer spawnWg.Done()
			dir, err := os.MkdirTemp("", fmt.Sprintf("challenge_tree_%d_*", idx))
			if err != nil {
				spawnErrCh <- fmt.Errorf("failed to create temp dir %d: %w", idx, err)
				return
			}
			tempDirs[idx] = dir

			cmd, pPid, cPid, gPid := spawnPython3TierTree(t, dir)
			trees[idx] = treeInfo{
				parentCmd: cmd,
				pPid:      pPid,
				cPid:      cPid,
				gPid:      gPid,
			}
		}(i)
	}

	spawnWg.Wait()
	close(spawnErrCh)
	for err := range spawnErrCh {
		if err != nil {
			t.Fatalf("failed spawning trees: %v", err)
		}
	}

	// Ensure cleanup in case of test failure
	defer func() {
		for _, tr := range trees {
			if tr.pPid > 0 {
				_ = KillProcessTree(tr.pPid)
			}
			if tr.cPid > 0 {
				_ = KillProcessTree(tr.cPid)
			}
			if tr.gPid > 0 {
				_ = KillProcessTree(tr.gPid)
			}
		}
		for _, d := range tempDirs {
			if d != "" {
				_ = os.RemoveAll(d)
			}
		}
	}()

	// Step 2: Confirm all 30 processes are alive
	for i, tr := range trees {
		if !isProcessAlive(tr.pPid) || !isProcessAlive(tr.cPid) || !isProcessAlive(tr.gPid) {
			t.Fatalf("tree %d: expected all 3 processes to be alive (P:%d, C:%d, GC:%d)", i, tr.pPid, tr.cPid, tr.gPid)
		}
	}
	t.Logf("All 10 trees (30 processes) confirmed alive simultaneously.")

	// Step 3: Trigger 10 simultaneous KillProcessTree calls via barrier
	startBarrier := make(chan struct{})
	var killWg sync.WaitGroup
	killErrCh := make(chan error, treeCount)

	for i := 0; i < treeCount; i++ {
		killWg.Add(1)
		go func(idx int) {
			defer killWg.Done()
			<-startBarrier // wait for synchronized release

			pPid := trees[idx].pPid
			if err := KillProcessTree(pPid); err != nil {
				killErrCh <- fmt.Errorf("tree %d (PID %d) kill failed: %w", idx, pPid, err)
			}
		}(i)
	}

	// Release all 10 goroutines at the exact same instant
	close(startBarrier)
	killWg.Wait()
	close(killErrCh)

	for err := range killErrCh {
		if err != nil {
			t.Fatalf("concurrent kill error: %v", err)
		}
	}

	// Step 4: Verify that every single process (all 30 PIDs) is dead
	for i, tr := range trees {
		if err := waitForProcessDead(tr.pPid, 5*time.Second); err != nil {
			t.Fatalf("tree %d parent PID %d still alive: %v", i, tr.pPid, err)
		}
		if err := waitForProcessDead(tr.cPid, 5*time.Second); err != nil {
			t.Fatalf("tree %d child PID %d still alive: %v", i, tr.cPid, err)
		}
		if err := waitForProcessDead(tr.gPid, 5*time.Second); err != nil {
			t.Fatalf("tree %d grandchild PID %d still alive: %v", i, tr.gPid, err)
		}

		// Verify tasklist confirms zero orphans on host system
		assertProcessInTaskList(t, tr.pPid, false)
		assertProcessInTaskList(t, tr.cPid, false)
		assertProcessInTaskList(t, tr.gPid, false)
	}

	t.Logf("PASS: Successfully killed 10 simultaneous process trees (30 processes) concurrently with 0 tasklist orphans.")
}

// TestChallenge_WideBranchingProcessTree tests terminating a tree where 1 parent
// spawns 4 children, each spawning 2 grandchildren (total 1 + 4 + 8 = 13 processes).
func TestChallenge_WideBranchingProcessTree(t *testing.T) {
	workDir := t.TempDir()
	pythonPath, err := exec.LookPath("python")
	if err != nil {
		t.Skip("python not available")
	}

	// Python script that can spawn N children
	spawnerScript := filepath.Join(workDir, "spawner.py")
	scriptContent := `import os, sys, time, subprocess

# argv[1] = pid_file
# argv[2] = number of children to spawn
# argv[3] = child_script_path (if any)
# argv[4] = children count for grandchildren

pid_file = sys.argv[1]
with open(pid_file, "w") as f:
    f.write(str(os.getpid()))
    f.flush()

num_children = int(sys.argv[2])
if num_children > 0 and len(sys.argv) > 3:
    child_script = sys.argv[3]
    next_children_count = sys.argv[4] if len(sys.argv) > 4 else "0"
    for i in range(num_children):
        c_pid_file = pid_file + f".child_{i}"
        subprocess.Popen([sys.executable, child_script, c_pid_file, next_children_count, child_script, "0"])

while True:
    time.sleep(1)
`
	if err := os.WriteFile(spawnerScript, []byte(scriptContent), 0600); err != nil {
		t.Fatalf("failed to write spawner.py: %v", err)
	}

	parentPidFile := filepath.Join(workDir, "parent.pid")
	// Parent spawns 4 children, each child spawns 2 grandchildren
	cmd := exec.Command(pythonPath, spawnerScript, parentPidFile, "4", spawnerScript, "2")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start wide tree parent: %v", err)
	}

	parentPid := waitForPID(t, parentPidFile, 5*time.Second)
	allPids := []int{parentPid}

	// Wait for all 4 children PIDs
	childPids := make([]int, 4)
	for i := 0; i < 4; i++ {
		cPidFile := fmt.Sprintf("%s.child_%d", parentPidFile, i)
		childPids[i] = waitForPID(t, cPidFile, 5*time.Second)
		allPids = append(allPids, childPids[i])
	}

	// Wait for all 8 grandchildren PIDs (2 per child)
	gcPids := make([]int, 8)
	gcIdx := 0
	for i := 0; i < 4; i++ {
		cPidFile := fmt.Sprintf("%s.child_%d", parentPidFile, i)
		for j := 0; j < 2; j++ {
			gcPidFile := fmt.Sprintf("%s.child_%d", cPidFile, j)
			gcPids[gcIdx] = waitForPID(t, gcPidFile, 5*time.Second)
			allPids = append(allPids, gcPids[gcIdx])
			gcIdx++
		}
	}

	defer func() {
		for _, pid := range allPids {
			_ = KillProcessTree(pid)
		}
	}()

	t.Logf("Wide tree spawned with %d total processes (1 parent, 4 children, 8 grandchildren)", len(allPids))

	// Verify all 13 processes are alive
	for _, pid := range allPids {
		if !isProcessAlive(pid) {
			t.Fatalf("expected PID %d to be alive in wide tree", pid)
		}
	}

	// Terminate root parent with KillProcessTree
	if err := KillProcessTree(parentPid); err != nil {
		t.Fatalf("KillProcessTree failed for wide tree parent %d: %v", parentPid, err)
	}

	// Verify all 13 processes are dead
	for _, pid := range allPids {
		assertProcessDead(t, pid, 5*time.Second)
		assertProcessInTaskList(t, pid, false)
	}

	t.Logf("PASS: Wide tree with %d processes destroyed completely by KillProcessTree(%d).", len(allPids), parentPid)
}

// TestChallenge_Deep5TierChainedProcessTree tests a 5-level deep vertical process hierarchy:
// Tier 1 -> Tier 2 -> Tier 3 -> Tier 4 -> Tier 5.
func TestChallenge_Deep5TierChainedProcessTree(t *testing.T) {
	workDir := t.TempDir()
	pythonPath, err := exec.LookPath("python")
	if err != nil {
		t.Skip("python not available")
	}

	chainScript := filepath.Join(workDir, "chain.py")
	scriptContent := `import os, sys, time, subprocess

# argv[1] = current depth
# argv[2] = max depth
# argv[3] = base dir

depth = int(sys.argv[1])
max_depth = int(sys.argv[2])
base_dir = sys.argv[3]

pid_file = os.path.join(base_dir, f"tier_{depth}.pid")
with open(pid_file, "w") as f:
    f.write(str(os.getpid()))
    f.flush()

if depth < max_depth:
    next_depth = depth + 1
    subprocess.Popen([sys.executable, sys.argv[0], str(next_depth), str(max_depth), base_dir])

while True:
    time.sleep(1)
`
	if err := os.WriteFile(chainScript, []byte(scriptContent), 0600); err != nil {
		t.Fatalf("failed to write chain.py: %v", err)
	}

	const maxDepth = 5
	cmd := exec.Command(pythonPath, chainScript, "1", strconv.Itoa(maxDepth), workDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start deep chain parent: %v", err)
	}

	pids := make([]int, maxDepth)
	for i := 1; i <= maxDepth; i++ {
		pidFile := filepath.Join(workDir, fmt.Sprintf("tier_%d.pid", i))
		pids[i-1] = waitForPID(t, pidFile, 5*time.Second)
	}

	defer func() {
		for _, pid := range pids {
			_ = KillProcessTree(pid)
		}
	}()

	t.Logf("5-Tier Deep Process Tree spawned: %v", pids)

	for _, pid := range pids {
		if !isProcessAlive(pid) {
			t.Fatalf("tier PID %d expected alive", pid)
		}
	}

	// Kill Tier 1
	if err := KillProcessTree(pids[0]); err != nil {
		t.Fatalf("KillProcessTree failed for Tier 1 PID %d: %v", pids[0], err)
	}

	// Verify all 5 tiers are dead
	for i, pid := range pids {
		assertProcessDead(t, pid, 5*time.Second)
		assertProcessInTaskList(t, pid, false)
		t.Logf("Tier %d (PID %d) confirmed dead and removed from tasklist", i+1, pid)
	}

	t.Logf("PASS: Deep 5-tier process chain destroyed completely.")
}

// TestChallenge_Adversarial_DeadParentOrphanFailureMode tests the known Windows limitation:
// If a parent dies before taskkill is invoked, taskkill cannot find orphaned children by parent PID.
// It verifies that:
// 1. KillProcessTree(parentPid) handles the dead parent gracefully (returns nil, exit code 128).
// 2. The Job Object mechanism (pkg/process/job_windows.go) solves this exact failure mode!
func TestChallenge_Adversarial_DeadParentOrphanFailureMode(t *testing.T) {
	workDir := t.TempDir()
	pythonPath, err := exec.LookPath("python")
	if err != nil {
		t.Skip("python not available")
	}

	// Create Job Object
	job, err := NewJob()
	if err != nil {
		t.Fatalf("NewJob failed: %v", err)
	}
	defer job.Close()

	// Transient parent script: writes PID, spawns child, and exits after 100ms
	transientParentScript := filepath.Join(workDir, "transient_parent.py")
	childScript := filepath.Join(workDir, "child_sleep.py")

	childContent := `import os, sys, time
with open(sys.argv[1], "w") as f:
    f.write(str(os.getpid()))
    f.flush()
while True:
    time.sleep(1)
`
	parentContent := `import os, sys, time, subprocess
with open(sys.argv[1], "w") as f:
    f.write(str(os.getpid()))
    f.flush()

# wait for signal file to confirm assignment to Job Object
signal_file = sys.argv[3]
deadline = time.time() + 5.0
while time.time() < deadline:
    if os.path.exists(signal_file):
        break
    time.sleep(0.01)

# Spawn child
subprocess.Popen([sys.executable, sys.argv[2], sys.argv[4]])
# Parent exits immediately to orphan the child!
sys.exit(0)
`
	if err := os.WriteFile(childScript, []byte(childContent), 0600); err != nil {
		t.Fatalf("failed to write child script: %v", err)
	}
	if err := os.WriteFile(transientParentScript, []byte(parentContent), 0600); err != nil {
		t.Fatalf("failed to write transient parent script: %v", err)
	}

	parentPidFile := filepath.Join(workDir, "parent.pid")
	childPidFile := filepath.Join(workDir, "child.pid")
	signalFile := filepath.Join(workDir, "assigned.signal")

	cmd := exec.Command(pythonPath, transientParentScript, parentPidFile, childScript, signalFile, childPidFile)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start transient parent: %v", err)
	}

	pPid := waitForPID(t, parentPidFile, 5*time.Second)

	// Assign parent to Job Object BEFORE child is spawned
	if err := job.AssignPID(pPid); err != nil {
		t.Fatalf("job.AssignPID failed: %v", err)
	}

	// Signal parent that it has been assigned, so it spawns child and exits
	if err := os.WriteFile(signalFile, []byte("ok"), 0600); err != nil {
		t.Fatalf("failed to write signal file: %v", err)
	}

	cPid := waitForPID(t, childPidFile, 5*time.Second)

	defer func() {
		_ = KillProcessTree(pPid)
		_ = KillProcessTree(cPid)
	}()

	// Wait for parent to exit
	_ = cmd.Wait()
	assertProcessDead(t, pPid, 3*time.Second)

	// 1. Calling KillProcessTree on dead parent PID returns nil (exit code 128 idempotent)
	if err := KillProcessTree(pPid); err != nil {
		t.Fatalf("expected nil for dead parent PID %d, got: %v", pPid, err)
	}

	// 2. But without Job Object, child PID would survive orphaned!
	// Confirm child is still alive right now:
	if !isProcessAlive(cPid) {
		t.Fatalf("expected child %d to be alive before job closure", cPid)
	}

	// 3. Now close Job Object: the kernel terminates the orphaned child!
	if err := job.Close(); err != nil {
		t.Fatalf("job.Close failed: %v", err)
	}

	assertProcessDead(t, cPid, 3*time.Second)
	assertProcessInTaskList(t, cPid, false)

	t.Logf("PASS: Dead-parent race condition tested and verified defense via Job Object.")
}
