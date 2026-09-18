//go:build windows

package launcher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
)

// TestBoundary_MissingInterpreters tests all missing interpreter scenarios and exact error contracts.
func TestBoundary_MissingInterpreters(t *testing.T) {
	ctx := context.Background()

	t.Run("Python Missing - Exact Error and Sentinels", func(t *testing.T) {
		customErr := errors.New("underlying lookup failure: file not found in path")
		l := New(WithLookPath(func(file string) (string, error) {
			if file == "python" || file == "python3" || file == "py" {
				return "", customErr
			}
			return "C:\\dummy\\path.exe", nil
		}))

		cmd, err := l.PrepareCommand(ctx, Config{PluginPath: "script.py"})
		if cmd != nil {
			t.Errorf("expected cmd to be nil on error, got %v", cmd)
		}
		if err == nil {
			t.Fatal("expected error for missing python, got nil")
		}

		// Exact string check per PROJECT.md line 82
		const expectedPyMsg = "Python interpreter not found in PATH. Please install Python to use this plugin."
		if err.Error() != expectedPyMsg {
			t.Errorf("error string mismatch:\nexpected: %q\ngot:      %q", expectedPyMsg, err.Error())
		}

		// errors.Is check against ErrPythonNotFound sentinel
		if !errors.Is(err, ErrPythonNotFound) {
			t.Errorf("expected errors.Is(err, ErrPythonNotFound) to be true")
		}

		// errors.As check for structured *InterpreterNotFoundError
		var interpErr *InterpreterNotFoundError
		if !errors.As(err, &interpErr) {
			t.Fatalf("expected *InterpreterNotFoundError, got %T", err)
		}
		if interpErr.Interpreter != "python" {
			t.Errorf("expected Interpreter 'python', got %q", interpErr.Interpreter)
		}
		if interpErr.Message != expectedPyMsg {
			t.Errorf("expected Message %q, got %q", expectedPyMsg, interpErr.Message)
		}
		if !errors.Is(interpErr.Unwrap(), customErr) {
			t.Errorf("expected unwrapped error to be customErr, got %v", interpErr.Unwrap())
		}

		// Launch should also return this exact error
		_, launchErr := l.Launch(ctx, Config{PluginPath: "script.py"})
		if launchErr == nil || launchErr.Error() != expectedPyMsg {
			t.Errorf("Launch error expected %q, got %v", expectedPyMsg, launchErr)
		}
	})

	t.Run("Node Missing - Exact Error and Sentinels", func(t *testing.T) {
		customErr := errors.New("node not found in PATH")
		l := New(WithLookPath(func(file string) (string, error) {
			if file == "node" || file == "nodejs" {
				return "", customErr
			}
			return "C:\\dummy\\path.exe", nil
		}))

		cmd, err := l.PrepareCommand(ctx, Config{PluginPath: "widget.js"})
		if cmd != nil {
			t.Errorf("expected cmd to be nil on error, got %v", cmd)
		}
		if err == nil {
			t.Fatal("expected error for missing node, got nil")
		}

		// Exact string check per PROJECT.md line 83
		const expectedNodeMsg = "Node.js interpreter not found in PATH. Please install Node.js to use this plugin."
		if err.Error() != expectedNodeMsg {
			t.Errorf("error string mismatch:\nexpected: %q\ngot:      %q", expectedNodeMsg, err.Error())
		}

		// errors.Is check against ErrNodeNotFound sentinel
		if !errors.Is(err, ErrNodeNotFound) {
			t.Errorf("expected errors.Is(err, ErrNodeNotFound) to be true")
		}

		// errors.As check
		var interpErr *InterpreterNotFoundError
		if !errors.As(err, &interpErr) {
			t.Fatalf("expected *InterpreterNotFoundError, got %T", err)
		}
		if interpErr.Interpreter != "node" {
			t.Errorf("expected Interpreter 'node', got %q", interpErr.Interpreter)
		}
		if !errors.Is(interpErr.Unwrap(), customErr) {
			t.Errorf("expected unwrapped error to be customErr, got %v", interpErr.Unwrap())
		}

		// Launch should also return this exact error
		_, launchErr := l.Launch(ctx, Config{PluginPath: "widget.js"})
		if launchErr == nil || launchErr.Error() != expectedNodeMsg {
			t.Errorf("Launch error expected %q, got %v", expectedNodeMsg, launchErr)
		}
	})

	t.Run("Cmd Missing - Exact Error and Fallbacks", func(t *testing.T) {
		// Test when all lookups fail (cmd.exe, cmd, COMSPEC)
		calls := []string{}
		l := New(WithLookPath(func(file string) (string, error) {
			calls = append(calls, file)
			return "", exec.ErrNotFound
		}))

		cmd, err := l.PrepareCommand(ctx, Config{PluginPath: "run.bat"})
		if cmd != nil {
			t.Errorf("expected cmd to be nil on error, got %v", cmd)
		}
		if err == nil {
			t.Fatal("expected error for missing cmd.exe, got nil")
		}

		const expectedCmdMsg = "Command prompt interpreter (cmd.exe) not found in PATH."
		if err.Error() != expectedCmdMsg {
			t.Errorf("error string mismatch:\nexpected: %q\ngot:      %q", expectedCmdMsg, err.Error())
		}

		if !errors.Is(err, ErrCmdNotFound) {
			t.Errorf("expected errors.Is(err, ErrCmdNotFound) to be true")
		}

		// Verify fallback sequence: cmd.exe, cmd, and COMSPEC
		if len(calls) < 2 {
			t.Errorf("expected at least cmd.exe and cmd lookups, got: %v", calls)
		}
		if calls[0] != "cmd.exe" || calls[1] != "cmd" {
			t.Errorf("expected first calls to be cmd.exe and cmd, got: %v", calls)
		}

		// Test for .cmd extension as well
		_, errCmdExt := l.PrepareCommand(ctx, Config{PluginPath: "run.cmd"})
		if errCmdExt == nil || errCmdExt.Error() != expectedCmdMsg {
			t.Errorf("expected .cmd to return ErrCmdNotFoundMsg, got %v", errCmdExt)
		}
	})

	t.Run("Cmd Fallback to COMSPEC", func(t *testing.T) {
		// Mock where cmd.exe and cmd fail, but COMSPEC lookup succeeds
		targetComSpec := "C:\\Custom\\cmd.exe"
		t.Setenv("COMSPEC", targetComSpec)

		l := New(WithLookPath(func(file string) (string, error) {
			if file == targetComSpec {
				return targetComSpec, nil
			}
			return "", exec.ErrNotFound
		}))

		cmd, err := l.PrepareCommand(ctx, Config{PluginPath: "runner.bat"})
		if err != nil {
			t.Fatalf("expected COMSPEC fallback to succeed, got %v", err)
		}
		if cmd.Path != targetComSpec {
			t.Errorf("expected cmd.Path %q, got %q", targetComSpec, cmd.Path)
		}
	})
}

// TestBoundary_UnsupportedExtensions tests unsupported extensions and malformed paths.
func TestBoundary_UnsupportedExtensions(t *testing.T) {
	l := New()
	ctx := context.Background()

	testCases := []struct {
		name string
		path string
	}{
		{"Shell script", "script.sh"},
		{"Ruby script", "script.rb"},
		{"Perl script", "script.pl"},
		{"PowerShell script", "script.ps1"},
		{"Random extension", "plugin.xyz"},
		{"No extension", "plugin_binary"},
		{"Dotfile with no extension", ".gitignore"},
		{"Dotfile with unsupported ext", ".env.local"},
		{"Directory traversal no ext", "../some/path/bin"},
		{"Double extension ending in unsupported", "app.exe.bak"},
		{"Archive extension", "bundle.tar.gz"},
		{"Trailing dot", "myplugin."},
		{"Single dot", "."},
		{"Double dot", ".."},
		{"CCOM executable unsupported", "program.com"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, err := l.PrepareCommand(ctx, Config{PluginPath: tc.path})
			if cmd != nil {
				t.Errorf("expected cmd to be nil for unsupported extension %q, got %v", tc.path, cmd)
			}
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc.path)
			}
			if !errors.Is(err, ErrUnsupportedExtension) {
				t.Errorf("expected errors.Is(err, ErrUnsupportedExtension) for %q, got: %v", tc.path, err)
			}
		})
	}
}

// TestBoundary_EmptyAndBlankPaths tests empty and whitespace paths.
func TestBoundary_EmptyAndBlankPaths(t *testing.T) {
	l := New()
	ctx := context.Background()

	blankCases := []struct {
		name string
		path string
	}{
		{"Empty string", ""},
		{"Single space", " "},
		{"Multiple spaces", "     "},
		{"Tab", "\t"},
		{"Newline", "\n"},
		{"Carriage return and newline", "\r\n"},
		{"Mixed whitespace", " \t \r\n \v "},
	}

	for _, tc := range blankCases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, err := l.PrepareCommand(ctx, Config{PluginPath: tc.path})
			if cmd != nil {
				t.Errorf("expected cmd nil for %s, got %v", tc.name, cmd)
			}
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
			if !errors.Is(err, ErrEmptyPluginPath) {
				t.Errorf("expected errors.Is(err, ErrEmptyPluginPath) for %s, got: %v", tc.name, err)
			}

			// Launch should also immediately return ErrEmptyPluginPath
			_, launchErr := l.Launch(ctx, Config{PluginPath: tc.path})
			if !errors.Is(launchErr, ErrEmptyPluginPath) {
				t.Errorf("expected Launch to return ErrEmptyPluginPath for %s, got: %v", tc.name, launchErr)
			}
		})
	}
}

// TestBoundary_WindowsProcessCreationFlags verifies that SysProcAttr is correctly configured
// on Windows for all supported extension types.
func TestBoundary_WindowsProcessCreationFlags(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only process creation flags verification")
	}

	mockPython := "C:\\Python\\python.exe"
	mockNode := "C:\\Node\\node.exe"
	mockCmd := "C:\\Windows\\System32\\cmd.exe"

	l := New(WithLookPath(func(file string) (string, error) {
		switch file {
		case "python":
			return mockPython, nil
		case "node":
			return mockNode, nil
		case "cmd.exe", "cmd":
			return mockCmd, nil
		default:
			return file, nil
		}
	}))

	ctx := context.Background()

	cases := []struct {
		ext      string
		filename string
	}{
		{".exe", "service.exe"},
		{".EXE", "SERVICE.EXE"},
		{".py", "worker.py"},
		{".Py", "WORKER.PY"},
		{".js", "index.js"},
		{".JS", "INDEX.JS"},
		{".bat", "task.bat"},
		{".BAT", "TASK.BAT"},
		{".cmd", "task.cmd"},
		{".CMD", "TASK.CMD"},
	}

	const CREATE_NO_WINDOW_FLAG = 0x08000000

	for _, tc := range cases {
		t.Run(tc.filename, func(t *testing.T) {
			cmd, err := l.PrepareCommand(ctx, Config{PluginPath: tc.filename})
			if err != nil {
				t.Fatalf("PrepareCommand failed for %s: %v", tc.filename, err)
			}

			if cmd.SysProcAttr == nil {
				t.Fatalf("cmd.SysProcAttr is nil for %s", tc.filename)
			}

			// Verify HideWindow is true
			if !cmd.SysProcAttr.HideWindow {
				t.Errorf("cmd.SysProcAttr.HideWindow is false for %s, expected true", tc.filename)
			}

			// Verify CreationFlags contains CREATE_NO_WINDOW (0x08000000)
			if cmd.SysProcAttr.CreationFlags&CREATE_NO_WINDOW_FLAG != CREATE_NO_WINDOW_FLAG {
				t.Errorf("cmd.SysProcAttr.CreationFlags for %s is 0x%08x, missing CREATE_NO_WINDOW (0x%08x)",
					tc.filename, cmd.SysProcAttr.CreationFlags, CREATE_NO_WINDOW_FLAG)
			}
		})
	}
}

// TestBoundary_DirectoryPaths tests passing directory paths to PrepareCommand and Launch.
func TestBoundary_DirectoryPaths(t *testing.T) {
	ctx := context.Background()
	l := New()

	tempDir := t.TempDir()

	// 1. Directory with no extension (e.g. tempDir itself)
	t.Run("Directory without extension", func(t *testing.T) {
		cmd, err := l.PrepareCommand(ctx, Config{PluginPath: tempDir})
		if cmd != nil {
			t.Errorf("expected cmd nil, got %v", cmd)
		}
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, ErrUnsupportedExtension) {
			t.Errorf("expected ErrUnsupportedExtension, got %v", err)
		}
	})

	// 2. Directory named with a valid extension (e.g. folder.exe, folder.py)
	t.Run("Directory named with .exe extension", func(t *testing.T) {
		dirWithExe := filepath.Join(tempDir, "fake_app.exe")
		if err := os.Mkdir(dirWithExe, 0755); err != nil {
			t.Fatalf("failed to create directory: %v", err)
		}

		// PrepareCommand routes based on extension
		cmd, err := l.PrepareCommand(ctx, Config{PluginPath: dirWithExe})
		if err != nil {
			t.Fatalf("PrepareCommand returned error: %v", err)
		}
		if cmd == nil {
			t.Fatal("cmd is nil")
		}

		// Launch should attempt to execute the directory as an executable, which the OS will reject
		_, launchErr := l.Launch(ctx, Config{PluginPath: dirWithExe})
		if launchErr == nil {
			t.Fatal("expected Launch of directory to fail, but it succeeded")
		}
		if !strings.Contains(launchErr.Error(), "failed to start plugin process") {
			t.Errorf("expected 'failed to start plugin process' in error, got %v", launchErr)
		}
	})

	t.Run("Directory named with .py extension", func(t *testing.T) {
		if _, err := exec.LookPath("python"); err != nil {
			t.Skip("Python not installed on host")
		}

		dirWithPy := filepath.Join(tempDir, "fake_script.py")
		if err := os.Mkdir(dirWithPy, 0755); err != nil {
			t.Fatalf("failed to create directory: %v", err)
		}

		cmd, err := l.PrepareCommand(ctx, Config{PluginPath: dirWithPy})
		if err != nil {
			t.Fatalf("PrepareCommand failed: %v", err)
		}

		// Running python on a directory fails with error
		err = cmd.Run()
		if err == nil {
			t.Error("expected running python on a directory to fail, but it succeeded")
		}
	})
}

// TestBoundary_NonExistentFiles tests behavior with non-existent plugin files.
func TestBoundary_NonExistentFiles(t *testing.T) {
	ctx := context.Background()
	l := New()

	nonExistentExe := filepath.Join(t.TempDir(), "ghost_plugin_12345.exe")

	t.Run("Non-existent .exe Launch", func(t *testing.T) {
		// PrepareCommand creates the command structure
		cmd, err := l.PrepareCommand(ctx, Config{PluginPath: nonExistentExe})
		if err != nil {
			t.Fatalf("PrepareCommand unexpectedly failed: %v", err)
		}
		if cmd == nil {
			t.Fatal("PrepareCommand returned nil cmd")
		}

		// Launch calls cmd.Start(), which must fail because the file does not exist
		_, launchErr := l.Launch(ctx, Config{PluginPath: nonExistentExe})
		if launchErr == nil {
			t.Fatal("expected Launch to fail for non-existent executable, got nil")
		}

		if !strings.Contains(launchErr.Error(), "failed to start plugin process") {
			t.Errorf("expected 'failed to start plugin process' in error, got: %v", launchErr)
		}
	})

	t.Run("Non-existent .py Execution", func(t *testing.T) {
		if _, err := exec.LookPath("python"); err != nil {
			t.Skip("Python not installed on host")
		}

		nonExistentPy := filepath.Join(t.TempDir(), "ghost_plugin_12345.py")
		cmd, err := l.PrepareCommand(ctx, Config{PluginPath: nonExistentPy})
		if err != nil {
			t.Fatalf("PrepareCommand failed: %v", err)
		}
		if cmd == nil {
			t.Fatal("expected cmd non-nil")
		}

		// Launch starts python (which succeeds since python exists)
		activeCmd, launchErr := l.Launch(ctx, Config{PluginPath: nonExistentPy})
		if launchErr != nil {
			t.Fatalf("Launch of python process failed to start: %v", launchErr)
		}

		// But when the process exits, python should exit with non-zero code because file doesn't exist
		waitErr := activeCmd.Wait()
		if waitErr == nil {
			t.Fatal("expected python to exit with error when target script is missing")
		}
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			t.Errorf("expected *exec.ExitError, got: %T (%v)", waitErr, waitErr)
		} else if exitErr.ExitCode() == 0 {
			t.Errorf("expected non-zero exit code, got %d", exitErr.ExitCode())
		}
	})

	t.Run("Non-existent .js Execution", func(t *testing.T) {
		if _, err := exec.LookPath("node"); err != nil {
			t.Skip("Node not installed on host")
		}

		nonExistentJs := filepath.Join(t.TempDir(), "ghost_plugin_12345.js")
		cmd, err := l.PrepareCommand(ctx, Config{PluginPath: nonExistentJs})
		if err != nil {
			t.Fatalf("PrepareCommand failed: %v", err)
		}
		if cmd == nil {
			t.Fatal("expected cmd non-nil")
		}

		activeCmd, launchErr := l.Launch(ctx, Config{PluginPath: nonExistentJs})
		if launchErr != nil {
			t.Fatalf("Launch of node process failed to start: %v", launchErr)
		}

		waitErr := activeCmd.Wait()
		if waitErr == nil {
			t.Fatal("expected node to exit with error when target script is missing")
		}
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			t.Errorf("expected *exec.ExitError, got: %T (%v)", waitErr, waitErr)
		} else if exitErr.ExitCode() == 0 {
			t.Errorf("expected non-zero exit code, got %d", exitErr.ExitCode())
		}
	})
}

// TestBoundary_SysProcAttrPreservation verifies that if a custom CommandContextFunc sets
// initial SysProcAttr fields, applyPlatformAttributes does not wipe them out.
func TestBoundary_SysProcAttrPreservation(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}

	ctx := context.Background()
	customToken := syscall.Token(1234)

	l := New(WithCommandContext(func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, name, arg...)
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Token:         customToken,
			CreationFlags: 0x00000004, // e.g. CREATE_SUSPENDED
		}
		return cmd
	}))

	cmd, err := l.PrepareCommand(ctx, Config{PluginPath: "test.exe"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cmd.SysProcAttr.Token != customToken {
		t.Errorf("expected Token %v preserved, got %v", customToken, cmd.SysProcAttr.Token)
	}

	// Verify CREATE_NO_WINDOW was bitwise OR'd without clearing existing flags
	expectedFlags := uint32(0x00000004 | 0x08000000)
	if cmd.SysProcAttr.CreationFlags != expectedFlags {
		t.Errorf("expected CreationFlags 0x%08x, got 0x%08x", expectedFlags, cmd.SysProcAttr.CreationFlags)
	}

	if !cmd.SysProcAttr.HideWindow {
		t.Error("expected HideWindow to be true")
	}
}

// TestBoundary_ConcurrentPrepare verifies thread safety of Launcher when called concurrently.
func TestBoundary_ConcurrentPrepare(t *testing.T) {
	mockPython := "C:\\python.exe"
	mockNode := "C:\\node.exe"
	mockCmd := "C:\\cmd.exe"

	l := New(WithLookPath(func(file string) (string, error) {
		switch file {
		case "python":
			return mockPython, nil
		case "node":
			return mockNode, nil
		case "cmd.exe", "cmd":
			return mockCmd, nil
		default:
			return "", exec.ErrNotFound
		}
	}))

	ctx := context.Background()
	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	errChan := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			var path string
			switch idx % 5 {
			case 0:
				path = fmt.Sprintf("C:\\app_%d.exe", idx)
			case 1:
				path = fmt.Sprintf("scripts\\worker_%d.py", idx)
			case 2:
				path = fmt.Sprintf("ui\\widget_%d.js", idx)
			case 3:
				path = fmt.Sprintf("batch\\runner_%d.bat", idx)
			case 4:
				path = fmt.Sprintf("batch\\runner_%d.cmd", idx)
			}

			cmd, err := l.PrepareCommand(ctx, Config{
				PluginPath: path,
				Args:       []string{fmt.Sprintf("--id=%d", idx)},
				Env:        []string{fmt.Sprintf("THREAD_ID=%d", idx)},
			})
			if err != nil {
				errChan <- fmt.Errorf("goroutine %d failed: %w", idx, err)
				return
			}
			if cmd == nil {
				errChan <- fmt.Errorf("goroutine %d returned nil cmd", idx)
				return
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Errorf("concurrent test error: %v", err)
	}
}

// TestBoundary_CmdQuotingAnomaly empirically demonstrates the Windows cmd.exe /c quoting
// behavior when both the batch file path and an argument contain whitespace.
func TestBoundary_CmdQuotingAnomaly(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific cmd.exe test")
	}

	tempDir := t.TempDir()
	dirWithSpace := filepath.Join(tempDir, "Folder With Spaces")
	if err := os.MkdirAll(dirWithSpace, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	batPath := filepath.Join(dirWithSpace, "target script.bat")
	batContent := "@echo off\r\necho HELLO_BATCH: %1\r\n"
	if err := os.WriteFile(batPath, []byte(batContent), 0644); err != nil {
		t.Fatalf("failed to write batch: %v", err)
	}

	l := New()
	ctx := context.Background()

	// Case 1: Batch path with spaces, arguments WITHOUT spaces -> PASSES
	t.Run("Path with spaces and arg without spaces", func(t *testing.T) {
		cmd, err := l.PrepareCommand(ctx, Config{
			PluginPath: batPath,
			Args:       []string{"simple_arg"},
		})
		if err != nil {
			t.Fatalf("PrepareCommand failed: %v", err)
		}
		out, runErr := cmd.CombinedOutput()
		if runErr != nil {
			t.Fatalf("Run failed: %v, out: %s", runErr, string(out))
		}
		if !strings.Contains(string(out), "HELLO_BATCH: simple_arg") {
			t.Errorf("unexpected output: %s", string(out))
		}
	})

	// Case 2: Batch path with spaces AND argument WITH spaces -> SUCCEEDS via 'call' prefix
	t.Run("Path with spaces AND arg with spaces succeeds via call prefix", func(t *testing.T) {
		cmd, err := l.PrepareCommand(ctx, Config{
			PluginPath: batPath,
			Args:       []string{"argument with space"},
		})
		if err != nil {
			t.Fatalf("PrepareCommand failed: %v", err)
		}
		out, runErr := cmd.CombinedOutput()
		if runErr != nil {
			t.Fatalf("Run failed: %v, out: %s", runErr, string(out))
		}
		if !strings.Contains(string(out), "argument with space") {
			t.Errorf("expected output to contain argument with space, got: %s", string(out))
		}
	})
}
