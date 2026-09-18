package transport

import (
	"google.golang.org/grpc"

	"tui/pkg/proto/plugin"
)

// StartPluginServer creates and starts a gRPC server on a named pipe
func StartPluginServer(pipePath string, srv plugin.WidgetPluginServer) (*grpc.Server, *PipeServer, error) {
	ps, err := NewPipeServer(pipePath)
	if err != nil {
		return nil, nil, err
	}

	grpcServer := NewPluginServer()
	plugin.RegisterWidgetPluginServer(grpcServer, srv)

	// Serve in background
	go func() {
		_ = ps.Serve(grpcServer)
	}()

	return grpcServer, ps, nil
}

// NewPluginServer creates a gRPC server with default options
func NewPluginServer() *grpc.Server {
	return grpc.NewServer()
}
