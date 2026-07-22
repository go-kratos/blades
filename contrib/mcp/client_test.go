package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type callerContextKey struct{}

func TestSendingMiddlewareAddsCallMetadata(t *testing.T) {
	receivedMeta := make(chan sdkmcp.Meta, 1)
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test-server", Version: "v1"}, nil)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "lookup"}, func(
		_ context.Context,
		request *sdkmcp.CallToolRequest,
		input map[string]any,
	) (*sdkmcp.CallToolResult, map[string]any, error) {
		receivedMeta <- request.Params.Meta
		return nil, input, nil
	})
	httpServer := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(
		func(*http.Request) *sdkmcp.Server { return server },
		nil,
	))
	t.Cleanup(httpServer.Close)

	client, err := NewClient(ClientConfig{
		Name:      "test-client",
		Transport: TransportHTTP,
		Endpoint:  httpServer.URL,
		SendingMiddleware: []sdkmcp.Middleware{func(next sdkmcp.MethodHandler) sdkmcp.MethodHandler {
			return func(ctx context.Context, method string, request sdkmcp.Request) (sdkmcp.Result, error) {
				if method == "tools/call" {
					meta := request.GetParams().GetMeta()
					if meta == nil {
						meta = make(map[string]any)
					}
					meta["caller"] = ctx.Value(callerContextKey{})
					request.GetParams().SetMeta(meta)
				}
				return next(ctx, method, request)
			}
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	_, err = client.CallTool(
		context.WithValue(context.Background(), callerContextKey{}, "agent-1"),
		"lookup",
		map[string]any{"query": "weather"},
	)
	if err != nil {
		t.Fatal(err)
	}

	select {
	case meta := <-receivedMeta:
		if meta["caller"] != "agent-1" {
			t.Fatalf("received caller metadata = %#v, want agent-1", meta)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not receive tool call metadata")
	}
}

func TestCreateStdioTransportIncludesProcessEnv(t *testing.T) {
	t.Setenv("BLADES_MCP_BASE_ENV", "base")
	client, err := NewClient(ClientConfig{
		Name:      "test",
		Transport: TransportStdio,
		Command:   "cat",
		Env: map[string]string{
			"BLADES_MCP_OVERRIDE_ENV": "override",
		},
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	transport, err := client.createStdioTransport()
	if err != nil {
		t.Fatalf("createStdioTransport returned error: %v", err)
	}
	commandTransport, ok := transport.(*sdkmcp.CommandTransport)
	if !ok {
		t.Fatalf("unexpected transport type: %T", transport)
	}

	env := strings.Join(commandTransport.Command.Env, "\n")
	if !strings.Contains(env, "BLADES_MCP_BASE_ENV=base") {
		t.Fatalf("expected base process env to be preserved; env=%s", env)
	}
	if !strings.Contains(env, "BLADES_MCP_OVERRIDE_ENV=override") {
		t.Fatalf("expected override env to be present; env=%s", env)
	}
}

func TestReconnectStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	client, err := NewClient(ClientConfig{
		Name:      "test",
		Transport: TransportStdio,
		Command:   "cat",
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		client.reconnect(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reconnect did not stop after context cancellation")
	}
}
