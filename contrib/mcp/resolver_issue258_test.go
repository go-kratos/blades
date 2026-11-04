package mcp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type noArgs struct{}

// startMCPServer serves a real MCP server with a single tool over streamable
// HTTP on the given listener.
func serveMCPOnListener(t *testing.T, ln net.Listener, toolName string) {
	t.Helper()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test-" + toolName, Version: "1.0.0"}, nil)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: toolName, Description: "test tool " + toolName},
		func(ctx context.Context, req *sdkmcp.CallToolRequest, _ noArgs) (*sdkmcp.CallToolResult, any, error) {
			return &sdkmcp.CallToolResult{
				Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "ok"}},
			}, nil, nil
		})
	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil)
	httpSrv := &http.Server{Handler: handler}
	go func() { _ = httpSrv.Serve(ln) }()
	t.Cleanup(func() { _ = httpSrv.Close() })
}

func startMCPServer(t *testing.T, toolName string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	serveMCPOnListener(t, ln, toolName)
	return fmt.Sprintf("http://%s/", ln.Addr().String())
}

// reserveDeadEndpoint returns an http endpoint on a port where nothing is
// listening (connections are refused), plus the port so a server can be
// started on it later.
func reserveDeadEndpoint(t *testing.T) (string, int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return fmt.Sprintf("http://127.0.0.1:%d/", port), port
}

func resolveCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 15*time.Second)
}

// TestResolveRetriesFailedServerAfterRecovery reproduces issue #258 using
// only the pre-existing API: with server A healthy and server B down, the
// first Resolve returns A's tools and no error; after B recovers, a second
// Resolve must return B's tools as well (the failed server is retried, not
// permanently cached as loaded).
func TestResolveRetriesFailedServerAfterRecovery(t *testing.T) {
	endpointA := startMCPServer(t, "tool_a")
	endpointB, portB := reserveDeadEndpoint(t)

	resolver, err := NewToolsResolver(
		ClientConfig{Name: "a", Transport: TransportHTTP, Endpoint: endpointA},
		ClientConfig{Name: "b", Transport: TransportHTTP, Endpoint: endpointB},
	)
	if err != nil {
		t.Fatalf("NewToolsResolver: %v", err)
	}
	defer resolver.Close()

	ctx, cancel := resolveCtx()
	defer cancel()
	got, err := resolver.Resolve(ctx)
	if err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	if len(got) != 1 || got[0].Name() != "tool_a" {
		t.Fatalf("first Resolve tools = %v, want only [tool_a]", got)
	}

	// Server B recovers: start a real MCP server on the same port.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", portB))
	if err != nil {
		t.Fatalf("listen for recovered server B: %v", err)
	}
	serveMCPOnListener(t, ln, "tool_b")

	ctx2, cancel2 := resolveCtx()
	defer cancel2()
	got, err = resolver.Resolve(ctx2)
	if err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range got {
		names[tool.Name()] = true
	}
	if !names["tool_a"] || !names["tool_b"] {
		t.Fatalf("second Resolve tools = %v, want tool_a and tool_b (failed server was never retried)", got)
	}
}

type errorRecorder struct {
	mu   sync.Mutex
	errs []error
}

func (r *errorRecorder) record(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errs = append(r.errs, err)
}

func (r *errorRecorder) all() []error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]error(nil), r.errs...)
}

// TestResolvePartialFailureInvokesErrorHandler is the issue's exact repro:
// server A is healthy, server B's stdio command does not exist. Resolve must
// return A's tools with a nil error (partial success), but B's failure must
// reach the registered error handler — and B must be retried, not cached as
// loaded, on the next Resolve.
func TestResolvePartialFailureInvokesErrorHandler(t *testing.T) {
	endpointA := startMCPServer(t, "tool_a")
	recorder := &errorRecorder{}

	resolver, err := NewToolsResolverWithOptions(
		[]ClientConfig{
			{Name: "a", Transport: TransportHTTP, Endpoint: endpointA},
			{Name: "dead-b", Transport: TransportStdio, Command: "blades-mcp-nonexistent-server-258"},
		},
		WithErrorHandler(recorder.record),
	)
	if err != nil {
		t.Fatalf("NewToolsResolverWithOptions: %v", err)
	}
	defer resolver.Close()

	ctx, cancel := resolveCtx()
	defer cancel()
	got, err := resolver.Resolve(ctx)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got) != 1 || got[0].Name() != "tool_a" {
		t.Fatalf("Resolve tools = %v, want only [tool_a]", got)
	}
	errs := recorder.all()
	if len(errs) != 1 {
		t.Fatalf("error handler invoked %d times, want 1 (partial failure was swallowed)", len(errs))
	}
	if !strings.Contains(errs[0].Error(), "dead-b") {
		t.Fatalf("handler error = %q, want it to identify server dead-b", errs[0])
	}

	// B is still down: a second Resolve must retry it and report again,
	// rather than serving the cached partial result silently.
	ctx2, cancel2 := resolveCtx()
	defer cancel2()
	got, err = resolver.Resolve(ctx2)
	if err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	if len(got) != 1 || got[0].Name() != "tool_a" {
		t.Fatalf("second Resolve tools = %v, want only [tool_a]", got)
	}
	if errs := recorder.all(); len(errs) != 2 {
		t.Fatalf("error handler invoked %d times after second Resolve, want 2 (failed server was not retried)", len(errs))
	}
}

// TestResolveSuccessDoesNotInvokeErrorHandler: when every server loads, the
// error handler stays silent and later Resolve calls serve the cache.
func TestResolveSuccessDoesNotInvokeErrorHandler(t *testing.T) {
	endpointA := startMCPServer(t, "tool_a")
	endpointB := startMCPServer(t, "tool_b")
	recorder := &errorRecorder{}

	resolver, err := NewToolsResolverWithOptions(
		[]ClientConfig{
			{Name: "a", Transport: TransportHTTP, Endpoint: endpointA},
			{Name: "b", Transport: TransportHTTP, Endpoint: endpointB},
		},
		WithErrorHandler(recorder.record),
	)
	if err != nil {
		t.Fatalf("NewToolsResolverWithOptions: %v", err)
	}
	defer resolver.Close()

	for call := 1; call <= 2; call++ {
		ctx, cancel := resolveCtx()
		got, err := resolver.Resolve(ctx)
		cancel()
		if err != nil {
			t.Fatalf("Resolve call %d: %v", call, err)
		}
		if len(got) != 2 {
			t.Fatalf("Resolve call %d tools = %v, want 2 tools", call, got)
		}
	}
	if errs := recorder.all(); len(errs) != 0 {
		t.Fatalf("error handler invoked %d times on full success, want 0: %v", len(errs), errs)
	}
}

// TestResolveAllServersDownReturnsError pins the pre-existing total-failure
// behavior: when no server can be reached, Resolve returns an error.
func TestResolveAllServersDownReturnsError(t *testing.T) {
	resolver, err := NewToolsResolver(
		ClientConfig{Name: "dead", Transport: TransportStdio, Command: "blades-mcp-nonexistent-server-258"},
	)
	if err != nil {
		t.Fatalf("NewToolsResolver: %v", err)
	}
	defer resolver.Close()

	ctx, cancel := resolveCtx()
	defer cancel()
	got, err := resolver.Resolve(ctx)
	if err == nil {
		t.Fatalf("Resolve with all servers down = %v tools, nil error; want an error", got)
	}
	if len(got) != 0 {
		t.Fatalf("Resolve with all servers down returned %d tools, want 0", len(got))
	}
}
