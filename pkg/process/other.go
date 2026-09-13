//go:build !windows

package process

import (
	"errors"
	"fmt"
	"syscall"
)

// KillProcessTree forcefully terminates the target process and its process group on POSIX systems.
//
// Behavior:
// - Returns ErrInvalidPID if pid <= 0.
// - Sends SIGKILL to the process group (-pid).
// - If ESRCH is returned, returns nil (idempotent success).
// - If process group kill fails, falls back to killing the single process (pid).
func KillProcessTree(pid int) error {
	if pid <= 0 {
		return ErrInvalidPID
	}

	// In POSIX, a negative PID (-pid) signals the entire process group.
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if err == nil || errors.Is(err, syscall.ESRCH) {
		return nil
	}

	// Fallback to single process kill if process group kill is not permitted or process was not group leader
	fallbackErr := syscall.Kill(pid, syscall.SIGKILL)
	if fallbackErr == nil || errors.Is(fallbackErr, syscall.ESRCH) {
		return nil
	}

	return fmt.Errorf("failed to kill process %d: %w", pid, err)
}
