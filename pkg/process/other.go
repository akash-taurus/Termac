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
// - Returns ErrInvalidPID if pid <= 0 or pid == 1 (never kill init).
// - Sends SIGKILL to the process group (-pid). Callers must ensure the
// - target was started with Setpgid=true (see launcher.applyPlatformAttributes),
// - otherwise -pid may target the wrong group.
// - If ESRCH is returned, returns nil (idempotent success).
// - If process group kill fails, falls back to killing the single process (pid).
// - Note: children that called setsid() escape the group kill and may leak.
func KillProcessTree(pid int) error {
	if pid <= 0 || pid == 1 {
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

	return errors.Join(
		fmt.Errorf("failed to kill process group %d: %w", pid, err),
		fmt.Errorf("fallback single kill %d failed: %w", pid, fallbackErr),
	)
}
