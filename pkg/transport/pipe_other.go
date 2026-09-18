//go:build !windows

package transport

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// listenPipe creates a Unix-domain-socket listener for non-Windows platforms.
// pipePath must be a filesystem path (see generatePipePath). Stale socket
// files are removed before binding.
func listenPipe(pipePath string) (net.Listener, error) {
	if pipePath == "" {
		return nil, fmt.Errorf("empty pipe path")
	}
	// Tolerate tcp-style leftovers from older builds: fall back to socket file.
	if strings.Contains(pipePath, ":") {
		pipePath = filepath.Join(os.TempDir(), "dashboard-"+sanitize(pipePath)+".sock")
	}
	_ = os.Remove(pipePath)
	if err := os.MkdirAll(filepath.Dir(pipePath), 0700); err != nil {
		return nil, err
	}
	lis, err := net.Listen("unix", pipePath)
	if err != nil {
		return nil, err
	}
	return lis, nil
}

// dialPipe creates a gRPC client connection over a Unix socket on non-Windows.
func dialPipe(ctx context.Context, pipePath string) (*grpc.ClientConn, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultDialTimeout)
		defer cancel()
		return dialPipeWithTimeout(ctx, pipePath)
	}
	return dialPipeWithTimeout(ctx, pipePath)
}

func dialPipeWithTimeout(ctx context.Context, pipePath string) (*grpc.ClientConn, error) {
	dialer := func(dialCtx context.Context, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(dialCtx, "unix", pipePath)
	}
	conn, err := grpc.DialContext(ctx, "unix://"+pipePath,
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// generatePipePath creates a unique Unix socket path for non-Windows platforms.
func generatePipePath(prefix string) string {
	if prefix == "" {
		prefix = "dashboard"
	}
	prefix = sanitize(prefix)
	if len(prefix) > 32 {
		prefix = prefix[:32]
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fallback to PID-based uniqueness if CSPRNG fails.
		return filepath.Join(os.TempDir(), fmt.Sprintf("%s-%d.sock", prefix, os.Getpid()))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return filepath.Join(os.TempDir(), fmt.Sprintf("%s-%08x-%04x-%04x-%04x-%012x.sock",
		prefix, b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]))
}

func sanitize(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			b = append(b, c)
		} else {
			b = append(b, '_')
		}
	}
	if len(b) == 0 {
		return "dashboard"
	}
	return string(b)
}
