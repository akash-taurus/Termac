//go:build windows

package term

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

const (
	// ENABLE_VIRTUAL_TERMINAL_PROCESSING enables ANSI escape sequence processing
	ENABLE_VIRTUAL_TERMINAL_PROCESSING = 0x0004
	// ENABLE_MOUSE_INPUT enables mouse input
	ENABLE_MOUSE_INPUT = 0x0010
	// ENABLE_QUICK_EDIT_MODE is used to disable quick edit mode
	ENABLE_QUICK_EDIT_MODE = 0x0040
	// ENABLE_EXTENDED_FLAGS is required for quick-edit changes to take effect
	ENABLE_EXTENDED_FLAGS = 0x0080
	// CP_UTF8 is the Windows code page for UTF-8 encoding
	CP_UTF8 = 65001
)

// ErrVTPNotSupported is returned when Virtual Terminal Processing cannot be enabled
var ErrVTPNotSupported = fmt.Errorf("virtual terminal processing not supported")

// EnableWindowsVirtualTerminal enables ANSI escape sequence processing (VT100) on Windows
// Returns nil on success, or an error if VTP cannot be enabled
func EnableWindowsVirtualTerminal() error {
	handle := windows.Handle(os.Stdout.Fd())
	var mode uint32

	// Get current console mode
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return fmt.Errorf("GetConsoleMode failed: %w", err)
	}

	// Enable virtual terminal processing
	newMode := mode | ENABLE_VIRTUAL_TERMINAL_PROCESSING
	if err := windows.SetConsoleMode(handle, newMode); err != nil {
		return fmt.Errorf("SetConsoleMode failed: %w", err)
	}

	// Set console code page to UTF-8 (65001) for proper unicode/box drawing rendering
	_ = windows.SetConsoleOutputCP(CP_UTF8)
	_ = windows.SetConsoleCP(CP_UTF8)

	return nil
}

// EnableMouseInput enables mouse input and disables quick edit mode on Windows
// Returns nil on success, or an error if mouse input cannot be enabled.
// Uses STD_INPUT_HANDLE: mouse/quick-edit are INPUT modes, not OUTPUT.
func EnableMouseInput() error {
	handle := windows.Handle(windows.STD_INPUT_HANDLE)
	// windows.STD_INPUT_HANDLE const is int; GetStdHandle indirection needed
	// when Stdout is redirected. Resolve the real input handle:
	if h, err := windows.GetStdHandle(windows.STD_INPUT_HANDLE); err == nil {
		handle = h
	}
	if ft, _ := windows.GetFileType(handle); ft != windows.FILE_TYPE_CHAR {
		return nil // no console (redirected); nothing to do
	}
	var mode uint32

	// Get current console mode
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return fmt.Errorf("GetConsoleMode failed: %w", err)
	}

	// Enable mouse input and disable quick edit mode (EXTENDED_FLAGS required)
	newMode := (mode | ENABLE_MOUSE_INPUT | ENABLE_EXTENDED_FLAGS) &^ ENABLE_QUICK_EDIT_MODE
	if err := windows.SetConsoleMode(handle, newMode); err != nil {
		return fmt.Errorf("SetConsoleMode failed: %w", err)
	}

	return nil
}

// DisableMouseInput disables mouse input
func DisableMouseInput() error {
	handle := handleInput()
	var mode uint32

	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		// Redirected stdout: GetConsoleMode fails with INVALID_HANDLE; not an error.
		return nil
	}

	newMode := mode &^ ENABLE_MOUSE_INPUT
	if err := windows.SetConsoleMode(handle, newMode); err != nil {
		return fmt.Errorf("SetConsoleMode failed: %w", err)
	}

	return nil
}

func handleInput() windows.Handle {
	if h, err := windows.GetStdHandle(windows.STD_INPUT_HANDLE); err == nil {
		if ft, _ := windows.GetFileType(h); ft == windows.FILE_TYPE_CHAR {
			return h
		}
		// Redirected: return handle anyway; callers treat GetConsoleMode
		// failure as no-console.
		return h
	}
	return windows.Handle(os.Stdout.Fd())
}

// CheckVTPEnabled returns true if Virtual Terminal Processing is already enabled
func CheckVTPEnabled() bool {
	handle := windows.Handle(os.Stdout.Fd())
	var mode uint32

	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return false
	}

	return (mode & ENABLE_VIRTUAL_TERMINAL_PROCESSING) != 0
}
