//go:build windows

package transport

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// listenPipe creates a named pipe listener on Windows using go-winio
func listenPipe(pipePath string) (net.Listener, error) {
	config := &winio.PipeConfig{
		MessageMode: false, // Byte stream mode for gRPC HTTP/2 framing
	}
	return winio.ListenPipe(pipePath, config)
}

// dialPipe creates a gRPC client connection to a Windows named pipe
func dialPipe(ctx context.Context, pipePath string) (*grpc.ClientConn, error) {
	dialer := func(ctx context.Context, addr string) (net.Conn, error) {
		return winio.DialPipeContext(ctx, addr)
	}

	conn, err := grpc.DialContext(ctx, pipePath,
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// generatePipePath creates a unique named pipe path with UUID on Windows
func generatePipePath(prefix string) string {
	return `\\.\pipe\` + prefix + `_` + generateUUID()
}

// generateUUID generates a UUID string using crypto/rand
func generateUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	// Set version (4) and variant bits
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}