package transport

import (
	"context"
	"net"
	"time"

	"google.golang.org/grpc"
)

// Address represents a plugin communication endpoint
type Address struct {
	Path string // e.g., \\.\pipe\dashboard_plugin_<uuid>
}

// String returns the string representation
func (a *Address) String() string { return a.Path }

// Network returns the network type
func (a *Address) Network() string { return "npipe" }

// PipeServer is a gRPC server backed by a named pipe
type PipeServer struct {
	listener net.Listener
	pipePath string
}

// NewPipeServer creates a new named pipe server
func NewPipeServer(pipePath string) (*PipeServer, error) {
	lis, err := ListenPipe(pipePath)
	if err != nil {
		return nil, err
	}
	return &PipeServer{
		listener: lis,
		pipePath: pipePath,
	}, nil
}

// Serve starts serving gRPC requests on the named pipe
func (s *PipeServer) Serve(srv *grpc.Server) error {
	return srv.Serve(s.listener)
}

// Close closes the listener
func (s *PipeServer) Close() error {
	return s.listener.Close()
}

// Path returns the pipe path
func (s *PipeServer) Path() string {
	return s.pipePath
}

// ListenPipe creates a net.Listener on a Windows named pipe
func ListenPipe(pipePath string) (net.Listener, error) {
	return listenPipe(pipePath)
}

// DialPipe creates a gRPC client connection to a named pipe
func DialPipe(ctx context.Context, pipePath string) (*grpc.ClientConn, error) {
	return dialPipe(ctx, pipePath)
}

// GeneratePipePath creates a unique named pipe path with UUID
func GeneratePipePath(prefix string) string {
	return generatePipePath(prefix)
}

// DefaultDialTimeout is the default timeout for dialing a named pipe
const DefaultDialTimeout = 5 * time.Second