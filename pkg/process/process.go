package process

import (
	"errors"
	"fmt"
	"os/exec"
)

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

	// Terminate the entire descendant process tree
	if err := KillProcessTree(pid); err != nil {
		return err
	}

	// Reap the process to release OS process handles and prevent handle leaks.
	// If the process was already reaped (cmd.ProcessState != nil), skip Wait.
	if cmd.ProcessState == nil {
		if err := cmd.Wait(); err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) && err.Error() != "exec: Wait was already called" {
				return fmt.Errorf("failed to reap process %d: %w", pid, err)
			}
		}
	}

	return nil
}
