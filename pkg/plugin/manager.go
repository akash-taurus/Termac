package plugin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
}

// NewManager creates a new plugin manager
func NewManager(pluginsDir string) *Manager {
	return &Manager{
		pluginsDir: pluginsDir,
		launcher:   launcher.New(),
		instances:  make(map[string]*PluginInstance),
	}
}

// DiscoverPlugins scans pluginsDir and returns detected plugins
func (m *Manager) DiscoverPlugins() ([]*PluginInstance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := os.MkdirAll(m.pluginsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create plugins dir: %w", err)
	}

	entries, err := os.ReadDir(m.pluginsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read plugins dir: %w", err)
	}

	validExts := map[string]bool{
		".exe": true,
		".py":  true,
		".js":  true,
		".bat": true,
		".cmd": true,
		".ps1": true,
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := stringsToLower(filepath.Ext(entry.Name()))
		if !validExts[ext] {
			continue
		}

		id := entry.Name()
		if _, exists := m.instances[id]; !exists {
			fullPath := filepath.Join(m.pluginsDir, entry.Name())
			m.instances[id] = &PluginInstance{
				ID:        id,
				Name:      entry.Name(),
				Path:      fullPath,
				Extension: ext,
				Status:    StatusStopped,
			}
		}
	}

	list := make([]*PluginInstance, 0, len(m.instances))
	for _, inst := range m.instances {
		list = append(list, inst)
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
		m.mu.Unlock()
		return nil
	}

	inst.Status = StatusStarting
	inst.ErrorMsg = ""
	pipePath := transport.GeneratePipePath(sanitizePrefix(inst.ID))
	inst.PipePath = pipePath
	m.mu.Unlock()

	// Launch process with PLUGIN_PIPE environment variable
	cfg := launcher.Config{
		PluginPath: inst.Path,
		Env:        append(os.Environ(), fmt.Sprintf("PLUGIN_PIPE=%s", pipePath)),
	}

	cmd, err := m.launcher.Launch(ctx, cfg)
	if err != nil {
		m.mu.Lock()
		inst.Status = StatusError
		inst.ErrorMsg = err.Error()
		m.mu.Unlock()
		return fmt.Errorf("launch failed: %w", err)
	}

	m.mu.Lock()
	inst.Cmd = cmd
	if cmd.Process != nil {
		inst.PID = cmd.Process.Pid
	}
	m.mu.Unlock()

	// Connect to plugin Named Pipe with retry
	dialCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	var conn *grpc.ClientConn
	for {
		select {
		case <-dialCtx.Done():
			m.mu.Lock()
			inst.Status = StatusError
			inst.ErrorMsg = "timeout dialing plugin named pipe"
			if inst.Cmd != nil {
				_ = process.KillCmd(inst.Cmd)
				inst.Cmd = nil
			}
			m.mu.Unlock()
			return fmt.Errorf("timeout dialing pipe %s", pipePath)
		default:
			conn, err = transport.DialPipe(dialCtx, pipePath)
			if err == nil && conn != nil {
				goto Connected
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

Connected:
	m.mu.Lock()
	inst.Conn = conn
	inst.Client = pb.NewWidgetPluginClient(conn)
	inst.Status = StatusRunning
	inst.LastUpdated = time.Now()
	m.mu.Unlock()

	// Immediately request initial data
	_ = m.FetchAndRender(ctx, id, 80, 24)
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
	m.mu.RLock()
	inst, exists := m.instances[id]
	if !exists || inst.Client == nil || inst.Status != StatusRunning {
		m.mu.RUnlock()
		return fmt.Errorf("plugin %s not running", id)
	}
	client := inst.Client
	m.mu.RUnlock()

	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	fetchResp, err := client.FetchData(callCtx, &pb.FetchRequest{PluginId: id})
	if err != nil {
		m.mu.Lock()
		inst.Status = StatusError
		inst.ErrorMsg = fmt.Sprintf("fetch error: %v", err)
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
		inst.Status = StatusError
		inst.ErrorMsg = fmt.Sprintf("render error: %v", err)
		m.mu.Unlock()
		return err
	}

	m.mu.Lock()
	inst.LastData = fetchResp.GetData()
	inst.LastRender = renderResp.GetRenderedString()
	inst.LastUpdated = time.Now()
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
	}
}

// ListInstances returns current known plugins
func (m *Manager) ListInstances() []*PluginInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := make([]*PluginInstance, 0, len(m.instances))
	for _, inst := range m.instances {
		list = append(list, inst)
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
	return string(b)
}

func stringsToLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		} else {
			b[i] = c
		}
	}
	return string(b)
}
