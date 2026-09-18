package plugin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"tui/pkg/launcher"
	"tui/pkg/process"
	pb "tui/pkg/proto/plugin"
	"tui/pkg/transport"
)

// PluginStatus indicates the runtime state of a plugin
type PluginStatus string

const (
	StatusStopped  PluginStatus = "Stopped"
	StatusStarting PluginStatus = "Starting"
	StatusRunning  PluginStatus = "Running"
	StatusError    PluginStatus = "Error"
)

// PluginInstance represents a registered plugin
type PluginInstance struct {
	ID          string
	Name        string
	Path        string
	Extension   string
	Status      PluginStatus
	ErrorMsg    string
	PipePath    string
	Cmd         *exec.Cmd
	PID         int
	Client      pb.WidgetPluginClient
	Conn        *grpc.ClientConn
	LastData    string
	LastRender  string
	LastUpdated time.Time
}

// Manager manages plugin lifecycle and communication
type Manager struct {
	mu         sync.RWMutex
	pluginsDir string
	launcher   launcher.Launcher
	instances  map[string]*PluginInstance
	// starting guards against concurrent double-StartPlugin for same id.
	starting map[string]bool
}

// NewManager creates a new plugin manager
func NewManager(pluginsDir string) *Manager {
	return &Manager{
		pluginsDir: pluginsDir,
		launcher:   launcher.New(),
		instances:  make(map[string]*PluginInstance),
		starting:   make(map[string]bool),
	}
}

// copyInstance returns a deep-ish copy (scalars + strings; Cmd/Conn/Client shared pointers but struct itself copied so callers cannot mutate status).
func copyInstance(src *PluginInstance) *PluginInstance {
	if src == nil {
		return nil
	}
	cp := *src
	return &cp
}

// DiscoverPlugins scans pluginsDir and returns detected plugins
func (m *Manager) DiscoverPlugins() ([]*PluginInstance, error) {
	if strings.TrimSpace(m.pluginsDir) == "" {
		return nil, fmt.Errorf("plugins directory cannot be empty")
	}
	cleanDir := filepath.Clean(m.pluginsDir)

	if err := os.MkdirAll(cleanDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create plugins dir: %w", err)
	}

	entries, err := os.ReadDir(cleanDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read plugins dir: %w", err)
	}

	// Launcher only implements these; .ps1 discovery would always fail at launch.
	validExts := map[string]bool{
		".exe": true,
		".py":  true,
		".js":  true,
		".bat": true,
		".cmd": true,
	}

	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if !validExts[ext] {
			continue
		}

		id := entry.Name()
		seen[id] = true
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// GC deleted files: stop + remove stale instances.
	for id, inst := range m.instances {
		if !seen[id] {
			if inst.Conn != nil {
				_ = inst.Conn.Close()
			}
			if inst.Cmd != nil {
				_ = process.KillCmd(inst.Cmd)
			}
			delete(m.instances, id)
		}
	}

	for id := range seen {
		if _, exists := m.instances[id]; !exists {
			ext := strings.ToLower(filepath.Ext(id))
			fullPath := filepath.Join(cleanDir, id)
			m.instances[id] = &PluginInstance{
				ID:        id,
				Name:      id,
				Path:      fullPath,
				Extension: ext,
				Status:    StatusStopped,
			}
		}
	}

	list := make([]*PluginInstance, 0, len(m.instances))
	for _, inst := range m.instances {
		list = append(list, copyInstance(inst))
	}
	return list, nil
}

// StartPlugin spawns the plugin and connects via Named Pipe
func (m *Manager) StartPlugin(ctx context.Context, id string) error {
	m.mu.Lock()
	inst, exists := m.instances[id]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("plugin %q not found", id)
	}

	if inst.Status == StatusRunning && inst.Cmd != nil {
		if inst.Cmd.ProcessState == nil || !inst.Cmd.ProcessState.Exited() {
			m.mu.Unlock()
			return nil
		}
		// Stale Running with exited process: fall through and restart.
	}
	if m.starting[id] {
		m.mu.Unlock()
		return fmt.Errorf("plugin %q is already starting", id)
	}
	// If previous run ended in Error but process still alive, kill orphan first.
	if inst.Cmd != nil && (inst.Cmd.ProcessState == nil || !inst.Cmd.ProcessState.Exited()) && inst.Status == StatusError {
		_ = process.KillCmd(inst.Cmd)
		inst.Cmd = nil
		inst.Conn = nil
		inst.Client = nil
	}

	m.starting[id] = true
	inst.Status = StatusStarting
	inst.ErrorMsg = ""
	pipePath := transport.GeneratePipePath(sanitizePrefix(inst.ID))
	inst.PipePath = pipePath
	// Capture path before unlocking; do not hold lock across Launch.
	pluginPath := inst.Path
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.starting, id)
		m.mu.Unlock()
	}()

	// Launch process with PLUGIN_PIPE environment variable (override wins).
	cfg := launcher.Config{
		PluginPath: pluginPath,
		Env:        []string{fmt.Sprintf("PLUGIN_PIPE=%s", pipePath)},
	}

	cmd, err := m.launcher.Launch(ctx, cfg)
	if err != nil {
		m.mu.Lock()
		// Only mark error if this start is still current (not stopped concurrently).
		if cur, ok := m.instances[id]; ok && cur.PipePath == pipePath {
			cur.Status = StatusError
			cur.ErrorMsg = err.Error()
		}
		m.mu.Unlock()
		return fmt.Errorf("launch failed: %w", err)
	}

	m.mu.Lock()
	// If StopPlugin ran while we were launching, kill the orphan and respect Stopped.
	cur, ok := m.instances[id]
	if !ok {
		m.mu.Unlock()
		_ = process.KillCmd(cmd)
		return fmt.Errorf("plugin %q removed during start", id)
	}
	if cur.Status == StatusStopped {
		m.mu.Unlock()
		_ = process.KillCmd(cmd)
		return fmt.Errorf("plugin %q stopped during start", id)
	}
	cur.Cmd = cmd
	if cmd.Process != nil {
		cur.PID = cmd.Process.Pid
	}
	m.mu.Unlock()

	// Connect with short per-try dials so the retry loop actually iterates.
	// transport.dialPipe WithBlock would otherwise block the full 4s on first try.
	deadline := time.Now().Add(4 * time.Second)
	var conn *grpc.ClientConn
	for {
		if time.Now().After(deadline) {
			break
		}
		if err := ctx.Err(); err != nil {
			m.mu.Lock()
			if cur, ok := m.instances[id]; ok && cur.PipePath == pipePath {
				cur.Status = StatusError
				cur.ErrorMsg = "start cancelled: " + err.Error()
				if cur.Cmd != nil {
					_ = process.KillCmd(cur.Cmd)
					cur.Cmd = nil
				}
			}
			m.mu.Unlock()
			return fmt.Errorf("start cancelled: %w", err)
		}
		tryCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		conn, err = transport.DialPipe(tryCtx, pipePath)
		cancel()
		if err == nil && conn != nil {
			goto Connected
		}
		time.Sleep(100 * time.Millisecond)
	}

	m.mu.Lock()
	if cur, ok := m.instances[id]; ok && cur.PipePath == pipePath {
		cur.Status = StatusError
		cur.ErrorMsg = "timeout dialing plugin named pipe"
		if cur.Cmd != nil {
			_ = process.KillCmd(cur.Cmd)
			cur.Cmd = nil
		}
	}
	m.mu.Unlock()
	return fmt.Errorf("timeout dialing pipe %s", pipePath)

Connected:
	m.mu.Lock()
	if cur, ok := m.instances[id]; ok && cur.PipePath == pipePath && cur.Status != StatusStopped {
		cur.Conn = conn
		cur.Client = pb.NewWidgetPluginClient(conn)
		cur.Status = StatusRunning
		cur.LastUpdated = time.Now()
	} else {
		m.mu.Unlock()
		_ = conn.Close()
		_ = process.KillCmd(cmd)
		return fmt.Errorf("plugin %q stopped during start", id)
	}
	m.mu.Unlock()

	// Immediately request initial data; surface error but keep Running only on success.
	if err := m.FetchAndRender(ctx, id, 80, 24); err != nil {
		return fmt.Errorf("plugin started but initial fetch failed: %w", err)
	}
	return nil
}

// StopPlugin terminates the plugin and its process tree
func (m *Manager) StopPlugin(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	inst, exists := m.instances[id]
	if !exists {
		return fmt.Errorf("plugin %q not found", id)
	}

	if inst.Conn != nil {
		_ = inst.Conn.Close()
		inst.Conn = nil
		inst.Client = nil
	}

	if inst.Cmd != nil {
		_ = process.KillCmd(inst.Cmd)
		inst.Cmd = nil
	}

	inst.Status = StatusStopped
	inst.PID = 0
	return nil
}

// FetchAndRender queries the plugin for state and rendering
func (m *Manager) FetchAndRender(ctx context.Context, id string, width, height int) error {
	if width < 1 {
		width = 80
	}
	if width > 1000 {
		width = 1000
	}
	if height < 1 {
		height = 24
	}
	if height > 1000 {
		height = 1000
	}
	m.mu.RLock()
	inst, exists := m.instances[id]
	if !exists || inst.Client == nil || inst.Status != StatusRunning {
		m.mu.RUnlock()
		return fmt.Errorf("plugin %s not running", id)
	}
	client := inst.Client
	conn := inst.Conn
	pipePath := inst.PipePath
	m.mu.RUnlock()

	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	fetchResp, err := client.FetchData(callCtx, &pb.FetchRequest{PluginId: id})
	if err != nil {
		m.mu.Lock()
		// Don't resurrect Error if plugin was stopped concurrently.
		if cur, ok := m.instances[id]; ok && cur.PipePath == pipePath && cur.Status == StatusRunning && cur.Conn == conn {
			cur.Status = StatusError
			cur.ErrorMsg = fmt.Sprintf("fetch error: %v", err)
		}
		m.mu.Unlock()
		return err
	}

	renderResp, err := client.Render(callCtx, &pb.RenderRequest{
		PluginId: id,
		Width:    int32(width),
		Height:   int32(height),
	})
	if err != nil {
		m.mu.Lock()
		if cur, ok := m.instances[id]; ok && cur.PipePath == pipePath && cur.Status == StatusRunning && cur.Conn == conn {
			cur.Status = StatusError
			cur.ErrorMsg = fmt.Sprintf("render error: %v", err)
		}
		m.mu.Unlock()
		return err
	}

	m.mu.Lock()
	if cur, ok := m.instances[id]; ok && cur.PipePath == pipePath && cur.Status == StatusRunning {
		cur.LastData = fetchResp.GetData()
		cur.LastRender = renderResp.GetRenderedString()
		cur.LastUpdated = time.Now()
	}
	m.mu.Unlock()

	return nil
}

// StopAll cleanly terminates all running plugins
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, inst := range m.instances {
		if inst.Conn != nil {
			_ = inst.Conn.Close()
			inst.Conn = nil
			inst.Client = nil
		}
		if inst.Cmd != nil {
			_ = process.KillCmd(inst.Cmd)
			inst.Cmd = nil
		}
		inst.Status = StatusStopped
		inst.PID = 0
	}
}

// ListInstances returns current known plugins
func (m *Manager) ListInstances() []*PluginInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := make([]*PluginInstance, 0, len(m.instances))
	for _, inst := range m.instances {
		list = append(list, copyInstance(inst))
	}
	return list
}

func sanitizePrefix(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			b = append(b, c)
		} else {
			b = append(b, '_')
		}
	}
	if len(b) > 20 {
		b = b[:20]
	}
	if len(b) == 0 {
		return "plugin"
	}
	return string(b)
}
