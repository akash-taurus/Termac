//go:build windows

package launcher

import (
	"context"
	"testing"
)

// TestPrepareCommand_WindowsAttributes verifies hidden-window process
// attributes on Windows. Kept in a windows-tagged file because
// syscall.SysProcAttr.HideWindow/CreationFlags and CREATE_NO_WINDOW
// do not exist on other platforms.
func TestPrepareCommand_WindowsAttributes(t *testing.T) {
	l := New()
	ctx := context.Background()

	cmd, err := l.PrepareCommand(ctx, Config{PluginPath: "test.exe"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cmd.SysProcAttr == nil {
		t.Fatal("expected cmd.SysProcAttr to be non-nil on Windows")
	}

	if !cmd.SysProcAttr.HideWindow {
		t.Error("expected cmd.SysProcAttr.HideWindow to be true")
	}

	if cmd.SysProcAttr.CreationFlags&CREATE_NO_WINDOW == 0 {
		t.Errorf("expected CREATE_NO_WINDOW flag (0x08000000) set in CreationFlags: 0x%08x", cmd.SysProcAttr.CreationFlags)
	}
}
