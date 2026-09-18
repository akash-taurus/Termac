//go:build !windows

package launcher

import (
	"os/exec"
	"syscall"
)

func applyPlatformAttributes(cmd *exec.Cmd) {
	// Put plugin in its own process group so Kill(-pid) targets only
	// the plugin tree, never the dashboard's group.
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}
