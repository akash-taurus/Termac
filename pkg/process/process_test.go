//go:build windows

package process

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const (
	processQueryInformation        = 0x0400
	processQueryLimitedInformation = 0x1000
	stillActive                    = 259
)

// isProcessAlive returns true if the process exists and is currently active.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		h, err = syscall.OpenProcess(processQueryInformation, false, uint32(pid))
		if err != nil {
			return false
		}
	}
	defer syscall.CloseHandle(h)

	var exitCode uint32
	if err := syscall.GetExitCodeProcess(h, &exitCode); err != nil {
		return false
	}
	return exitCode == stillActive
}

// waitForProcessDead polls until the process is dead or the timeout expires.
func waitForProcessDead(pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !isProcessAlive(pid) {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("process %d is still alive after %v timeout", pid, timeout)
}

// assertProcessDead asserts that the process terminates within the specified timeout.
func assertProcessDead(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	if err := waitForProcessDead(pid, timeout); err != nil {
		t.Fatal(err)
	}
}

// assertProcessInTaskList verifies whether tasklist.exe sees the process.
func assertProcessInTaskList(t *testing.T, pid int, expectedAlive bool) {
	t.Helper()
	cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("tasklist execution failed: %v", err)
	}
	found := bytes.Contains(out, []byte(strconv.Itoa(pid)))
	if found != expectedAlive {
		t.Fatalf("tasklist presence mismatch for PID %d: got found=%v, want expectedAlive=%v (output: %q)",
			pid, found, expectedAlive, strings.TrimSpace(string(out)))
	}
}

// waitForPIDNonFatal waits for a PID file to be written and parses the PID.
func waitForPIDNonFatal(pidFile string, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			s := strings.TrimSpace(string(data))
			if pid, err := strconv.Atoi(s); err == nil && pid > 0 {
				return pid, nil
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return 0, fmt.Errorf("timed out waiting for PID file %s", pidFile)
}

// waitForPID calls waitForPIDNonFatal and fails the test if it times out.
func waitForPID(t *testing.T, pidFile string, timeout time.Duration) int {
	t.Helper()
	pid, err := waitForPIDNonFatal(pidFile, timeout)
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

// TestHelperProcess is a self-reexecuting helper used to spawn multi-tier process trees.
// It is invoked when os.Args contains -test.run=^TestHelperProcess$.
func TestHelperProcess(t *testing.T) {
	role := os.Getenv("GO_PROCESS_HELPER_ROLE")
	if role == "" {
		return
	}
	pidsDir := os.Getenv("GO_PROCESS_PIDS_DIR")
	if pidsDir == "" {
		fmt.Fprintf(os.Stderr, "GO_PROCESS_PIDS_DIR not set\n")
		os.Exit(1)
	}

	pid := os.Getpid()
	pidFileName := role
	if role == "parent_job" {
		pidFileName = "parent"
	}
	pidFile := filepath.Join(pidsDir, pidFileName+".pid")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0600); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write PID file: %v\n", err)
		os.Exit(1)
	}

	if role == "parent_job" {
		signalFile := filepath.Join(pidsDir, "start_children.txt")
		// Wait up to 10s for main test to assign this process to Job Object
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(signalFile); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		childCmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
		childCmd.Env = append(os.Environ(),
			"GO_PROCESS_HELPER_ROLE=child",
			"GO_PROCESS_PIDS_DIR="+pidsDir,
		)
		childCmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: CREATE_NO_WINDOW,
		}
		if err := childCmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to start child: %v\n", err)
			os.Exit(1)
		}
		// Parent exits immediately, creating the dead parent race condition where child is orphaned
		os.Exit(0)
	}

	if role == "parent" {
		childCmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
		childCmd.Env = append(os.Environ(),
			"GO_PROCESS_HELPER_ROLE=child",
			"GO_PROCESS_PIDS_DIR="+pidsDir,
		)
		childCmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: CREATE_NO_WINDOW,
		}
		if err := childCmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to start child: %v\n", err)
			os.Exit(1)
		}
	} else if role == "child" {
		gcCmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
		gcCmd.Env = append(os.Environ(),
			"GO_PROCESS_HELPER_ROLE=grandchild",
			"GO_PROCESS_PIDS_DIR="+pidsDir,
		)
		gcCmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: CREATE_NO_WINDOW,
		}
		if err := gcCmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to start grandchild: %v\n", err)
			os.Exit(1)
		}
	}

	// Sleep cleanly to keep process running until terminated by taskkill.
	// NOTE: Do not use select {} without open channels to avoid Go deadlock runtime panics.
	time.Sleep(time.Hour)
}

// TestKillProcessTree_InvalidPID verifies that PID 0 and negative PIDs return ErrInvalidPID.
func TestKillProcessTree_InvalidPID(t *testing.T) {
	cases := []struct {
		name string
		pid  int
	}{
		{"PID zero (System Idle Process)", 0},
		{"PID negative one", -1},
		{"PID negative large", -9999},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := KillProcessTree(tc.pid)
			if !errors.Is(err, ErrInvalidPID) {
				t.Fatalf("expected ErrInvalidPID for pid %d, got: %v", tc.pid, err)
			}
		})
	}
}

// TestKillProcessTree_IdempotentAlreadyDead verifies that killing a non-existent PID returns nil.
func TestKillProcessTree_IdempotentAlreadyDead(t *testing.T) {
	// 999999 is guaranteed non-existent on Windows.
	err := KillProcessTree(999999)
	if err != nil {
		t.Fatalf("expected nil when killing non-existent PID 999999, got: %v", err)
	}
}

// TestKillProcessTree_DoubleKill verifies that calling KillProcessTree twice returns nil both times.
func TestKillProcessTree_DoubleKill(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "ping 127.0.0.1 -n 30 >nul")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start cmd: %v", err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = KillProcessTree(pid)
	})

	if !isProcessAlive(pid) {
		t.Fatalf("expected process %d to be alive", pid)
	}

	// First kill
	if err := KillProcessTree(pid); err != nil {
		t.Fatalf("first KillProcessTree failed: %v", err)
	}
	assertProcessDead(t, pid, 3*time.Second)

	// Second kill must return nil idempotently
	if err := KillProcessTree(pid); err != nil {
		t.Fatalf("second KillProcessTree failed: %v", err)
	}
}

// TestKillCmd_Validation verifies validation logic on KillCmd.
func TestKillCmd_Validation(t *testing.T) {
	t.Run("nil cmd returns ErrNilCmd", func(t *testing.T) {
		err := KillCmd(nil)
		if !errors.Is(err, ErrNilCmd) {
			t.Fatalf("expected ErrNilCmd, got: %v", err)
		}
	})

	t.Run("unstarted cmd returns ErrProcessNotStarted", func(t *testing.T) {
		cmd := exec.Command("cmd.exe", "/c", "exit 0")
		err := KillCmd(cmd)
		if !errors.Is(err, ErrProcessNotStarted) {
			t.Fatalf("expected ErrProcessNotStarted, got: %v", err)
		}
	})

	t.Run("cmd with invalid PID returns ErrInvalidPID", func(t *testing.T) {
		cmd := &exec.Cmd{
			Process: &os.Process{Pid: 0},
		}
		err := KillCmd(cmd)
		if !errors.Is(err, ErrInvalidPID) {
			t.Fatalf("expected ErrInvalidPID, got: %v", err)
		}
	})
}

// TestIntegration_3TierProcessTreeKill verifies that KillProcessTree on parent terminates
// the entire Parent -> Child -> Grandchild hierarchy without leaving any zombies.
func TestIntegration_3TierProcessTreeKill(t *testing.T) {
	pidsDir := t.TempDir()

	parentCmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
	parentCmd.Env = append(os.Environ(),
		"GO_PROCESS_HELPER_ROLE=parent",
		"GO_PROCESS_PIDS_DIR="+pidsDir,
	)
	parentCmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: CREATE_NO_WINDOW,
	}

	if err := parentCmd.Start(); err != nil {
		t.Fatalf("failed to start parent helper process: %v", err)
	}

	parentPid := waitForPID(t, filepath.Join(pidsDir, "parent.pid"), 5*time.Second)
	childPid := waitForPID(t, filepath.Join(pidsDir, "child.pid"), 5*time.Second)
	gcPid := waitForPID(t, filepath.Join(pidsDir, "grandchild.pid"), 5*time.Second)

	t.Cleanup(func() {
		if parentPid > 0 {
			_ = KillProcessTree(parentPid)
		}
		if childPid > 0 {
			_ = KillProcessTree(childPid)
		}
		if gcPid > 0 {
			_ = KillProcessTree(gcPid)
		}
	})

	t.Logf("3-Tier Process Tree spawned: Parent PID %d -> Child PID %d -> Grandchild PID %d", parentPid, childPid, gcPid)

	// Confirm all 3 processes are alive
	if !isProcessAlive(parentPid) {
		t.Fatalf("expected parent PID %d to be alive", parentPid)
	}
	if !isProcessAlive(childPid) {
		t.Fatalf("expected child PID %d to be alive", childPid)
	}
	if !isProcessAlive(gcPid) {
		t.Fatalf("expected grandchild PID %d to be alive", gcPid)
	}

	// Terminate the entire process tree using KillProcessTree on parent
	if err := KillProcessTree(parentPid); err != nil {
		t.Fatalf("KillProcessTree failed for parent PID %d: %v", parentPid, err)
	}

	// Verify parent, child, and grandchild are ALL completely dead without leaving zombies
	assertProcessDead(t, parentPid, 5*time.Second)
	assertProcessDead(t, childPid, 5*time.Second)
	assertProcessDead(t, gcPid, 5*time.Second)

	// Verify tasklist confirms no zombies
	assertProcessInTaskList(t, parentPid, false)
	assertProcessInTaskList(t, childPid, false)
	assertProcessInTaskList(t, gcPid, false)

	t.Logf("Verified 100%%: Parent %d, Child %d, and Grandchild %d were all cleanly terminated with zero zombies!", parentPid, childPid, gcPid)
}

// TestIntegration_ProcessKill_DemonstrateOrphan proves that standard Go cmd.Process.Kill()
// leaves child processes running as orphans, whereas KillProcessTree kills them all.
func TestIntegration_ProcessKill_DemonstrateOrphan(t *testing.T) {
	pidsDir := t.TempDir()

	parentCmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
	parentCmd.Env = append(os.Environ(),
		"GO_PROCESS_HELPER_ROLE=parent",
		"GO_PROCESS_PIDS_DIR="+pidsDir,
	)
	parentCmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: CREATE_NO_WINDOW,
	}

	if err := parentCmd.Start(); err != nil {
		t.Fatalf("failed to start parent helper process: %v", err)
	}

	parentPid := waitForPID(t, filepath.Join(pidsDir, "parent.pid"), 5*time.Second)
	childPid := waitForPID(t, filepath.Join(pidsDir, "child.pid"), 5*time.Second)
	gcPid := waitForPID(t, filepath.Join(pidsDir, "grandchild.pid"), 5*time.Second)

	t.Cleanup(func() {
		if parentPid > 0 {
			_ = KillProcessTree(parentPid)
		}
		if childPid > 0 {
			_ = KillProcessTree(childPid)
		}
		if gcPid > 0 {
			_ = KillProcessTree(gcPid)
		}
	})

	t.Logf("Contrasting test spawned: Parent PID %d -> Child PID %d -> Grandchild PID %d", parentPid, childPid, gcPid)

	if !isProcessAlive(parentPid) || !isProcessAlive(childPid) || !isProcessAlive(gcPid) {
		t.Fatalf("expected all 3 processes to be alive initially")
	}

	// Use standard Go (*os.Process).Kill() on parent
	if err := parentCmd.Process.Kill(); err != nil {
		t.Fatalf("cmd.Process.Kill failed: %v", err)
	}
	_ = parentCmd.Wait()

	// Assert parent is dead
	assertProcessDead(t, parentPid, 3*time.Second)

	// Check child and grandchild: Standard Go TerminateProcess leaves descendants running!
	childAlive := isProcessAlive(childPid)
	gcAlive := isProcessAlive(gcPid)

	t.Logf("After cmd.Process.Kill(): Parent dead=true, Child alive=%v, Grandchild alive=%v", childAlive, gcAlive)
	if !childAlive || !gcAlive {
		t.Fatalf("expected child and grandchild to survive cmd.Process.Kill (demonstrating orphan leak), got childAlive=%v, gcAlive=%v", childAlive, gcAlive)
	}

	// Now prove that KillProcessTree terminates surviving orphaned descendants
	if err := KillProcessTree(childPid); err != nil {
		t.Fatalf("KillProcessTree on surviving child PID %d failed: %v", childPid, err)
	}

	assertProcessDead(t, childPid, 5*time.Second)
	assertProcessDead(t, gcPid, 5*time.Second)

	t.Logf("Empirical contrast verified: cmd.Process.Kill() leaked child/grandchild, while KillProcessTree terminated them cleanly!")
}

// TestKillCmd_Lifecycle verifies starting a process, killing it via KillCmd, and confirming reaping.
func TestKillCmd_Lifecycle(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "ping 127.0.0.1 -n 30 >nul")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start command: %v", err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = KillProcessTree(pid)
	})

	if !isProcessAlive(pid) {
		t.Fatalf("expected process %d to be alive", pid)
	}

	// KillCmd should terminate tree, reap process, and return nil
	if err := KillCmd(cmd); err != nil {
		t.Fatalf("KillCmd failed: %v", err)
	}

	assertProcessDead(t, pid, 3*time.Second)

	if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
		t.Fatalf("expected cmd.ProcessState to be non-nil and Exited() == true")
	}

	// Calling KillCmd again on reaped process should return nil idempotently
	if err := KillCmd(cmd); err != nil {
		t.Fatalf("subsequent KillCmd failed: %v", err)
	}
}

// TestKillProcessTree_MockRunner tests error handling using mock runners.
func TestKillProcessTree_MockRunner(t *testing.T) {
	origRunner := runTaskkill
	defer func() { runTaskkill = origRunner }()

	t.Run("taskkill returns 128", func(t *testing.T) {
		runTaskkill = func(pid int) ([]byte, error) {
			cmd := exec.Command("cmd.exe", "/c", "exit 128")
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}
			err := cmd.Run()
			return []byte("ERROR: The process not found"), err
		}
		err := KillProcessTree(12345)
		if err != nil {
			t.Fatalf("expected nil for exit code 128, got: %v", err)
		}
	})

	t.Run("taskkill returns exit code 1 access denied", func(t *testing.T) {
		runTaskkill = func(pid int) ([]byte, error) {
			cmd := exec.Command("cmd.exe", "/c", "exit 1")
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}
			err := cmd.Run()
			return []byte("ERROR: Access is denied."), err
		}
		err := KillProcessTree(12345)
		if err == nil {
			t.Fatalf("expected error for exit code 1, got nil")
		}
		if !strings.Contains(err.Error(), "Access is denied") {
			t.Fatalf("expected error to contain 'Access is denied', got: %v", err)
		}
	})

	t.Run("taskkill execution error", func(t *testing.T) {
		runTaskkill = func(pid int) ([]byte, error) {
			return nil, errors.New("exec: executable file not found")
		}
		err := KillProcessTree(12345)
		if err == nil {
			t.Fatalf("expected error when execution fails, got nil")
		}
	})
}

// TestKillProcessTree_BatchTree tests terminating a native Windows batch script process tree.
func TestKillProcessTree_BatchTree(t *testing.T) {
	tmpDir := t.TempDir()
	batFile := filepath.Join(tmpDir, "tree.bat")
	batContent := "@echo off\r\nping 127.0.0.1 -n 30 >nul\r\n"
	if err := os.WriteFile(batFile, []byte(batContent), 0700); err != nil {
		t.Fatalf("failed to write batch file: %v", err)
	}

	cmd := exec.Command("cmd.exe", "/c", batFile)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start batch command: %v", err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = KillProcessTree(pid)
	})

	time.Sleep(200 * time.Millisecond)
	if !isProcessAlive(pid) {
		t.Fatalf("expected batch process %d to be alive", pid)
	}

	if err := KillProcessTree(pid); err != nil {
		t.Fatalf("KillProcessTree failed: %v", err)
	}

	assertProcessDead(t, pid, 3*time.Second)
	assertProcessInTaskList(t, pid, false)
}

// TestKillProcessTree_Stress_Concurrent verifies rapid concurrent process tree kills.
func TestKillProcessTree_Stress_Concurrent(t *testing.T) {
	const treeCount = 4
	var wg sync.WaitGroup
	errCh := make(chan error, treeCount)

	for i := 0; i < treeCount; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			pidsDir, err := os.MkdirTemp("", fmt.Sprintf("test_tree_conc_%d_*", index))
			if err != nil {
				errCh <- err
				return
			}
			defer os.RemoveAll(pidsDir)

			cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
			cmd.Env = append(os.Environ(),
				"GO_PROCESS_HELPER_ROLE=parent",
				"GO_PROCESS_PIDS_DIR="+pidsDir,
			)
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}
			if err := cmd.Start(); err != nil {
				errCh <- err
				return
			}

			pPid, err := waitForPIDNonFatal(filepath.Join(pidsDir, "parent.pid"), 5*time.Second)
			if err != nil {
				errCh <- err
				return
			}
			cPid, err := waitForPIDNonFatal(filepath.Join(pidsDir, "child.pid"), 5*time.Second)
			if err != nil {
				errCh <- err
				return
			}
			gcPid, err := waitForPIDNonFatal(filepath.Join(pidsDir, "grandchild.pid"), 5*time.Second)
			if err != nil {
				errCh <- err
				return
			}

			defer func() {
				_ = KillProcessTree(pPid)
				_ = KillProcessTree(cPid)
				_ = KillProcessTree(gcPid)
			}()

			if err := KillProcessTree(pPid); err != nil {
				errCh <- fmt.Errorf("concurrent KillProcessTree failed: %w", err)
				return
			}

			if err := waitForProcessDead(pPid, 4*time.Second); err != nil {
				errCh <- err
				return
			}
			if err := waitForProcessDead(cPid, 4*time.Second); err != nil {
				errCh <- err
				return
			}
			if err := waitForProcessDead(gcPid, 4*time.Second); err != nil {
				errCh <- err
				return
			}
		}(i)
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent test failed: %v", err)
		}
	}
}

// TestJobObject_Lifecycle tests Job Object creation, process assignment, and close termination.
func TestJobObject_Lifecycle(t *testing.T) {
	job, err := NewJob()
	if err != nil {
		t.Fatalf("NewJob failed: %v", err)
	}
	defer job.Close()

	if job.Handle() == 0 {
		t.Fatalf("expected valid handle for Job")
	}

	cmd := exec.Command("cmd.exe", "/c", "ping 127.0.0.1 -n 30 >nul")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start cmd: %v", err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = KillProcessTree(pid)
	})

	if err := job.AssignProcess(cmd.Process); err != nil {
		t.Fatalf("AssignProcess failed: %v", err)
	}

	if !isProcessAlive(pid) {
		t.Fatalf("expected process %d to be alive", pid)
	}

	// Close Job Object: Windows kernel should kill all member processes
	if err := job.Close(); err != nil {
		t.Fatalf("job.Close failed: %v", err)
	}

	assertProcessDead(t, pid, 3*time.Second)
}

// TestJobObject_DeadParent_ChildKilled verifies that Job Object terminates orphaned children
// even when the parent process terminated previously (closing the dead-parent race condition).
func TestJobObject_DeadParent_ChildKilled(t *testing.T) {
	pidsDir := t.TempDir()

	job, err := NewJob()
	if err != nil {
		t.Fatalf("NewJob failed: %v", err)
	}
	defer job.Close()

	parentCmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
	parentCmd.Env = append(os.Environ(),
		"GO_PROCESS_HELPER_ROLE=parent_job",
		"GO_PROCESS_PIDS_DIR="+pidsDir,
	)
	parentCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}
	if err := parentCmd.Start(); err != nil {
		t.Fatalf("failed to start parent: %v", err)
	}
	t.Cleanup(func() {
		if parentCmd.Process != nil {
			_ = parentCmd.Process.Kill()
			_ = parentCmd.Wait()
		}
	})

	parentPid := waitForPID(t, filepath.Join(pidsDir, "parent.pid"), 5*time.Second)

	// Assign parent to Job Object BEFORE child is spawned so child inherits membership
	if err := job.AssignPID(parentPid); err != nil {
		t.Fatalf("AssignPID failed: %v", err)
	}

	// Signal parent to spawn child and exit
	signalFile := filepath.Join(pidsDir, "start_children.txt")
	if err := os.WriteFile(signalFile, []byte("go"), 0600); err != nil {
		t.Fatalf("failed to write signal file: %v", err)
	}

	childPid := waitForPID(t, filepath.Join(pidsDir, "child.pid"), 5*time.Second)
	gcPid := waitForPID(t, filepath.Join(pidsDir, "grandchild.pid"), 5*time.Second)

	t.Cleanup(func() {
		_ = KillProcessTree(parentPid)
		_ = KillProcessTree(childPid)
		_ = KillProcessTree(gcPid)
	})

	// Wait for parent process to exit
	_ = parentCmd.Wait()
	assertProcessDead(t, parentPid, 3*time.Second)

	// In the dead-parent race condition, calling taskkill on dead parent returns exit code 128 (nil),
	// but leaves orphaned child/grandchild running!
	if err := KillProcessTree(parentPid); err != nil {
		t.Fatalf("KillProcessTree(parentPid) should return nil for dead parent, got: %v", err)
	}

	// Confirm child and grandchild are still running as orphaned background processes
	if !isProcessAlive(childPid) || !isProcessAlive(gcPid) {
		t.Fatalf("expected child and grandchild to survive dead parent before job close")
	}

	// Closing Job Object triggers kernel-level termination of the entire job hierarchy!
	if err := job.Close(); err != nil {
		t.Fatalf("job.Close failed: %v", err)
	}

	// Verify child and grandchild are now completely dead!
	assertProcessDead(t, childPid, 3*time.Second)
	assertProcessDead(t, gcPid, 3*time.Second)
	t.Logf("Verified: Job Object successfully terminated orphaned child PID %d and grandchild PID %d after parent died!", childPid, gcPid)
}
