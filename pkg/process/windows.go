//go:build windows

package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// CREATE_NO_WINDOW prevents creation of a console window when invoking taskkill.
	CREATE_NO_WINDOW = 0x08000000

	// ExitCodeProcessNotFound is returned by taskkill when the target PID does not exist (already terminated).
	ExitCodeProcessNotFound = 128
)

// taskkillRunner abstracts execution of taskkill to support unit test mocking.
type taskkillRunner func(pid int) ([]byte, error)

// runTaskkillMu guards runTaskkill against concurrent swap/use data races.
var runTaskkillMu sync.RWMutex

// runTaskkill is the active taskkill execution function, swappable in unit tests.
var runTaskkill taskkillRunner = defaultRunTaskkill

// SetTaskkillRunner swaps the runner under lock (test helper to avoid races).
func SetTaskkillRunner(fn taskkillRunner) {
	runTaskkillMu.Lock()
	defer runTaskkillMu.Unlock()
	runTaskkill = fn
}

func getTaskkillRunner() taskkillRunner {
	runTaskkillMu.RLock()
	defer runTaskkillMu.RUnlock()
	return runTaskkill
}

// taskkillPath resolves %SystemRoot%\System32\taskkill.exe to avoid PATH hijack.
func taskkillPath() string {
	if sysRoot := os.Getenv("SystemRoot"); sysRoot != "" {
		abs := filepath.Join(sysRoot, "System32", "taskkill.exe")
		if _, err := os.Stat(abs); err == nil {
			return abs
		}
	}
	if p, err := exec.LookPath("taskkill.exe"); err == nil {
		return p
	}
	return "taskkill"
}

func defaultRunTaskkill(pid int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, taskkillPath(), "/F", "/T", "/PID", strconv.Itoa(pid))
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

	out, err := getTaskkillRunner()(pid)
	if err != nil {
		outStr := strings.TrimSpace(string(out))
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if exitErr.ExitCode() == ExitCodeProcessNotFound {
				// Process not found or already dead; idempotent success.
				return nil
			}
			// Defense for localized Windows where message differs but
			// process is already gone. taskkill reports e.g.:
			// "There is no running instance of the task."
			lower := strings.ToLower(outStr)
			if strings.Contains(lower, "not found") || strings.Contains(lower, "not exist") ||
				strings.Contains(lower, "no running instance") || strings.Contains(lower, "no such process") {
				return nil
			}
		}
		// Context timeout surfaces as context.DeadlineExceeded wrapped by exec.
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(outStr, "DeadlineExceeded") {
			return fmt.Errorf("taskkill timed out for PID %d: %w (%s)", pid, err, outStr)
		}
		if outStr != "" {
			return fmt.Errorf("taskkill failed for PID %d: %w (%s)", pid, err, outStr)
		}
		return fmt.Errorf("taskkill failed for PID %d: %w", pid, err)
	}

	return nil
}
