package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"tui/pkg/proto/plugin"
	"tui/pkg/transport"

	"google.golang.org/grpc"
)

type dummyPlugin struct {
	plugin.UnimplementedWidgetPluginServer
}

func (d *dummyPlugin) FetchData(ctx context.Context, req *plugin.FetchRequest) (*plugin.FetchResponse, error) {
	return &plugin.FetchResponse{Data: "Plugin Data Here"}, nil
}

func (d *dummyPlugin) Render(ctx context.Context, req *plugin.RenderRequest) (*plugin.RenderResponse, error) {
	return &plugin.RenderResponse{RenderedString: "Plugin Active"}, nil
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: dummy_grpc_plugin <pipe_path>")
	}
	pipePath := os.Args[1]

	lis, err := transport.ListenPipe(pipePath)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	defer lis.Close()

	s := grpc.NewServer()
	plugin.RegisterWidgetPluginServer(s, &dummyPlugin{})

	fmt.Fprintf(os.Stdout, "PIPE_PATH=%s\n", pipePath)
	os.Stdout.Sync()

	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
