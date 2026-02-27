package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

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
