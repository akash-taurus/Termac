package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPluginManager_Discovery(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "plugins_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create dummy test files
	_ = os.WriteFile(filepath.Join(tempDir, "test_plugin.py"), []byte("# test"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "sample_plugin.js"), []byte("// test"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "notes.txt"), []byte("ignore me"), 0644)

	mgr := NewManager(tempDir)
	plugins, err := mgr.DiscoverPlugins()
	if err != nil {
		t.Fatalf("DiscoverPlugins failed: %v", err)
	}

	if len(plugins) != 2 {
		t.Errorf("expected 2 plugins discovered, got %d", len(plugins))
	}

	instances := mgr.ListInstances()
	if len(instances) != 2 {
		t.Errorf("expected 2 instances, got %d", len(instances))
	}
}
