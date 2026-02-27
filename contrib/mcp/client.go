package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var _ tools.Resolver = (*Client)(nil)

// Client wraps the official MCP SDK client for a single server connection.
type Client struct {
	config        ClientConfig
	client        *mcp.Client
	session       *mcp.ClientSession
	connected     atomic.Bool
	connectMutex  sync.Mutex
	connectCtx    context.Context
	connectCancel context.CancelFunc
	reconnecting  atomic.Bool
}

// NewClient creates a new MCP client.
func NewClient(config ClientConfig) (*Client, error) {
	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	client := mcp.NewClient(&mcp.Implementation{
		Name:    config.Name,
		Version: blades.Version,
	}, nil)
	c := &Client{
		config: config,
		client: client,
	}
	c.connectCtx, c.connectCancel = context.WithCancel(context.Background())
	return c, nil
}

// Connect establishes connection to the MCP server.
func (c *Client) Connect(ctx context.Context) error {
	return c.connect(ctx, true)
}

func (c *Client) connect(ctx context.Context, startReconnect bool) error {
	// Ensure only one connection attempt at a time
	c.connectMutex.Lock()
	defer c.connectMutex.Unlock()
	if c.connectCtx == nil || c.connectCtx.Err() != nil {
		c.connectCtx, c.connectCancel = context.WithCancel(context.Background())
	}
	// If already connected, return
	if c.connected.Load() {
		return nil
	}
	var (
		err       error
		transport mcp.Transport
	)
	switch c.config.Transport {
	case TransportStdio:
		transport, err = c.createStdioTransport()
	case TransportHTTP, TransportWebSocket:
		// Both HTTP and WebSocket use StreamableClientTransport
		// The transport is determined by the URL scheme (http/https vs ws/wss)
		transport, err = c.createStreamableTransport()
	default:
		return fmt.Errorf("mcp: invalid config: unsupported transport: %s", c.config.Transport)
	}
	if err != nil {
		return fmt.Errorf("mcp [%s] create_transport: %w", c.config.Name, err)
	}
	// Connect to the server
	session, err := c.client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("mcp [%s] connect: %w", c.config.Name, err)
	}
	c.session = session
	c.connected.Store(true)
	if startReconnect && c.reconnecting.CompareAndSwap(false, true) {
		go c.reconnect(c.connectCtx)
	}
	return nil
}

// createStdioTransport creates a CommandTransport for stdio communication.
func (c *Client) createStdioTransport() (mcp.Transport, error) {
	cmd := exec.Command(c.config.Command, c.config.Args...)
	// Set environment variables
	cmd.Env = os.Environ()
	if len(c.config.Env) > 0 {
		for k, v := range c.config.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}
	// Set working directory
	if c.config.WorkDir != "" {
		cmd.Dir = c.config.WorkDir
	}
	return &mcp.CommandTransport{
		Command: cmd,
	}, nil
}

// createStreamableTransport creates a StreamableClientTransport for HTTP/WebSocket communication.
// Supports both HTTP (http://https://) and WebSocket (ws://wss://) based on URL scheme.
func (c *Client) createStreamableTransport() (mcp.Transport, error) {
	transport := &mcp.StreamableClientTransport{
		Endpoint: c.config.Endpoint,
	}
	if len(c.config.Headers) > 0 {
		baseTransport := http.DefaultTransport
		httpClient := &http.Client{
			Transport: newHeaderRoundTripper(c.config.Headers, baseTransport),
		}
		transport.HTTPClient = httpClient
	}
	return transport, nil
}

// ListTools lists all available tools from the server.
func (c *Client) ListTools(ctx context.Context) ([]*mcp.Tool, error) {
	if !c.connected.Load() {
		if err := c.Connect(ctx); err != nil {
			return nil, err
		}
	}
	result, err := c.session.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp [%s] list_tools: %w", c.config.Name, err)
	}
	return result.Tools, nil
}

// Resolve implements the tools.Resolver interface.
func (c *Client) Resolve(ctx context.Context) ([]tools.Tool, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	mcpTools, err := c.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	var res []tools.Tool
	for _, mcpTool := range mcpTools {
		handler := c.handler(mcpTool.Name)
		tool, err := toBladesTool(mcpTool, handler)
		if err != nil {
			return nil, fmt.Errorf("failed to convert MCP tool [%s]: %w", mcpTool.Name, err)
		}
		res = append(res, tool)
	}
	return res, nil
}

// handler returns a tool handler that calls the MCP tool.
func (c *Client) handler(name string) tools.HandleFunc {
	return func(ctx context.Context, input string) (string, error) {
		var arguments map[string]any
		if err := json.Unmarshal([]byte(input), &arguments); err != nil {
			return "", fmt.Errorf("invalid input JSON: %w", err)
		}
		result, err := c.CallTool(ctx, name, arguments)
		if err != nil {
			return "", err
		}
		output, err := formatToolResult(result)
		if err != nil {
			return "", fmt.Errorf("failed to format tool result: %w", err)
		}
		return output, nil
	}
}

// CallTool calls a tool on the server.
func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (*mcp.CallToolResult, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	if !c.connected.Load() {
		if err := c.Connect(ctx); err != nil {
			return nil, err
		}
	}
	result, err := c.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      name,
		Arguments: arguments,
	})
	if err != nil {
		return nil, fmt.Errorf("mcp [%s] call_tool: %w", c.config.Name, err)
	}
	return result, nil
}

// Close closes the client connection.
func (c *Client) Close() error {
	if c.connectCancel != nil {
		c.connectCancel()
	}
	c.connectMutex.Lock()
	session := c.session
	c.session = nil
	c.connected.Store(false)
	c.connectMutex.Unlock()
	if session != nil {
		if err := session.Close(); err != nil {
			return fmt.Errorf("mcp [%s] close: %w", c.config.Name, err)
		}
	}
	return nil
}

func (c *Client) reconnect(ctx context.Context) {
	defer c.reconnecting.Store(false)
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c.connectMutex.Lock()
		session := c.session
		connected := c.connected.Load()
		c.connectMutex.Unlock()
		if session != nil && connected {
			session.Wait()
			c.connected.Store(false)
		}

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if err := c.connect(ctx, false); err == nil {
				backoff = time.Second
				break
			}
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			if backoff < maxBackoff {
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
		}
	}
}
