//go:build !windows

package shell

import "fmt"

// OpenFolderDialog fallback for non-Windows platforms
func OpenFolderDialog(title string) (string, error) {
	return "", fmt.Errorf("GUI folder dialog is only supported on Windows")
}
