//go:build !windows

package term

import "fmt"

// EnableWindowsVirtualTerminal is a no-op on non-Windows platforms
func EnableWindowsVirtualTerminal() error {
	return nil
}

// EnableMouseInput is a no-op on non-Windows platforms
func EnableMouseInput() error {
	return nil
}

// DisableMouseInput is a no-op on non-Windows platforms
func DisableMouseInput() error {
	return nil
}

// CheckVTPEnabled returns false on non-Windows platforms
func CheckVTPEnabled() bool {
	return false
}