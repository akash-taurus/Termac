package transport

import (
	"context"

	"google.golang.org/grpc"

	"tui/pkg/proto/plugin"
)

// NewPluginClient creates a gRPC client connected to a named pipe
func NewPluginClient(ctx context.Context, pipePath string) (plugin.WidgetPluginClient, *grpc.ClientConn, error) {
	conn, err := DialPipe(ctx, pipePath)
	if err != nil {
		return nil, nil, err
	}
	client := plugin.NewWidgetPluginClient(conn)
	return client, conn, nil
}

// ConnectToPlugin connects to a plugin and returns a client
func ConnectToPlugin(ctx context.Context, addr *Address) (plugin.WidgetPluginClient, *grpc.ClientConn, error) {
	return NewPluginClient(ctx, addr.Path)
}