package transport

import (
	"context"
	"net"
	"strings"
	"testing"
)

func TestGeneratePipePath(t *testing.T) {
	path := GeneratePipePath("dashboard_plugin")
	
	// Should contain the prefix
	if !strings.Contains(path, "dashboard_plugin") {
		t.Errorf("expected path to contain prefix, got %q", path)
	}
	
	// On Windows, should be a named pipe path
	// On other platforms, should be a TCP address
	if strings.HasPrefix(path, `\\.\pipe\`) {
		// Windows: validate named pipe format
		if !strings.HasSuffix(path, `_`+generateUUID()) && len(path) <= len(`\\.\pipe\dashboard_plugin_`) {
			t.Errorf("expected Windows named pipe with UUID, got %q", path)
		}
	} else if strings.HasPrefix(path, "127.0.0.1:") {
		// Non-Windows: TCP fallback
		// Port 0 means OS assigns
	} else {
		t.Errorf("unexpected pipe path format: %q", path)
	}
}

func TestAddress_String(t *testing.T) {
	addr := &Address{Path: `\\.\pipe\test_pipe_123`}
	if addr.String() != `\\.\pipe\test_pipe_123` {
		t.Errorf("expected %q, got %q", `\\.\pipe\test_pipe_123`, addr.String())
	}
}

func TestAddress_Network(t *testing.T) {
	addr := &Address{Path: `\\.\pipe\test_pipe_123`}
	if addr.Network() != "npipe" {
		t.Errorf("expected 'npipe', got %q", addr.Network())
	}
}

func TestListenPipe_DialPipe_RoundTrip(t *testing.T) {
	pipePath := GeneratePipePath("test_roundtrip")

	lis, err := ListenPipe(pipePath)
	if err != nil {
		t.Fatalf("ListenPipe failed: %v", err)
	}
	defer lis.Close()

	// Verify listener is usable
	addr := lis.Addr()
	if addr == nil {
		t.Fatal("listener address is nil")
	}

	// Test DialPipe with cancelled context - should fail quickly, not hang
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = DialPipe(ctx, pipePath)
	if err == nil {
		t.Error("expected error due to cancelled context, got nil")
	}
}

func TestPipeServer_Lifecycle(t *testing.T) {
	pipePath := GeneratePipePath("test_lifecycle")
	
	ps, err := NewPipeServer(pipePath)
	if err != nil {
		t.Fatalf("NewPipeServer failed: %v", err)
	}

	// Verify path
	if ps.Path() != pipePath {
		t.Errorf("expected path %q, got %q", pipePath, ps.Path())
	}

	// Verify listener is set
	if ps.listener == nil {
		t.Fatal("expected listener to be set")
	}

	// Close should work
	if err := ps.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Second close should be idempotent or return error
	_ = ps.Close()
}

func TestNewPipeServer_ErrorHandling(t *testing.T) {
	// Test with invalid path (empty)
	_, err := NewPipeServer("")
	// On Windows, empty path should fail
	// On non-Windows, it creates a TCP listener on :0 which succeeds
	// We just verify it doesn't panic
	_ = err
}

func TestTransport_Interface(t *testing.T) {
	// Verify Address implements net.Addr interface
	var _ net.Addr = (*Address)(nil)
	
	addr := &Address{Path: `\\.\pipe\test`}
	if addr.Network() != "npipe" {
		t.Errorf("Address.Network() = %q, want npipe", addr.Network())
	}
	if addr.String() != `\\.\pipe\test` {
		t.Errorf("Address.String() = %q, want %q", addr.String(), `\\.\pipe\test`)
	}
}

func TestDialPipe_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	pipePath := GeneratePipePath("test_cancel")
	_, err := DialPipe(ctx, pipePath)
	if err == nil {
		t.Error("expected error due to context cancellation, got nil")
	}
}

func TestListenPipe_InvalidPath(t *testing.T) {
	// Test with empty path
	_, err := ListenPipe("")
	// Behavior depends on platform; just verify no panic
	_ = err
}