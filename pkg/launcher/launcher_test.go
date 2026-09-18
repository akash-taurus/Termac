package launcher

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Unit Tests using Dependency Injection

func TestPrepareCommand_Validation(t *testing.T) {
	l := New()
	ctx := context.Background()

	// Empty path
	_, err := l.PrepareCommand(ctx, Config{PluginPath: ""})
	if !errors.Is(err, ErrEmptyPluginPath) {
		t.Fatalf("expected ErrEmptyPluginPath, got %v", err)
	}

	// Whitespace path
	_, err = l.PrepareCommand(ctx, Config{PluginPath: "   \t\n"})
	if !errors.Is(err, ErrEmptyPluginPath) {
		t.Fatalf("expected ErrEmptyPluginPath for whitespace, got %v", err)
	}

	// Unsupported extension
	_, err = l.PrepareCommand(ctx, Config{PluginPath: "plugin.unsupported"})
	if !errors.Is(err, ErrUnsupportedExtension) {
		t.Fatalf("expected ErrUnsupportedExtension, got %v", err)
	}

	// Another unsupported extension
	_, err = l.PrepareCommand(ctx, Config{PluginPath: "script.sh"})
	if !errors.Is(err, ErrUnsupportedExtension) {
		t.Fatalf("expected ErrUnsupportedExtension for .sh, got %v", err)
	}
}

func TestPrepareCommand_Exe(t *testing.T) {
	l := New()
	ctx := context.Background()

	cfg := Config{
		PluginPath: filepath.Join("C:", "plugins", "my_plugin.exe"),
		Args:       []string{"--port", "8080", "--verbose"},
	}

	cmd, err := l.PrepareCommand(ctx, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cleanExpected := filepath.Clean(cfg.PluginPath)
	if cmd.Path != cleanExpected {
		t.Errorf("expected cmd.Path %q, got %q", cleanExpected, cmd.Path)
	}

	expectedArgs := append([]string{cleanExpected}, cfg.Args...)
	if len(cmd.Args) != len(expectedArgs) {
		t.Fatalf("expected %d args, got %d", len(expectedArgs), len(cmd.Args))
	}
	for i, arg := range expectedArgs {
		if cmd.Args[i] != arg {
			t.Errorf("arg[%d] expected %q, got %q", i, arg, cmd.Args[i])
		}
	}
}

func TestPrepareCommand_Python_Success(t *testing.T) {
	mockPython := filepath.Join("C:", "Python314", "python.exe")
	l := New(WithLookPath(func(file string) (string, error) {
		if file == "python" {
			return mockPython, nil
		}
		return "", exec.ErrNotFound
	}))

	ctx := context.Background()
	cfg := Config{
		PluginPath: filepath.Join("plugins", "test.py"),
		Args:       []string{"--data", "sample"},
	}

	cmd, err := l.PrepareCommand(ctx, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cmd.Path != mockPython {
		t.Errorf("expected cmd.Path %q, got %q", mockPython, cmd.Path)
	}

	expectedArgs := []string{mockPython, filepath.Clean(cfg.PluginPath), "--data", "sample"}
	if len(cmd.Args) != len(expectedArgs) {
		t.Fatalf("expected %d args, got %d (%v)", len(expectedArgs), len(cmd.Args), cmd.Args)
	}
	for i, arg := range expectedArgs {
		if cmd.Args[i] != arg {
			t.Errorf("arg[%d] expected %q, got %q", i, arg, cmd.Args[i])
		}
	}
}

func TestPrepareCommand_Python_Missing(t *testing.T) {
	l := New(WithLookPath(func(file string) (string, error) {
		return "", exec.ErrNotFound
	}))

	ctx := context.Background()
	cfg := Config{
		PluginPath: "plugin.py",
	}

	_, err := l.PrepareCommand(ctx, cfg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify exact required error message string
	if err.Error() != ErrPythonNotFoundMsg {
		t.Errorf("expected exact error %q, got %q", ErrPythonNotFoundMsg, err.Error())
	}

	// Verify errors.Is matches sentinel
	if !errors.Is(err, ErrPythonNotFound) {
		t.Error("expected errors.Is(err, ErrPythonNotFound) to be true")
	}

	// Verify structured InterpreterNotFoundError
	var interpErr *InterpreterNotFoundError
	if !errors.As(err, &interpErr) {
		t.Fatal("expected error to be *InterpreterNotFoundError")
	}
	if interpErr.Interpreter != "python" {
		t.Errorf("expected Interpreter 'python', got %q", interpErr.Interpreter)
	}
	if !errors.Is(interpErr.Unwrap(), exec.ErrNotFound) {
		t.Errorf("expected unwrapped error exec.ErrNotFound, got %v", interpErr.Unwrap())
	}
}

func TestPrepareCommand_Node_Success(t *testing.T) {
	mockNode := filepath.Join("C:", "nodejs", "node.exe")
	l := New(WithLookPath(func(file string) (string, error) {
		if file == "node" {
			return mockNode, nil
		}
		return "", exec.ErrNotFound
	}))

	ctx := context.Background()
	cfg := Config{
		PluginPath: filepath.Join("plugins", "widget.js"),
		Args:       []string{"--env", "production"},
	}

	cmd, err := l.PrepareCommand(ctx, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cmd.Path != mockNode {
		t.Errorf("expected cmd.Path %q, got %q", mockNode, cmd.Path)
	}

	expectedArgs := []string{mockNode, filepath.Clean(cfg.PluginPath), "--env", "production"}
	if len(cmd.Args) != len(expectedArgs) {
		t.Fatalf("expected %d args, got %d (%v)", len(expectedArgs), len(cmd.Args), cmd.Args)
	}
	for i, arg := range expectedArgs {
		if cmd.Args[i] != arg {
			t.Errorf("arg[%d] expected %q, got %q", i, arg, cmd.Args[i])
		}
	}
}

func TestPrepareCommand_Node_Missing(t *testing.T) {
	l := New(WithLookPath(func(file string) (string, error) {
		return "", exec.ErrNotFound
	}))

	ctx := context.Background()
	cfg := Config{
		PluginPath: "plugin.js",
	}

	_, err := l.PrepareCommand(ctx, cfg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify exact required error message string
	if err.Error() != ErrNodeNotFoundMsg {
		t.Errorf("expected exact error %q, got %q", ErrNodeNotFoundMsg, err.Error())
	}

	// Verify errors.Is matches sentinel
	if !errors.Is(err, ErrNodeNotFound) {
		t.Error("expected errors.Is(err, ErrNodeNotFound) to be true")
	}

	// Verify structured InterpreterNotFoundError
	var interpErr *InterpreterNotFoundError
	if !errors.As(err, &interpErr) {
		t.Fatal("expected error to be *InterpreterNotFoundError")
	}
	if interpErr.Interpreter != "node" {
		t.Errorf("expected Interpreter 'node', got %q", interpErr.Interpreter)
	}
	if !errors.Is(interpErr.Unwrap(), exec.ErrNotFound) {
		t.Errorf("expected unwrapped error exec.ErrNotFound, got %v", interpErr.Unwrap())
	}
}

func TestPrepareCommand_Batch_Success(t *testing.T) {
	mockCmd := filepath.Join("C:", "Windows", "System32", "cmd.exe")
	l := New(WithLookPath(func(file string) (string, error) {
		if file == "cmd.exe" || file == "cmd" {
			return mockCmd, nil
		}
		return "", exec.ErrNotFound
	}))

	ctx := context.Background()
	cfg := Config{
		PluginPath: filepath.Join("scripts", "setup.bat"),
		Args:       []string{"arg1", "arg2"},
	}

	cmd, err := l.PrepareCommand(ctx, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cmd.Path != mockCmd {
		t.Errorf("expected cmd.Path %q, got %q", mockCmd, cmd.Path)
	}

	expectedArgs := []string{mockCmd, "/c", "call", filepath.Clean(cfg.PluginPath), "arg1", "arg2"}
	if len(cmd.Args) != len(expectedArgs) {
		t.Fatalf("expected %d args, got %d (%v)", len(expectedArgs), len(cmd.Args), cmd.Args)
	}
	for i, arg := range expectedArgs {
		if cmd.Args[i] != arg {
			t.Errorf("arg[%d] expected %q, got %q", i, arg, cmd.Args[i])
		}
	}
}

func TestPrepareCommand_Batch_Missing(t *testing.T) {
	l := New(WithLookPath(func(file string) (string, error) {
		return "", exec.ErrNotFound
	}))

	ctx := context.Background()
	cfg := Config{
		PluginPath: "script.bat",
	}

	_, err := l.PrepareCommand(ctx, cfg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if err.Error() != ErrCmdNotFoundMsg {
		t.Errorf("expected error %q, got %q", ErrCmdNotFoundMsg, err.Error())
	}
	if !errors.Is(err, ErrCmdNotFound) {
		t.Error("expected errors.Is(err, ErrCmdNotFound) to be true")
	}

	var interpErr *InterpreterNotFoundError
	if !errors.As(err, &interpErr) {
		t.Fatal("expected error to be *InterpreterNotFoundError")
	}
	if interpErr.Interpreter != "cmd.exe" {
		t.Errorf("expected Interpreter 'cmd.exe', got %q", interpErr.Interpreter)
	}
}

func TestPrepareCommand_CaseInsensitivity(t *testing.T) {
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

	testCases := []struct {
		filename     string
		expectedPath string
	}{
		{"PLUGIN.EXE", "PLUGIN.EXE"},
		{"plugin.Py", mockPython},
		{"PLUGIN.PY", mockPython},
		{"script.Js", mockNode},
		{"SCRIPT.JS", mockNode},
		{"runner.BAT", mockCmd},
		{"runner.Cmd", mockCmd},
		{"RUNNER.CMD", mockCmd},
	}

	for _, tc := range testCases {
		t.Run(tc.filename, func(t *testing.T) {
			cmd, err := l.PrepareCommand(ctx, Config{PluginPath: tc.filename})
			if err != nil {
				t.Fatalf("unexpected error for %s: %v", tc.filename, err)
			}
			if cmd.Path != tc.expectedPath {
				t.Errorf("for %s expected Path %q, got %q", tc.filename, tc.expectedPath, cmd.Path)
			}
		})
	}
}

func TestPrepareCommand_EnvAndDir(t *testing.T) {
	l := New()
	ctx := context.Background()

	targetDir := t.TempDir()
	customEnv := []string{"FOO=bar", "BAZ=qux"}

	cfg := Config{
		PluginPath: "dummy.exe",
		Dir:        targetDir,
		Env:        customEnv,
	}

	cmd, err := l.PrepareCommand(ctx, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cmd.Dir != targetDir {
		t.Errorf("expected cmd.Dir %q, got %q", targetDir, cmd.Dir)
	}

	if len(cmd.Env) == 0 {
		t.Fatal("expected cmd.Env to be non-empty")
	}

	// Verify custom env exists
	foundFoo := false
	foundBaz := false
	for _, env := range cmd.Env {
		if env == "FOO=bar" {
			foundFoo = true
		}
		if env == "BAZ=qux" {
			foundBaz = true
		}
	}
	if !foundFoo || !foundBaz {
		t.Errorf("custom env missing in cmd.Env: %v", cmd.Env)
	}

	// On Windows, verify system environment variables (e.g. SystemRoot or PATH) were preserved
	if runtime.GOOS == "windows" {
		foundSystem := false
		for _, env := range cmd.Env {
			upper := strings.ToUpper(env)
			if strings.HasPrefix(upper, "SYSTEMROOT=") || strings.HasPrefix(upper, "PATH=") {
				foundSystem = true
				break
			}
		}
		if !foundSystem {
			t.Error("expected system environment variables to be preserved in cmd.Env on Windows")
		}
	}
}

func TestOptions_NilSafety(t *testing.T) {
	// Should not panic or overwrite with nil
	l := New(WithLookPath(nil), WithCommandContext(nil))
	if l.lookPath == nil || l.commandContext == nil {
		t.Fatal("lookPath or commandContext became nil")
	}
}

func TestLaunch_PrepareError(t *testing.T) {
	l := New()
	ctx := context.Background()

	_, err := l.Launch(ctx, Config{PluginPath: ""})
	if !errors.Is(err, ErrEmptyPluginPath) {
		t.Fatalf("expected ErrEmptyPluginPath, got %v", err)
	}
}

func TestLaunch_StartError(t *testing.T) {
	// Inject a command with an invalid executable path
	l := New(WithCommandContext(func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "non_existent_binary_12345_never_exists.exe")
	}))

	ctx := context.Background()
	_, err := l.Launch(ctx, Config{PluginPath: "plugin.exe"})
	if err == nil {
		t.Fatal("expected error on launching non-existent binary, got nil")
	}
	if !strings.Contains(err.Error(), "failed to start plugin process") {
		t.Errorf("expected 'failed to start plugin process' in error, got %q", err.Error())
	}
}

func TestInterpreterNotFoundError_Is(t *testing.T) {
	pyErr := &InterpreterNotFoundError{
		Interpreter: "python",
		Err:         exec.ErrNotFound,
		Message:     ErrPythonNotFoundMsg,
	}
	if !pyErr.Is(ErrPythonNotFound) {
		t.Error("expected pyErr to match ErrPythonNotFound")
	}
	if !pyErr.Is(errors.New(ErrPythonNotFoundMsg)) {
		t.Error("expected pyErr to match error with same message")
	}
	if pyErr.Is(ErrNodeNotFound) {
		t.Error("expected pyErr not to match ErrNodeNotFound")
	}
	if pyErr.Is(nil) {
		t.Error("expected pyErr not to match nil")
	}

	nodeErr := &InterpreterNotFoundError{
		Interpreter: "node",
		Err:         exec.ErrNotFound,
		Message:     ErrNodeNotFoundMsg,
	}
	if !nodeErr.Is(ErrNodeNotFound) {
		t.Error("expected nodeErr to match ErrNodeNotFound")
	}
	if !nodeErr.Is(errors.New(ErrNodeNotFoundMsg)) {
		t.Error("expected nodeErr to match error with same message")
	}
	if nodeErr.Is(ErrPythonNotFound) {
		t.Error("expected nodeErr not to match ErrPythonNotFound")
	}

	cmdErr := &InterpreterNotFoundError{
		Interpreter: "cmd.exe",
		Err:         exec.ErrNotFound,
		Message:     ErrCmdNotFoundMsg,
	}
	if !cmdErr.Is(ErrCmdNotFound) {
		t.Error("expected cmdErr to match ErrCmdNotFound")
	}
	if !cmdErr.Is(errors.New(ErrCmdNotFoundMsg)) {
		t.Error("expected cmdErr to match error with same message")
	}
	if cmdErr.Is(ErrPythonNotFound) {
		t.Error("expected cmdErr not to match ErrPythonNotFound")
	}

	customErr := &InterpreterNotFoundError{
		Interpreter: "ruby",
		Err:         exec.ErrNotFound,
		Message:     "Ruby not found",
	}
	if !customErr.Is(errors.New("Ruby not found")) {
		t.Error("expected customErr to match same message")
	}
	if customErr.Is(errors.New("Different message")) {
		t.Error("expected customErr not to match different message")
	}
}

// Integration Tests executing real dummy scripts on Windows

func getFixturePath(t *testing.T, filename string) string {
	t.Helper()
	// Try relative from pkg/launcher directory: ../../test/e2e/fixtures/<filename>
	p1 := filepath.Join("..", "..", "test", "e2e", "fixtures", filename)
	if _, err := os.Stat(p1); err == nil {
		abs, err := filepath.Abs(p1)
		if err == nil {
			return abs
		}
		return p1
	}

	// Try workspace root relative: test/e2e/fixtures/<filename>
	p2 := filepath.Join("test", "e2e", "fixtures", filename)
	if _, err := os.Stat(p2); err == nil {
		abs, err := filepath.Abs(p2)
		if err == nil {
			return abs
		}
		return p2
	}

	t.Fatalf("fixture file %q not found", filename)
	return ""
}

func TestIntegration_RealDummyPython(t *testing.T) {
	if _, err := exec.LookPath("python"); err != nil {
		t.Skip("Python interpreter not found on host, skipping integration test")
	}

	fixturePath := getFixturePath(t, "dummy.py")
	workDir := t.TempDir()

	l := New()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg := Config{
		PluginPath: fixturePath,
		Args:       []string{"--test-arg", "py_val"},
		Env:        []string{"PLUGIN_TEST_ENV=PY_INTEGRATION_OK", "TEST_TRIGGER_STDERR=1"},
		Dir:        workDir,
	}

	cmd, err := l.PrepareCommand(ctx, cfg)
	if err != nil {
		t.Fatalf("PrepareCommand failed: %v", err)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if err := cmd.Run(); err != nil {
		t.Fatalf("command failed: %v, stderr: %s", err, stderrBuf.String())
	}

	stdout := stdoutBuf.String()
	stderr := stderrBuf.String()

	if !strings.Contains(stdout, "DUMMY_PY_STDOUT: OK") {
		t.Errorf("stdout missing handshake: %s", stdout)
	}
	if !strings.Contains(stdout, "ARGS:--test-arg,py_val") {
		t.Errorf("stdout missing expected args: %s", stdout)
	}
	if !strings.Contains(stdout, "ENV:PY_INTEGRATION_OK") {
		t.Errorf("stdout missing expected env: %s", stdout)
	}
	if !strings.Contains(stderr, "DUMMY_PY_STDERR: NOTICE") {
		t.Errorf("stderr missing expected notice: %s", stderr)
	}

	// Also test direct Launch method
	launchCmd, err := l.Launch(ctx, cfg)
	if err != nil {
		t.Fatalf("Launch failed: %v", err)
	}
	if launchCmd.Process == nil || launchCmd.Process.Pid <= 0 {
		t.Fatalf("Launch process not started: %v", launchCmd.Process)
	}
	if err := launchCmd.Wait(); err != nil {
		t.Fatalf("Launch wait failed: %v", err)
	}
}

func TestIntegration_RealDummyNode(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js interpreter not found on host, skipping integration test")
	}

	fixturePath := getFixturePath(t, "dummy.js")
	workDir := t.TempDir()

	l := New()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg := Config{
		PluginPath: fixturePath,
		Args:       []string{"--test-arg", "node_val"},
		Env:        []string{"PLUGIN_TEST_ENV=NODE_INTEGRATION_OK", "TEST_TRIGGER_STDERR=1"},
		Dir:        workDir,
	}

	cmd, err := l.PrepareCommand(ctx, cfg)
	if err != nil {
		t.Fatalf("PrepareCommand failed: %v", err)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if err := cmd.Run(); err != nil {
		t.Fatalf("command failed: %v, stderr: %s", err, stderrBuf.String())
	}

	stdout := stdoutBuf.String()
	stderr := stderrBuf.String()

	if !strings.Contains(stdout, "DUMMY_JS_STDOUT: OK") {
		t.Errorf("stdout missing handshake: %s", stdout)
	}
	if !strings.Contains(stdout, "ARGS:--test-arg,node_val") {
		t.Errorf("stdout missing expected args: %s", stdout)
	}
	if !strings.Contains(stdout, "ENV:NODE_INTEGRATION_OK") {
		t.Errorf("stdout missing expected env: %s", stdout)
	}
	if !strings.Contains(stderr, "DUMMY_JS_STDERR: NOTICE") {
		t.Errorf("stderr missing expected notice: %s", stderr)
	}

	// Also test direct Launch method
	launchCmd, err := l.Launch(ctx, cfg)
	if err != nil {
		t.Fatalf("Launch failed: %v", err)
	}
	if launchCmd.Process == nil || launchCmd.Process.Pid <= 0 {
		t.Fatalf("Launch process not started: %v", launchCmd.Process)
	}
	if err := launchCmd.Wait(); err != nil {
		t.Fatalf("Launch wait failed: %v", err)
	}
}

func TestIntegration_RealDummyBatch(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Batch execution is Windows-only")
	}

	fixturePath := getFixturePath(t, "dummy.bat")
	workDir := t.TempDir()

	l := New()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg := Config{
		PluginPath: fixturePath,
		Args:       []string{"arg1", "arg2"},
		Env:        []string{"PLUGIN_TEST_ENV=BAT_INTEGRATION_OK", "TEST_TRIGGER_STDERR=1"},
		Dir:        workDir,
	}

	cmd, err := l.PrepareCommand(ctx, cfg)
	if err != nil {
		t.Fatalf("PrepareCommand failed: %v", err)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if err := cmd.Run(); err != nil {
		t.Fatalf("command failed: %v, stderr: %s", err, stderrBuf.String())
	}

	stdout := stdoutBuf.String()
	stderr := stderrBuf.String()

	if !strings.Contains(stdout, "DUMMY_BAT_STDOUT: OK") {
		t.Errorf("stdout missing handshake: %s", stdout)
	}
	if !strings.Contains(stdout, "ENV:BAT_INTEGRATION_OK") {
		t.Errorf("stdout missing expected env: %s", stdout)
	}
	if !strings.Contains(stderr, "DUMMY_BAT_STDERR: NOTICE") {
		t.Errorf("stderr missing expected notice: %s", stderr)
	}

	// Also test direct Launch method
	launchCmd, err := l.Launch(ctx, cfg)
	if err != nil {
		t.Fatalf("Launch failed: %v", err)
	}
	if launchCmd.Process == nil || launchCmd.Process.Pid <= 0 {
		t.Fatalf("Launch process not started: %v", launchCmd.Process)
	}
	if err := launchCmd.Wait(); err != nil {
		t.Fatalf("Launch wait failed: %v", err)
	}
}

func TestIntegration_NonZeroExitCode(t *testing.T) {
	if _, err := exec.LookPath("python"); err != nil {
		t.Skip("Python interpreter not found on host, skipping integration test")
	}

	fixturePath := getFixturePath(t, "dummy.py")
	l := New()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg := Config{
		PluginPath: fixturePath,
		Args:       []string{"--exit-code", "42"},
	}

	cmd, err := l.PrepareCommand(ctx, cfg)
	if err != nil {
		t.Fatalf("PrepareCommand failed: %v", err)
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	err = cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit error, got nil")
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
	}

	if exitErr.ExitCode() != 42 {
		t.Errorf("expected exit code 42, got %d", exitErr.ExitCode())
	}

	if !strings.Contains(stderrBuf.String(), "EXITING_WITH_CODE_42") {
		t.Errorf("stderr missing exit message: %s", stderrBuf.String())
	}
}

func TestIntegration_ContextCancellation(t *testing.T) {
	if _, err := exec.LookPath("python"); err != nil {
		t.Skip("Python interpreter not found on host, skipping integration test")
	}

	fixturePath := getFixturePath(t, "dummy.py")
	l := New()

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	cfg := Config{
		PluginPath: fixturePath,
		Args:       []string{"--sleep", "5"},
	}

	start := time.Now()
	cmd, err := l.PrepareCommand(ctx, cfg)
	if err != nil {
		t.Fatalf("PrepareCommand failed: %v", err)
	}

	err = cmd.Run()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error due to context cancellation, got nil")
	}

	if elapsed > 3*time.Second {
		t.Errorf("command took %v, should have timed out within ~250ms", elapsed)
	}
}

func TestIntegration_PathsWithSpaces(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only batch test")
	}

	tempDir := t.TempDir()
	folderWithSpace := filepath.Join(tempDir, "plugin folder with space")
	if err := os.MkdirAll(folderWithSpace, 0755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	srcBatch := getFixturePath(t, "dummy.bat")
	dstBatch := filepath.Join(folderWithSpace, "my script.bat")

	srcFile, err := os.Open(srcBatch)
	if err != nil {
		t.Fatalf("failed to open source batch: %v", err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dstBatch)
	if err != nil {
		t.Fatalf("failed to create destination batch: %v", err)
	}
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		dstFile.Close()
		t.Fatalf("failed to copy batch: %v", err)
	}
	dstFile.Close()

	l := New()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg := Config{
		PluginPath: dstBatch,
		Args:       []string{"hello", "world"},
	}

	cmd, err := l.PrepareCommand(ctx, cfg)
	if err != nil {
		t.Fatalf("PrepareCommand failed: %v", err)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if err := cmd.Run(); err != nil {
		t.Fatalf("batch execution with spaces failed: %v, stderr: %s", err, stderrBuf.String())
	}

	if !strings.Contains(stdoutBuf.String(), "DUMMY_BAT_STDOUT: OK") {
		t.Errorf("stdout missing handshake: %s", stdoutBuf.String())
	}
}

// TestIntegration_Batch_SpacesInPathAndArgs explicitly verifies that batch scripts with spaces
// in both the directory path and script name, executed with arguments containing spaces,
// do not trigger cmd.exe quote-stripping errors thanks to the 'call' prefix.
func TestIntegration_Batch_SpacesInPathAndArgs(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only batch test")
	}

	tempDir := t.TempDir()
	folderWithSpace := filepath.Join(tempDir, "Program Files (x86) Test Folder")
	if err := os.MkdirAll(folderWithSpace, 0755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	srcBatch := getFixturePath(t, "dummy.bat")
	dstBatch := filepath.Join(folderWithSpace, "my test script.bat")

	srcFile, err := os.Open(srcBatch)
	if err != nil {
		t.Fatalf("failed to open source batch: %v", err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dstBatch)
	if err != nil {
		t.Fatalf("failed to create destination batch: %v", err)
	}
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		dstFile.Close()
		t.Fatalf("failed to copy batch: %v", err)
	}
	dstFile.Close()

	l := New()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg := Config{
		PluginPath: dstBatch,
		Args:       []string{"first arg with space", "second arg with spaces", "--flag"},
	}

	cmd, err := l.PrepareCommand(ctx, cfg)
	if err != nil {
		t.Fatalf("PrepareCommand failed: %v", err)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if err := cmd.Run(); err != nil {
		t.Fatalf("batch execution failed (quote stripping regression): %v\nStderr: %s\nStdout: %s",
			err, stderrBuf.String(), stdoutBuf.String())
	}

	outStr := stdoutBuf.String()
	if !strings.Contains(outStr, "DUMMY_BAT_STDOUT: OK") {
		t.Errorf("stdout missing expected handshake: %s", outStr)
	}
	if !strings.Contains(outStr, "first arg with space") || !strings.Contains(outStr, "second arg with spaces") {
		t.Errorf("stdout missing passed arguments: %s", outStr)
	}
}
