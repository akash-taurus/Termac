//go:build windows

package shell

import (
	"testing"
)

func TestOpenFolderDialog_Initialization(t *testing.T) {
	// Verify Win32 DLL procs are loaded and functional
	if procSHBrowseForFolderW.Find() != nil {
		t.Fatalf("SHBrowseForFolderW not found in shell32.dll")
	}
	if procSHGetPathFromIDListW.Find() != nil {
		t.Fatalf("SHGetPathFromIDListW not found in shell32.dll")
	}
	if procCoInitializeEx.Find() != nil {
		t.Fatalf("CoInitializeEx not found in ole32.dll")
	}
	if procCoTaskMemFree.Find() != nil {
		t.Fatalf("CoTaskMemFree not found in ole32.dll")
	}
}
