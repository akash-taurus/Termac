//go:build !windows

package launcher

import "os/exec"

func applyPlatformAttributes(cmd *exec.Cmd) {
	// No-op for non-Windows platforms to ensure cross-compilation success.
}
