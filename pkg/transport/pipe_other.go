//go:build !windows

package transport

import (
	"context"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// listenPipe creates a TCP listener for non-Windows platforms (testing fallback)
func listenPipe(pipePath string) (net.Listener, error) {
	// On non-Windows, use a random available TCP port on localhost
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	return lis, nil
}

// dialPipe creates a gRPC client connection to a TCP address on non-Windows
func dialPipe(ctx context.Context, pipePath string) (*grpc.ClientConn, error) {
	// For non-Windows, pipePath is actually a TCP address like "127.0.0.1:12345"
	conn, err := grpc.DialContext(ctx, pipePath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// generatePipePath creates a TCP address for non-Windows platforms
func generatePipePath(prefix string) string {
	return "127.0.0.1:0" // Let OS assign port, actual port returned by listener
}
