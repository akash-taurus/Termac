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
// Returns nil on success, or an error if mouse input cannot be enabled
func EnableMouseInput() error {
	handle := windows.Handle(os.Stdout.Fd())
	var mode uint32

	// Get current console mode
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return fmt.Errorf("GetConsoleMode failed: %w", err)
	}

	// Enable mouse input and disable quick edit mode
	newMode := (mode | ENABLE_MOUSE_INPUT) &^ ENABLE_QUICK_EDIT_MODE
	if err := windows.SetConsoleMode(handle, newMode); err != nil {
		return fmt.Errorf("SetConsoleMode failed: %w", err)
	}

	return nil
}

// DisableMouseInput disables mouse input
func DisableMouseInput() error {
	handle := windows.Handle(os.Stdout.Fd())
	var mode uint32

	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return fmt.Errorf("GetConsoleMode failed: %w", err)
	}

	newMode := mode &^ ENABLE_MOUSE_INPUT
	if err := windows.SetConsoleMode(handle, newMode); err != nil {
		return fmt.Errorf("SetConsoleMode failed: %w", err)
	}

	return nil
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