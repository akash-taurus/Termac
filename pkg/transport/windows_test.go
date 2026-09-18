//go:build windows

package transport

import (
	"context"
	"os"
	"testing"
	"time"

	"tui/pkg/proto/plugin"
	"tui/pkg/verify"
)

type testPluginServer struct {
	plugin.UnimplementedWidgetPluginServer
}

func (s *testPluginServer) FetchData(ctx context.Context, req *plugin.FetchRequest) (*plugin.FetchResponse, error) {
	return &plugin.FetchResponse{Data: "Plugin Data Here"}, nil
}

func (s *testPluginServer) Render(ctx context.Context, req *plugin.RenderRequest) (*plugin.RenderResponse, error) {
	return &plugin.RenderResponse{RenderedString: "Plugin Active"}, nil
}

func TestIntegration_NamedPipeGRPC(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pipePath := GeneratePipePath("integration_test")

	ps, err := NewPipeServer(pipePath)
	if err != nil {
		t.Fatalf("NewPipeServer failed: %v", err)
	}
	defer ps.Close()

	grpcServer := NewPluginServer()
	testServer := &testPluginServer{}
	plugin.RegisterWidgetPluginServer(grpcServer, testServer)

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- ps.Serve(grpcServer)
	}()

	time.Sleep(100 * time.Millisecond)

	clientConn, err := DialPipe(ctx, pipePath)
	if err != nil {
		grpcServer.Stop()
		t.Fatalf("DialPipe failed: %v", err)
	}
	defer clientConn.Close()

	client := plugin.NewWidgetPluginClient(clientConn)

	fetchResp, err := client.FetchData(ctx, &plugin.FetchRequest{PluginId: "test_plugin"})
	if err != nil {
		grpcServer.Stop()
		t.Fatalf("FetchData failed: %v", err)
	}
	if fetchResp.GetData() != "Plugin Data Here" {
		t.Errorf("FetchData returned unexpected data: %q", fetchResp.GetData())
	}

	renderResp, err := client.Render(ctx, &plugin.RenderRequest{
		PluginId: "test_plugin",
		Width:    80,
		Height:   24,
	})
	if err != nil {
		grpcServer.Stop()
		t.Fatalf("Render failed: %v", err)
	}
	if renderResp.GetRenderedString() != "Plugin Active" {
		t.Errorf("Render returned unexpected string: %q", renderResp.GetRenderedString())
	}

	verify.AssertNoTCPSockets(t, os.Getpid(), "NamedPipe gRPC integration test")

	grpcServer.Stop()
	<-serveErr
}
