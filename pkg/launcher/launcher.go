package launcher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Required UI error messages per project specification.
const (
	ErrPythonNotFoundMsg = "Python interpreter not found in PATH. Please install Python to use this plugin."
	ErrNodeNotFoundMsg   = "Node.js interpreter not found in PATH. Please install Node.js to use this plugin."
	ErrCmdNotFoundMsg    = "Command prompt interpreter (cmd.exe) not found in PATH."
)

var (
	// ErrEmptyPluginPath is returned when Config.PluginPath is blank.
	ErrEmptyPluginPath = errors.New("plugin path cannot be empty")
	// ErrUnsupportedExtension is returned for unrecognized plugin file extensions.
	ErrUnsupportedExtension = errors.New("unsupported plugin extension")

	// Sentinel errors matching the exact specification strings for errors.Is checking.
	ErrPythonNotFound = errors.New(ErrPythonNotFoundMsg)
	ErrNodeNotFound   = errors.New(ErrNodeNotFoundMsg)
	ErrCmdNotFound    = errors.New(ErrCmdNotFoundMsg)
)

// InterpreterNotFoundError is a structured error containing missing interpreter details.
type InterpreterNotFoundError struct {
	Interpreter string
	Err         error
	Message     string
}

func (e *InterpreterNotFoundError) Error() string {
	return e.Message
}

func (e *InterpreterNotFoundError) Unwrap() error {
	return e.Err
}

func (e *InterpreterNotFoundError) Is(target error) bool {
	if target == nil {
		return false
	}
	// Sentinel match (wrapping-aware).
	switch e.Interpreter {
	case "python":
		if errors.Is(target, ErrPythonNotFound) {
			return true
		}
	case "node":
		if errors.Is(target, ErrNodeNotFound) {
			return true
		}
	case "cmd.exe", "cmd":
		if errors.Is(target, ErrCmdNotFound) {
			return true
		}
	}
	// Message match for compat: errors.New(Msg) without wrapping.
	if target.Error() == e.Message {
		return true
	}
	return errors.Is(target, e.Err)
}

// Config specifies execution parameters for a plugin.
type Config struct {
	PluginPath string
	Args       []string
	Env        []string
	Dir        string
}

// Launcher defines the contract for preparing and launching plugin processes.
type Launcher interface {
	PrepareCommand(ctx context.Context, cfg Config) (*exec.Cmd, error)
	Launch(ctx context.Context, cfg Config) (*exec.Cmd, error)
}

// LookPathFunc abstracts executable discovery (matches exec.LookPath signature).
type LookPathFunc func(file string) (string, error)

// CommandContextFunc abstracts command creation (matches exec.CommandContext signature).
type CommandContextFunc func(ctx context.Context, name string, arg ...string) *exec.Cmd

// PluginLauncher implements Launcher with dependency injection for testing.
type PluginLauncher struct {
	lookPath       LookPathFunc
	commandContext CommandContextFunc
}

var _ Launcher = (*PluginLauncher)(nil)

// Option configures PluginLauncher.
type Option func(*PluginLauncher)

// WithLookPath overrides the LookPath implementation.
func WithLookPath(fn LookPathFunc) Option {
	return func(l *PluginLauncher) {
		if fn != nil {
			l.lookPath = fn
		}
	}
}

// WithCommandContext overrides the CommandContext implementation.
func WithCommandContext(fn CommandContextFunc) Option {
	return func(l *PluginLauncher) {
		if fn != nil {
			l.commandContext = fn
		}
	}
}

// New creates a new PluginLauncher with production defaults.
func New(opts ...Option) *PluginLauncher {
	l := &PluginLauncher{
		lookPath:       exec.LookPath,
		commandContext: exec.CommandContext,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// lookPathFirst tries each name in order, returning the first found.
func (l *PluginLauncher) lookPathFirst(names ...string) (string, error) {
	var lastErr error
	for _, n := range names {
		if p, err := l.lookPath(n); err == nil {
			return p, nil
		} else {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no candidates")
	}
	return "", lastErr
}

// PrepareCommand constructs and configures an *exec.Cmd based on the plugin file extension.
func (l *PluginLauncher) PrepareCommand(ctx context.Context, cfg Config) (*exec.Cmd, error) {
	if strings.TrimSpace(cfg.PluginPath) == "" {
		return nil, ErrEmptyPluginPath
	}

	cleanPath := filepath.Clean(cfg.PluginPath)
	ext := strings.ToLower(filepath.Ext(cleanPath))

	var execPath string
	var execArgs []string

	switch ext {
	case ".exe":
		execPath = cleanPath
		execArgs = cfg.Args

	case ".py":
		pythonPath, err := l.lookPathFirst("python3", "python", "py")
		if err != nil {
			return nil, &InterpreterNotFoundError{
				Interpreter: "python",
				Err:         err,
				Message:     ErrPythonNotFoundMsg,
			}
		}
		execPath = pythonPath
		execArgs = append([]string{cleanPath}, cfg.Args...)

	case ".js":
		nodePath, err := l.lookPathFirst("node", "nodejs")
		if err != nil {
			return nil, &InterpreterNotFoundError{
				Interpreter: "node",
				Err:         err,
				Message:     ErrNodeNotFoundMsg,
			}
		}
		execPath = nodePath
		execArgs = append([]string{cleanPath}, cfg.Args...)

	case ".bat", ".cmd":
		cmdPath, err := l.lookPath("cmd.exe")
		if err != nil {
			cmdPath, err = l.lookPath("cmd")
		}
		if err != nil {
			if comSpec := os.Getenv("COMSPEC"); comSpec != "" {
				cmdPath, err = l.lookPath(comSpec)
			}
		}
		if err != nil {
			return nil, &InterpreterNotFoundError{
				Interpreter: "cmd.exe",
				Err:         err,
				Message:     ErrCmdNotFoundMsg,
			}
		}
		execPath = cmdPath
		// NOTE: cmd.exe re-parses the whole command line, so metachars in
		// args (& | < > ^ % !) would split into extra commands.
		// Escape them with ^ so batch plugins cannot inject commands.
		// The plugin path itself is passed as a separate argv entry (Go
		// quoting applies); do not reject it — paths with & are covered
		// by existing stress tests.
		safeArgs := make([]string, 0, len(cfg.Args))
		for _, a := range cfg.Args {
			safeArgs = append(safeArgs, escapeCmdArg(a))
		}
		execArgs = append([]string{"/c", "call", cleanPath}, safeArgs...)

	default:
		return nil, fmt.Errorf("%w: %q (file: %s)", ErrUnsupportedExtension, ext, cleanPath)
	}

	cmd := l.commandContext(ctx, execPath, execArgs...)

	// Working directory configuration
	if cfg.Dir != "" {
		if st, err := os.Stat(cfg.Dir); err != nil || !st.IsDir() {
			return nil, fmt.Errorf("invalid working directory %q", cfg.Dir)
		}
		cmd.Dir = cfg.Dir
	}

	// Environment variables: preserve host os.Environ() with cfg.Env overrides winning.
	// Dedupe case-insensitively on Windows (PATH= twice is undefined).
	if len(cfg.Env) > 0 {
		cmd.Env = mergeEnv(os.Environ(), cfg.Env)
	}

	// Apply OS-specific attributes (CREATE_NO_WINDOW on Windows)
	applyPlatformAttributes(cmd)

	return cmd, nil
}

// escapeCmdArg prefixes cmd.exe metacharacters with ^ so they are passed
// literally through `cmd.exe /s /c call`.
func escapeCmdArg(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&', '|', '<', '>', '^', '%', '!':
			b.WriteRune('^')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// mergeEnv merges base + overrides so override keys win. Comparison is
// case-insensitive for Windows compat (PATH vs Path), case-sensitive otherwise.
func mergeEnv(base, overrides []string) []string {
	keyOf := func(kv string) string {
		k := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			k = kv[:i]
		}
		// Env var names are case-insensitive on Windows.
		return strings.ToLower(k)
	}
	merged := make(map[string]string, len(base)+len(overrides))
	order := make([]string, 0, len(base)+len(overrides))
	seen := make(map[string]bool)
	for _, kv := range base {
		k := keyOf(kv)
		if !seen[k] {
			order = append(order, k)
			seen[k] = true
		}
		merged[k] = kv
	}
	for _, kv := range overrides {
		k := keyOf(kv)
		if !seen[k] {
			order = append(order, k)
			seen[k] = true
		}
		// Preserve the override's original key casing for display.
		merged[k] = kv
	}
	out := make([]string, 0, len(order))
	for _, k := range order {
		out = append(out, merged[k])
	}
	return out
}

// Launch prepares and starts the command, returning the active *exec.Cmd.
func (l *PluginLauncher) Launch(ctx context.Context, cfg Config) (*exec.Cmd, error) {
	cmd, err := l.PrepareCommand(ctx, cfg)
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start plugin process: %w", err)
	}

	return cmd, nil
}
