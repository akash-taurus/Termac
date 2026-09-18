package process

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
)

// killCmdMu serializes KillCmd per *exec.Cmd. The shared mutable state is
// cmd.ProcessState plus the OS process handle: two goroutines racing into
// cmd.Wait() on Windows can lose with a raw NTSTATUS-as-errno (e.g.
// 0x20000027) that matches no Go sentinel, so racy Wait calls cannot be
// made safe by error filtering alone. Entries are intentionally never
// deleted: KillCmd call sites are few (plugin stop, tests) and deleting
// while another goroutine resolves the same key could hand out two
// mutexes for one cmd, defeating the purpose.
var killCmdMu sync.Map // map[*exec.Cmd]*sync.Mutex

func mutexForCmd(cmd *exec.Cmd) *sync.Mutex {
	mu, _ := killCmdMu.LoadOrStore(cmd, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

var (
	// ErrInvalidPID indicates an invalid process ID (PID <= 0).
	ErrInvalidPID = errors.New("invalid PID: must be greater than 0")

	// ErrNilCmd indicates that a nil *exec.Cmd was provided.
	ErrNilCmd = errors.New("cmd cannot be nil")

	// ErrProcessNotStarted indicates that the command has not been started (cmd.Process == nil).
	ErrProcessNotStarted = errors.New("process not started")
)

// KillCmd terminates the process tree of the given command and reaps the process resources.
// It extracts the PID from cmd.Process, executes KillProcessTree(pid), and reaps the process
// via cmd.Wait() if the process has not yet been waited upon.
func KillCmd(cmd *exec.Cmd) error {
	if cmd == nil {
		return ErrNilCmd
	}
	if cmd.Process == nil {
		return ErrProcessNotStarted
	}
	pid := cmd.Process.Pid
	if pid <= 0 {
		return ErrInvalidPID
	}

	// Serialize with other KillCmd calls on this same cmd so the
	// ProcessState check and Wait below are atomic per command.
	mu := mutexForCmd(cmd)
	mu.Lock()
	defer mu.Unlock()

	// Terminate the entire descendant process tree. Always attempt to reap
	// afterwards so a kill failure does not leak a zombie/handle.
	killErr := KillProcessTree(pid)

	// Reap the process to release OS process handles and prevent handle leaks.
	// If the process was already reaped (cmd.ProcessState != nil), skip Wait.
	// The filters below stay as defense-in-depth for Wait calls racing us
	// from outside KillCmd (same-handle concurrent Wait on Windows surfaces
	// raw NTSTATUS errnos that match no Go sentinel).
	if cmd.ProcessState == nil {
		if err := cmd.Wait(); err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) && err.Error() != "exec: Wait was already called" && !errors.Is(err, os.ErrInvalid) {
				if killErr != nil {
					return errors.Join(killErr, fmt.Errorf("failed to reap process %d: %w", pid, err))
				}
				return fmt.Errorf("failed to reap process %d: %w", pid, err)
			}
		}
	}

	return killErr
}
