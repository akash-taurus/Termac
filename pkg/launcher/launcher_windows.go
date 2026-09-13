//go:build windows

package launcher

import (
	"os/exec"
	"syscall"
)

const (
	// CREATE_NO_WINDOW prevents creation of a separate console window on Windows.
	CREATE_NO_WINDOW = 0x08000000
)

func applyPlatformAttributes(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= CREATE_NO_WINDOW
}
