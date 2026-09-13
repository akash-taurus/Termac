//go:build windows

package shell

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	shell32 = syscall.NewLazyDLL("shell32.dll")
	ole32   = syscall.NewLazyDLL("ole32.dll")

	procCoInitializeEx       = ole32.NewProc("CoInitializeEx")
	procCoUninitialize       = ole32.NewProc("CoUninitialize")
	procSHBrowseForFolderW   = shell32.NewProc("SHBrowseForFolderW")
	procSHGetPathFromIDListW = shell32.NewProc("SHGetPathFromIDListW")
	procCoTaskMemFree        = ole32.NewProc("CoTaskMemFree")
)

type BROWSEINFOW struct {
	HwndOwner      uintptr
	PidlRoot       uintptr
	PszDisplayName *uint16
	LpszTitle      *uint16
	UlFlags        uint32
	Lpfn           uintptr
	LParam         uintptr
	IImage         int32
}

const (
	BIF_RETURNONLYFSDIRS = 0x00000001
	BIF_NEWDIALOGSTYLE   = 0x00000040
	BIF_EDITBOX          = 0x00000010
	BIF_USENEWUI         = BIF_NEWDIALOGSTYLE | BIF_EDITBOX
	COINIT_APARTMENT     = 0x2
)

// OpenFolderDialog launches a native Windows GUI Folder Browser Dialog
// allowing the user to navigate through drives, folders, and subfolders visually.
// It returns the selected absolute folder path, or an empty string if canceled.
func OpenFolderDialog(title string) (string, error) {
	if title == "" {
		title = "Select a local folder to open in Terminal Dashboard"
	}

	// Try modern Windows Vista/10/11 File Explorer FolderBrowserDialog via PowerShell first
	path, err := openFolderViaPowerShell(title)
	if err == nil {
		return strings.TrimSpace(path), nil
	}

	// Fallback to native Win32 SHBrowseForFolderW
	return openFolderViaSHBrowse(title)
}

func openFolderViaPowerShell(title string) (string, error) {
	escapedTitle := strings.ReplaceAll(title, "'", "''")
	psScript := fmt.Sprintf(
		`Add-Type -AssemblyName System.Windows.Forms; `+
			`$f = New-Object System.Windows.Forms.FolderBrowserDialog; `+
			`$f.Description = '%s'; `+
			`$f.AutoUpgradeEnabled = $true; `+
			`$f.ShowNewFolderButton = $true; `+
			`[System.Windows.Forms.Application]::EnableVisualStyles(); `+
			`$result = $f.ShowDialog(); `+
			`if ($result -eq [System.Windows.Forms.DialogResult]::OK) { `+
			`[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; `+
			`[Console]::Out.Write($f.SelectedPath) `+
			`}`,
		escapedTitle,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-STA", "-Command", psScript)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("powershell dialog error: %v, stderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

func openFolderViaSHBrowse(title string) (string, error) {
	procCoInitializeEx.Call(0, COINIT_APARTMENT)
	defer procCoUninitialize.Call()

	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return "", err
	}

	displayBuf := make([]uint16, 260)
	bi := BROWSEINFOW{
		LpszTitle:      titlePtr,
		UlFlags:        BIF_RETURNONLYFSDIRS | BIF_USENEWUI,
		PszDisplayName: &displayBuf[0],
	}

	pidl, _, _ := procSHBrowseForFolderW.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return "", nil // User cancelled
	}
	defer procCoTaskMemFree.Call(pidl)

	pathBuf := make([]uint16, 32768)
	ret, _, _ := procSHGetPathFromIDListW.Call(pidl, uintptr(unsafe.Pointer(&pathBuf[0])))
	if ret == 0 {
		return "", fmt.Errorf("failed to retrieve path from folder ID list")
	}

	return syscall.UTF16ToString(pathBuf), nil
}
