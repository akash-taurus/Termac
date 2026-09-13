//go:build windows

package process

import (
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestChallenger_BoundaryPIDs thoroughly verifies all negative and zero PID boundaries.
func TestChallenger_BoundaryPIDs(t *testing.T) {
	testPIDs := []int{
		0,
		-1,
		-2,
		-9999,
		-100000,
		math.MinInt32,
		math.MinInt,
	}

	for _, pid := range testPIDs {
		t.Run(fmt.Sprintf("KillProcessTree_PID_%d", pid), func(t *testing.T) {
			err := KillProcessTree(pid)
			if !errors.Is(err, ErrInvalidPID) {
				t.Fatalf("KillProcessTree(%d) expected ErrInvalidPID, got: %v", pid, err)
			}
		})

		t.Run(fmt.Sprintf("Job_AssignPID_%d", pid), func(t *testing.T) {
			job, err := NewJob()
			if err != nil {
				t.Fatalf("NewJob failed: %v", err)
			}
			defer job.Close()

			err = job.AssignPID(pid)
			if !errors.Is(err, ErrInvalidPID) {
				t.Fatalf("job.AssignPID(%d) expected ErrInvalidPID, got: %v", pid, err)
			}
		})
	}
}

// TestChallenger_IdempotentNonExistent verifies that non-existent PIDs (999999, 999998, etc.)
// return nil idempotently because taskkill exit code 128 is treated as success.
func TestChallenger_IdempotentNonExistent(t *testing.T) {
	nonExistentPIDs := []int{999999, 999998, 888888}

	for _, pid := range nonExistentPIDs {
		t.Run(fmt.Sprintf("PID_%d", pid), func(t *testing.T) {
			// Single call
			err := KillProcessTree(pid)
			if err != nil {
				t.Fatalf("KillProcessTree(%d) on non-existent PID expected nil, got: %v", pid, err)
			}

			// Repetitive calls (hammer 5 times)
			for i := 0; i < 5; i++ {
				if err := KillProcessTree(pid); err != nil {
					t.Fatalf("KillProcessTree(%d) iteration %d expected nil, got: %v", pid, i, err)
				}
			}
		})
	}
}

// TestChallenger_DoubleKill_SequentialAndConcurrent verifies both sequential double-kill
// and high-concurrency race condition on the exact same live process.
func TestChallenger_DoubleKill_SequentialAndConcurrent(t *testing.T) {
	t.Run("Sequential Double Kill", func(t *testing.T) {
		cmd := exec.Command("cmd.exe", "/c", "ping 127.0.0.1 -n 30 >nul")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}
		if err := cmd.Start(); err != nil {
			t.Fatalf("failed to start cmd: %v", err)
		}
		pid := cmd.Process.Pid
		t.Cleanup(func() { _ = KillProcessTree(pid) })

		if !isProcessAlive(pid) {
			t.Fatalf("expected PID %d to be alive", pid)
		}

		// First kill
		if err := KillProcessTree(pid); err != nil {
			t.Fatalf("first KillProcessTree(%d) failed: %v", pid, err)
		}
		assertProcessDead(t, pid, 3*time.Second)

		// Second kill
		if err := KillProcessTree(pid); err != nil {
			t.Fatalf("second KillProcessTree(%d) failed: %v", pid, err)
		}

		// Third kill
		if err := KillProcessTree(pid); err != nil {
			t.Fatalf("third KillProcessTree(%d) failed: %v", pid, err)
		}
	})

	t.Run("Concurrent 10-way Double Kill Race", func(t *testing.T) {
		cmd := exec.Command("cmd.exe", "/c", "ping 127.0.0.1 -n 30 >nul")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}
		if err := cmd.Start(); err != nil {
			t.Fatalf("failed to start cmd: %v", err)
		}
		pid := cmd.Process.Pid
		t.Cleanup(func() { _ = KillProcessTree(pid) })

		const goroutines = 10
		var wg sync.WaitGroup
		errs := make(chan error, goroutines)

		// Launch 10 goroutines calling KillProcessTree on the exact same PID at the same time
		startGate := make(chan struct{})
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-startGate
				errs <- KillProcessTree(pid)
			}()
		}

		close(startGate)
		wg.Wait()
		close(errs)

		for err := range errs {
			if err != nil {
				t.Fatalf("concurrent KillProcessTree failed on PID %d: %v", pid, err)
			}
		}

		assertProcessDead(t, pid, 3*time.Second)
	})
}

// TestChallenger_DeadParent_JobObjectHierarchy stress-tests the Windows Job Object
// mechanism against deep orphaned processes where the parent exited.
func TestChallenger_DeadParent_JobObjectHierarchy(t *testing.T) {
	pidsDir := t.TempDir()

	job, err := NewJob()
	if err != nil {
		t.Fatalf("NewJob failed: %v", err)
	}
	defer job.Close()

	// Spawn parent helper configured to spawn children and immediately exit
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

	// Assign parent to Job Object before spawning descendants
	if err := job.AssignPID(parentPid); err != nil {
		t.Fatalf("AssignPID failed: %v", err)
	}

	// Trigger children spawn and parent exit
	signalFile := filepath.Join(pidsDir, "start_children.txt")
	if err := os.WriteFile(signalFile, []byte("start"), 0600); err != nil {
		t.Fatalf("failed to write signal file: %v", err)
	}

	childPid := waitForPID(t, filepath.Join(pidsDir, "child.pid"), 5*time.Second)
	gcPid := waitForPID(t, filepath.Join(pidsDir, "grandchild.pid"), 5*time.Second)

	t.Cleanup(func() {
		_ = KillProcessTree(parentPid)
		_ = KillProcessTree(childPid)
		_ = KillProcessTree(gcPid)
	})

	// Wait for parent process to exit completely
	_ = parentCmd.Wait()
	assertProcessDead(t, parentPid, 3*time.Second)

	// In the dead-parent race condition:
	// Parent is dead. Descendants childPid and gcPid are still running as orphans!
	if !isProcessAlive(childPid) {
		t.Fatalf("expected child %d to be alive before job.Close()", childPid)
	}
	if !isProcessAlive(gcPid) {
		t.Fatalf("expected grandchild %d to be alive before job.Close()", gcPid)
	}

	// Empirically show: KillProcessTree on parentPid returns nil because parent is dead (code 128),
	// BUT because parent is dead, taskkill /PID <parentPid> /T CANNOT kill the children!
	if err := KillProcessTree(parentPid); err != nil {
		t.Fatalf("KillProcessTree(parentPid) expected nil, got: %v", err)
	}

	// Child and grandchild are still running despite KillProcessTree(parentPid)!
	if !isProcessAlive(childPid) || !isProcessAlive(gcPid) {
		t.Fatalf("expected child and grandchild to survive taskkill on dead parent")
	}

	// Now close the Job Object: Kernel terminates ALL processes in the job!
	if err := job.Close(); err != nil {
		t.Fatalf("job.Close() failed: %v", err)
	}

	// Assert that both child and grandchild are now definitively dead!
	assertProcessDead(t, childPid, 4*time.Second)
	assertProcessDead(t, gcPid, 4*time.Second)

	// Assert tasklist also confirms no trace of them
	assertProcessInTaskList(t, childPid, false)
	assertProcessInTaskList(t, gcPid, false)

	// Verify idempotency of job.Close() - calling it again must return nil
	if err := job.Close(); err != nil {
		t.Fatalf("subsequent job.Close() failed: %v", err)
	}
}

// TestChallenger_JobObject_EdgeCases verifies edge cases in Job Object lifecycle.
func TestChallenger_JobObject_EdgeCases(t *testing.T) {
	t.Run("AssignPID to closed job returns error", func(t *testing.T) {
		job, err := NewJob()
		if err != nil {
			t.Fatalf("NewJob failed: %v", err)
		}
		if err := job.Close(); err != nil {
			t.Fatalf("job.Close failed: %v", err)
		}

		err = job.AssignPID(os.Getpid())
		if err == nil {
			t.Fatalf("expected error assigning to closed job, got nil")
		}
	})

	t.Run("AssignProcess nil returns ErrNilCmd", func(t *testing.T) {
		job, err := NewJob()
		if err != nil {
			t.Fatalf("NewJob failed: %v", err)
		}
		defer job.Close()

		err = job.AssignProcess(nil)
		if !errors.Is(err, ErrNilCmd) {
			t.Fatalf("expected ErrNilCmd, got: %v", err)
		}
	})

	t.Run("AssignPID non-existent PID returns error", func(t *testing.T) {
		job, err := NewJob()
		if err != nil {
			t.Fatalf("NewJob failed: %v", err)
		}
		defer job.Close()

		err = job.AssignPID(999999)
		if err == nil {
			t.Fatalf("expected error when assigning non-existent PID, got nil")
		}
	})

	t.Run("Terminate on closed job is no-op", func(t *testing.T) {
		job, err := NewJob()
		if err != nil {
			t.Fatalf("NewJob failed: %v", err)
		}
		_ = job.Close()

		err = job.Terminate(1)
		if err != nil {
			t.Fatalf("expected nil when terminating closed job, got: %v", err)
		}
	})

	t.Run("AssignHandle invalid handle returns error", func(t *testing.T) {
		job, err := NewJob()
		if err != nil {
			t.Fatalf("NewJob failed: %v", err)
		}
		defer job.Close()

		err = job.AssignHandle(0)
		if err == nil {
			t.Fatalf("expected error when assigning invalid null handle, got nil")
		}
	})

	t.Run("Rapid Job creation and destruction cycle", func(t *testing.T) {
		for i := 0; i < 50; i++ {
			job, err := NewJob()
			if err != nil {
				t.Fatalf("NewJob failed on iteration %d: %v", i, err)
			}
			if err := job.Close(); err != nil {
				t.Fatalf("job.Close failed on iteration %d: %v", i, err)
			}
		}
	})
}

// TestChallenger_KillCmd_Concurrency verifies that calling KillCmd concurrently does not panic.
func TestChallenger_KillCmd_Concurrency(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "ping 127.0.0.1 -n 30 >nul")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start cmd: %v", err)
	}

	pid := cmd.Process.Pid
	t.Cleanup(func() { _ = KillProcessTree(pid) })

	var wg sync.WaitGroup
	const callers = 5
	errs := make(chan error, callers)

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- KillCmd(cmd)
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("KillCmd concurrent call returned unexpected error: %v", err)
		}
	}

	assertProcessDead(t, pid, 3*time.Second)
}
