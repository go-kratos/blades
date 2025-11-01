package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"sync"

	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Client wraps the official MCP SDK client for a single server connection
type Client struct {
	config    ServerConfig
	client    *mcp.Client
	session   *mcp.ClientSession
	mu        sync.Mutex
	connected bool
}

// NewClient creates a new MCP client
func NewClient(config ServerConfig) (*Client, error) {
	config.SetDefaults()
	if err := config.Validate(); err != nil {
		return nil, err
	}

	// Create the official MCP client
	mcpClient := mcp.NewClient(&mcp.Implementation{
		Name:    "blades",
		Version: "0.1.0",
	}, nil)

	return &Client{
		config: config,
		client: mcpClient,
	}, nil
}

// Connect establishes connection to the MCP server
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	var transport mcp.Transport
	var err error

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
	c.connected = true

	return nil
}

// createStdioTransport creates a CommandTransport for stdio communication
func (c *Client) createStdioTransport() (mcp.Transport, error) {
	cmd := exec.Command(c.config.Command, c.config.Args...)

	// Set environment variables
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

// createStreamableTransport creates a StreamableClientTransport for HTTP/WebSocket communication
// Supports both HTTP (http://https://) and WebSocket (ws://wss://) based on URL scheme
func (c *Client) createStreamableTransport() (mcp.Transport, error) {
	transport := &mcp.StreamableClientTransport{
		Endpoint: c.config.URL,
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

// ListTools lists all available tools from the server
func (c *Client) ListTools(ctx context.Context) ([]*mcp.Tool, error) {
	if !c.connected {
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

// CallTool calls a tool on the server
func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (*mcp.CallToolResult, error) {
	if !c.connected {
		if err := c.Connect(ctx); err != nil {
			return nil, err
		}
	}

	// Marshal arguments to JSON
	argsJSON, err := json.Marshal(arguments)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal arguments: %w", err)
	}

	// Call the tool
	result, err := c.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      name,
		Arguments: argsJSON,
	})

	if err != nil {
		return nil, fmt.Errorf("mcp [%s] call_tool: %w", c.config.Name, err)
	}

	return result, nil
}

// Close closes the client connection
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil
	}

	if c.session != nil {
		if err := c.session.Close(); err != nil {
			return fmt.Errorf("mcp [%s] close: %w", c.config.Name, err)
		}
	}

	c.connected = false
	return nil
}

// ServerName returns the server's configured name
func (c *Client) ServerName() string {
	return c.config.Name
}

// ToBladesTool converts an MCP tool to a Blades tool
// This method is used by Provider to convert tools without creating separate Adapter instances
func (c *Client) ToBladesTool(mcpTool *mcp.Tool) (*tools.Tool, error) {
	// Convert the input schema
	inputSchema, err := c.convertSchema(mcpTool.InputSchema)
	if err != nil {
		return nil, fmt.Errorf("failed to convert input schema: %w", err)
	}

	// Convert the output schema if present
	var outputSchema *jsonschema.Schema
	if mcpTool.OutputSchema != nil {
		outputSchema, err = c.convertSchema(mcpTool.OutputSchema)
		if err != nil {
			return nil, fmt.Errorf("failed to convert output schema: %w", err)
		}
	}

	// Create a handler that calls the MCP tool via this client
	handler := tools.HandleFunc[string, string](func(ctx context.Context, input string) (string, error) {
		// Parse the input JSON into a map
		var arguments map[string]any
		if err := json.Unmarshal([]byte(input), &arguments); err != nil {
			return "", fmt.Errorf("invalid input JSON: %w", err)
		}

		// Call the MCP tool through this client
		result, err := c.CallTool(ctx, mcpTool.Name, arguments)
		if err != nil {
			return "", err
		}

		// Convert the result to JSON string
		output, err := c.formatToolResult(result)
		if err != nil {
			return "", fmt.Errorf("failed to format tool result: %w", err)
		}

		return output, nil
	})

	// Create and return the Blades tool
	return &tools.Tool{
		Name:         mcpTool.Name,
		Description:  mcpTool.Description,
		InputSchema:  inputSchema,
		OutputSchema: outputSchema,
		Handler:      handler,
	}, nil
}

// convertSchema converts an MCP schema to a Blades jsonschema.Schema
func (c *Client) convertSchema(mcpSchema any) (*jsonschema.Schema, error) {
	if mcpSchema == nil {
		// If no schema provided, create an empty object schema
		return &jsonschema.Schema{
			Type: "object",
		}, nil
	}

	// Marshal the interface{} to JSON
	schemaBytes, err := json.Marshal(mcpSchema)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schema: %w", err)
	}

	// Unmarshal into jsonschema.Schema
	var schema jsonschema.Schema
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		return nil, fmt.Errorf("failed to unmarshal schema: %w", err)
	}

	return &schema, nil
}

// formatToolResult converts MCP CallToolResult to a JSON string
func (c *Client) formatToolResult(result *mcp.CallToolResult) (string, error) {
	// Check if the tool execution failed
	if result.IsError {
		// Extract error message from Content
		if len(result.Content) > 0 {
			// Try to extract text content as error message
			var errorMsg string
			for _, content := range result.Content {
				if textContent, ok := content.(*mcp.TextContent); ok {
					errorMsg += textContent.Text
				}
			}
			if errorMsg != "" {
				return "", fmt.Errorf("tool execution failed: %s", errorMsg)
			}
		}
		return "", fmt.Errorf("tool execution failed")
	}

	// Prefer StructuredContent if available
	if result.StructuredContent != nil {
		outputBytes, err := json.Marshal(result.StructuredContent)
		if err != nil {
			return "", fmt.Errorf("failed to marshal structured content: %w", err)
		}
		return string(outputBytes), nil
	}

	// Otherwise use Content
	if len(result.Content) == 0 {
		return "{}", nil
	}

	outputBytes, err := json.Marshal(result.Content)
	if err != nil {
		return "", fmt.Errorf("failed to marshal content: %w", err)
	}

	return string(outputBytes), nil
}

type headerPair struct {
	key   string
	value string
}

type headerRoundTripper struct {
	base    http.RoundTripper
	headers []headerPair
}

func newHeaderRoundTripper(headers map[string]string, base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	pairs := make([]headerPair, 0, len(headers))
	for k, v := range headers {
		if k == "" {
			continue
		}
		pairs = append(pairs, headerPair{key: k, value: v})
	}
	if len(pairs) == 0 {
		return base
	}
	return &headerRoundTripper{
		base:    base,
		headers: pairs,
	}
}

func (rt *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for _, kv := range rt.headers {
		req.Header.Set(kv.key, kv.value)
	}
	return rt.base.RoundTrip(req)
}
