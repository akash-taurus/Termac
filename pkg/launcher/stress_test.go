package launcher_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"tui/pkg/launcher"
)

func findFixture(t *testing.T, filename string) string {
	t.Helper()
	p := filepath.Join("..", "..", "test", "e2e", "fixtures", filename)
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatalf("failed to resolve fixture %s: %v", filename, err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("fixture %s does not exist at %s: %v", filename, abs, err)
	}
	return abs
}

func copyFile(t *testing.T, srcPath, dstPath string) {
	t.Helper()
	src, err := os.Open(srcPath)
	if err != nil {
		t.Fatalf("failed to open src: %v", err)
	}
	defer src.Close()

	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		t.Fatalf("failed to mkdir: %v", err)
	}

	dst, err := os.Create(dstPath)
	if err != nil {
		t.Fatalf("failed to create dst: %v", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		t.Fatalf("failed to copy: %v", err)
	}
}

// 1. Real Execution of Python, Node, and Batch
func TestStress_RealExecution_AllInterpreters(t *testing.T) {
	l := launcher.New()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	testCases := []struct {
		name       string
		fixture    string
		expectOut  string
		skipIfNone string
	}{
		{"Python", "dummy.py", "DUMMY_PY_STDOUT: OK", "python"},
		{"Node", "dummy.js", "DUMMY_JS_STDOUT: OK", "node"},
		{"Batch", "dummy.bat", "DUMMY_BAT_STDOUT: OK", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skipIfNone != "" {
				if _, err := exec.LookPath(tc.skipIfNone); err != nil {
					t.Skipf("%s interpreter not found", tc.skipIfNone)
				}
			}
			if tc.name == "Batch" && runtime.GOOS != "windows" {
				t.Skip("Batch is Windows-only")
			}

			fixture := findFixture(t, tc.fixture)
			cfg := launcher.Config{
				PluginPath: fixture,
				Args:       []string{"--foo", "bar_val"},
				Env:        []string{"PLUGIN_TEST_ENV=CHALLENGER_VAL", "TEST_TRIGGER_STDERR=1"},
				Dir:        t.TempDir(),
			}

			cmd, err := l.PrepareCommand(ctx, cfg)
			if err != nil {
				t.Fatalf("PrepareCommand failed: %v", err)
			}

			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			if err := cmd.Run(); err != nil {
				t.Fatalf("cmd.Run() failed: %v, stderr: %s", err, stderr.String())
			}

			outStr := stdout.String()
			errStr := stderr.String()

			if !strings.Contains(outStr, tc.expectOut) {
				t.Errorf("expected stdout to contain %q, got:\n%s", tc.expectOut, outStr)
			}
			if !strings.Contains(outStr, "ENV:CHALLENGER_VAL") {
				t.Errorf("expected stdout to contain env value, got:\n%s", outStr)
			}
			if !strings.Contains(errStr, "NOTICE") {
				t.Errorf("expected stderr notice, got:\n%s", errStr)
			}
		})
	}
}

// 2. Non-zero Exit Codes
func TestStress_NonZeroExitCodes_Propagation(t *testing.T) {
	l := launcher.New()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	targets := []struct {
		name       string
		fixture    string
		skipIfNone string
		codes      []int
	}{
		{"Python", "dummy.py", "python", []int{1, 42, 127}},
		{"Node", "dummy.js", "node", []int{1, 42, 127}},
		{"Batch", "dummy.bat", "", []int{1, 42, 127}},
	}

	for _, tgt := range targets {
		t.Run(tgt.name, func(t *testing.T) {
			if tgt.skipIfNone != "" {
				if _, err := exec.LookPath(tgt.skipIfNone); err != nil {
					t.Skipf("%s not found", tgt.skipIfNone)
				}
			}
			if tgt.name == "Batch" && runtime.GOOS != "windows" {
				t.Skip("Windows-only")
			}

			fixture := findFixture(t, tgt.fixture)

			for _, code := range tgt.codes {
				t.Run(fmt.Sprintf("ExitCode_%d", code), func(t *testing.T) {
					cfg := launcher.Config{
						PluginPath: fixture,
						Args:       []string{"--exit-code", fmt.Sprintf("%d", code)},
					}

					cmd, err := l.PrepareCommand(ctx, cfg)
					if err != nil {
						t.Fatalf("PrepareCommand failed: %v", err)
					}

					var stderr bytes.Buffer
					cmd.Stderr = &stderr

					runErr := cmd.Run()
					if runErr == nil {
						t.Fatalf("expected error for exit code %d, got nil", code)
					}

					var exitErr *exec.ExitError
					if !errors.As(runErr, &exitErr) {
						t.Fatalf("expected *exec.ExitError, got %T: %v", runErr, runErr)
					}

					if exitErr.ExitCode() != code {
						t.Errorf("expected exit code %d, got %d", code, exitErr.ExitCode())
					}
				})
			}
		})
	}
}

// 3. Concurrent Plugin Launches (30 simultaneous processes)
func TestStress_ConcurrentPluginLaunches(t *testing.T) {
	l := launcher.New()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pyFixture := findFixture(t, "dummy.py")
	jsFixture := findFixture(t, "dummy.js")
	batFixture := findFixture(t, "dummy.bat")

	type task struct {
		name    string
		fixture string
		expect  string
	}

	tasks := []task{
		{"Python", pyFixture, "DUMMY_PY_STDOUT: OK"},
		{"Node", jsFixture, "DUMMY_JS_STDOUT: OK"},
		{"Batch", batFixture, "DUMMY_BAT_STDOUT: OK"},
	}

	const totalPerType = 10
	totalProcesses := len(tasks) * totalPerType

	var wg sync.WaitGroup
	errCh := make(chan error, totalProcesses)

	startGate := make(chan struct{})

	for _, tsk := range tasks {
		for i := 0; i < totalPerType; i++ {
			wg.Add(1)
			go func(tsk task, idx int) {
				defer wg.Done()
				<-startGate // Align start times for maximum concurrency stress

				cfg := launcher.Config{
					PluginPath: tsk.fixture,
					Args:       []string{"--task-id", fmt.Sprintf("%s-%d", tsk.name, idx)},
					Env:        []string{fmt.Sprintf("PLUGIN_TEST_ENV=%s_%d", tsk.name, idx)},
				}

				cmd, err := l.PrepareCommand(ctx, cfg)
				if err != nil {
					errCh <- fmt.Errorf("[%s-%d] PrepareCommand failed: %w", tsk.name, idx, err)
					return
				}

				var stdout, stderr bytes.Buffer
				cmd.Stdout = &stdout
				cmd.Stderr = &stderr

				if err := cmd.Run(); err != nil {
					errCh <- fmt.Errorf("[%s-%d] Run failed: %w (stderr: %s)", tsk.name, idx, err, stderr.String())
					return
				}

				if !strings.Contains(stdout.String(), tsk.expect) {
					errCh <- fmt.Errorf("[%s-%d] handshake missing in stdout: %s", tsk.name, idx, stdout.String())
					return
				}
			}(tsk, i)
		}
	}

	close(startGate) // Release all goroutines simultaneously
	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		t.Fatalf("%d / %d concurrent launches failed: %v", len(errs), totalProcesses, errs[0])
	}
}

// 4. Stress Test Paths with Spaces, Quotes, and Special Characters
func TestStress_PathsAndArgs_Stress(t *testing.T) {
	l := launcher.New()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pyFixture := findFixture(t, "dummy.py")
	jsFixture := findFixture(t, "dummy.js")
	batFixture := findFixture(t, "dummy.bat")

	tempBase := t.TempDir()

	testScenarios := []struct {
		desc        string
		subDir      string
		scriptBase  string
		ext         string
		srcFixture  string
		args        []string
		expectedOut string
		expectBug   bool
		bugReason   string
	}{
		{
			desc:        "Path with standard spaces",
			subDir:      "Directory With Multiple Spaces",
			scriptBase:  "my script with spaces",
			ext:         ".py",
			srcFixture:  pyFixture,
			args:        []string{"--arg1", "simple"},
			expectedOut: "DUMMY_PY_STDOUT: OK",
		},
		{
			desc:        "Path with spaces and args with spaces and quotes",
			subDir:      "Dir Space",
			scriptBase:  "plugin",
			ext:         ".py",
			srcFixture:  pyFixture,
			args:        []string{"--param", "value with spaces", "--quoted", "\"embedded quotes\""},
			expectedOut: "DUMMY_PY_STDOUT: OK",
		},
		{
			desc:        "Batch path with spaces and args with spaces",
			subDir:      "Batch Dir With Spaces",
			scriptBase:  "my batch",
			ext:         ".bat",
			srcFixture:  batFixture,
			args:        []string{"arg with space", "second arg"},
			expectedOut: "DUMMY_BAT_STDOUT: OK",
		},
		{
			desc:        "Batch path with parentheses",
			subDir:      "Program Files (x86) Clone",
			scriptBase:  "test (v1)",
			ext:         ".bat",
			srcFixture:  batFixture,
			args:        []string{"hello"},
			expectedOut: "DUMMY_BAT_STDOUT: OK",
		},
		{
			desc:        "Node path with parentheses and spaces",
			subDir:      "Node (Test) Folder",
			scriptBase:  "run (x86)",
			ext:         ".js",
			srcFixture:  jsFixture,
			args:        []string{"--status", "ok"},
			expectedOut: "DUMMY_JS_STDOUT: OK",
		},
		{
			desc:        "Path with plus and at symbols",
			subDir:      "dir+plus@at",
			scriptBase:  "script+test",
			ext:         ".bat",
			srcFixture:  batFixture,
			args:        []string{"val"},
			expectedOut: "DUMMY_BAT_STDOUT: OK",
		},
		{
			desc:        "Batch path with ampersand",
			subDir:      "dir & more",
			scriptBase:  "run & test",
			ext:         ".bat",
			srcFixture:  batFixture,
			args:        []string{"hello"},
			expectedOut: "DUMMY_BAT_STDOUT: OK",
		},
	}

	for _, sc := range testScenarios {
		t.Run(sc.desc, func(t *testing.T) {
			dstDir := filepath.Join(tempBase, sc.subDir)
			dstPath := filepath.Join(dstDir, sc.scriptBase+sc.ext)
			copyFile(t, sc.srcFixture, dstPath)

			cfg := launcher.Config{
				PluginPath: dstPath,
				Args:       sc.args,
				Dir:        dstDir,
			}

			cmd, err := l.PrepareCommand(ctx, cfg)
			if err != nil {
				t.Fatalf("PrepareCommand failed: %v", err)
			}

			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			runErr := cmd.Run()
			if sc.expectBug {
				if runErr != nil {
					t.Logf("[CONFIRMED BUG] %s failed as predicted: %v (Reason: %s)", sc.desc, runErr, sc.bugReason)
					t.Logf("Stderr output: %s", stderr.String())
					return // Bug verified empirically
				}
				t.Errorf("expected bug %q did not reproduce!", sc.bugReason)
			} else {
				if runErr != nil {
					t.Fatalf("Run failed for %s: %v\nStderr: %s\nStdout: %s", sc.desc, runErr, stderr.String(), stdout.String())
				}
				if !strings.Contains(stdout.String(), sc.expectedOut) {
					t.Errorf("expected stdout to contain %q, got:\n%s", sc.expectedOut, stdout.String())
				}
			}
		})
	}
}

// 5. Context Cancellation Immediate Halt & Zombie/Orphan Detection
func TestStress_ContextCancellation_HaltAndOrphans(t *testing.T) {
	l := launcher.New()

	pyFixture := findFixture(t, "dummy.py")
	jsFixture := findFixture(t, "dummy.js")

	testCases := []struct {
		name    string
		fixture string
		sleep   string
	}{
		{"Python", pyFixture, "10"},
		{"Node", jsFixture, "10"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()

			cfg := launcher.Config{
				PluginPath: tc.fixture,
				Args:       []string{"--sleep", tc.sleep},
			}

			start := time.Now()
			cmd, err := l.Launch(ctx, cfg)
			if err != nil {
				t.Fatalf("Launch failed: %v", err)
			}

			pid := cmd.Process.Pid
			t.Logf("[%s] Launched process PID: %d", tc.name, pid)

			waitErr := cmd.Wait()
			elapsed := time.Since(start)

			t.Logf("[%s] Elapsed time until halt: %v", tc.name, elapsed)

			if waitErr == nil {
				t.Errorf("[%s] Expected error on context cancellation, got nil", tc.name)
			}

			// Must halt in under 2 seconds, not sleep the full 10 seconds
			if elapsed > 2*time.Second {
				t.Errorf("[%s] Process did not halt immediately: took %v", tc.name, elapsed)
			}

			// Check if process itself is still running
			time.Sleep(100 * time.Millisecond) // brief settling time
			proc, err := os.FindProcess(pid)
			if err == nil && proc != nil {
				checkCmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid))
				out, _ := checkCmd.CombinedOutput()
				if strings.Contains(string(out), fmt.Sprintf("%d", pid)) {
					t.Errorf("[%s] Process PID %d is STILL RUNNING after context cancellation!", tc.name, pid)
				} else {
					t.Logf("[%s] Process PID %d successfully terminated", tc.name, pid)
				}
			}
		})
	}
}

// 6. Batch Sleep Fixture Verification
func TestStress_BatchSleepFixture(t *testing.T) {
	batFixture := findFixture(t, "dummy.bat")
	l := launcher.New()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	cmd, err := l.PrepareCommand(ctx, launcher.Config{
		PluginPath: batFixture,
		Args:       []string{"--sleep", "2"},
	})
	if err != nil {
		t.Fatalf("PrepareCommand failed: %v", err)
	}

	err = cmd.Run()
	elapsed := time.Since(start)

	t.Logf("dummy.bat --sleep 2 completed in %v with err: %v", elapsed, err)
	if err != nil {
		t.Fatalf("dummy.bat --sleep 2 failed with err: %v", err)
	}

	if elapsed < 1800*time.Millisecond {
		t.Errorf("dummy.bat --sleep 2 returned too quickly: %v (expected >= 1.8s)", elapsed)
	} else {
		t.Logf("dummy.bat slept successfully for %v", elapsed)
	}
}

// 7. Child Process Orphan Leak on Windows
// Demonstrates that Go's os/exec.CommandContext TerminateProcess does NOT kill child processes.
func TestStress_ChildProcessOrphanLeakDemonstration(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only")
	}

	tempDir := t.TempDir()
	childBat := filepath.Join(tempDir, "spawn_ping.bat")
	batContent := "@echo off\r\nping 127.0.0.1 -n 10 >nul\r\n"
	if err := os.WriteFile(childBat, []byte(batContent), 0755); err != nil {
		t.Fatalf("failed to write test batch: %v", err)
	}

	l := launcher.New()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	cmd, err := l.Launch(ctx, launcher.Config{PluginPath: childBat})
	if err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	parentPid := cmd.Process.Pid
	t.Logf("Launched parent cmd.exe PID: %d", parentPid)

	_ = cmd.Wait()
	t.Logf("Parent cmd.exe terminated by context cancellation")

	// Check if ping.exe is still alive!
	time.Sleep(300 * time.Millisecond)
	checkCmd := exec.Command("tasklist", "/FI", "IMAGENAME eq ping.exe")
	out, _ := checkCmd.CombinedOutput()
	outStr := string(out)
	t.Logf("tasklist output for ping.exe:\n%s", outStr)

	if strings.Contains(outStr, "ping.exe") {
		t.Logf("[ARCHITECTURAL FINDING] ping.exe survived parent termination! TerminateProcess leaves child processes orphaned.")
		t.Logf("This proves M2 (pkg/process.KillProcessTree with taskkill /F /T) is strictly mandatory.")
		// Clean up ping.exe to maintain clean state
		_ = exec.Command("taskkill", "/F", "/IM", "ping.exe").Run()
	} else {
		t.Logf("No leaked ping.exe detected")
	}
}
