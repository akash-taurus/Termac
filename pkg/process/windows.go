//go:build windows

package process

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

const (
	// CREATE_NO_WINDOW prevents creation of a console window when invoking taskkill.
	CREATE_NO_WINDOW = 0x08000000

	// ExitCodeProcessNotFound is returned by taskkill when the target PID does not exist (already terminated).
	ExitCodeProcessNotFound = 128
)

// taskkillRunner abstracts execution of taskkill to support unit test mocking.
type taskkillRunner func(pid int) ([]byte, error)

// runTaskkill is the active taskkill execution function, swappable in unit tests.
var runTaskkill taskkillRunner = defaultRunTaskkill

func defaultRunTaskkill(pid int) ([]byte, error) {
	cmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: CREATE_NO_WINDOW,
	}
	return cmd.CombinedOutput()
}

// KillProcessTree forcefully terminates the target process and all child/descendant
// processes on Windows using taskkill /F /T /PID.
//
// Behavior:
// - Returns ErrInvalidPID if pid <= 0 (preventing System Idle Process / PID 0 hazard).
// - Returns nil if the process tree was killed successfully (exit code 0).
// - Returns nil if the process was already terminated or not found (exit code 128).
// - Returns a descriptive error containing taskkill output on any other non-zero exit code.
func KillProcessTree(pid int) error {
	if pid <= 0 {
		return ErrInvalidPID
	}

	out, err := runTaskkill(pid)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if exitErr.ExitCode() == ExitCodeProcessNotFound {
				// Process not found or already dead; idempotent success.
				return nil
			}
		}

		outStr := strings.TrimSpace(string(out))
		if outStr != "" {
			return fmt.Errorf("taskkill failed for PID %d: %w (%s)", pid, err, outStr)
		}
		return fmt.Errorf("taskkill failed for PID %d: %w", pid, err)
	}

	return nil
}
