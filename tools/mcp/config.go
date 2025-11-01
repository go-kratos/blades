package mcp

import (
	"fmt"
	"time"
)

// TransportType defines the communication method for MCP servers
type TransportType string

const (
	// TransportStdio uses standard input/output for communication
	TransportStdio TransportType = "stdio"
	// TransportHTTP uses HTTP for communication
	TransportHTTP TransportType = "http"
	// TransportWebSocket uses WebSocket for communication
	TransportWebSocket TransportType = "websocket"
)

// ServerConfig configures an MCP server connection
type ServerConfig struct {
	// Name is the unique identifier for this server
	Name string

	// Transport specifies the communication method
	Transport TransportType

	// === Stdio Configuration (when Transport = TransportStdio) ===

	// Command is the executable to run (e.g., "python", "node", "npx")
	Command string

	// Args are the command arguments (e.g., ["-m", "mcp_server_time"])
	Args []string

	// Env contains environment variables for the subprocess
	Env map[string]string

	// WorkDir is the working directory for the subprocess
	WorkDir string

	// === HTTP Configuration (when Transport = TransportHTTP) ===

	// URL is the MCP server endpoint
	URL string

	// Headers are custom HTTP headers to include in requests
	Headers map[string]string

	// Timeout is the request timeout duration
	Timeout time.Duration

	// === Advanced Configuration ===

	// MaxRetries is the maximum number of retry attempts on failure
	MaxRetries int

	// RetryDelay is the delay between retry attempts
	RetryDelay time.Duration

	// EnableLogging enables detailed debug logging
	EnableLogging bool
}

// Validate checks if the configuration is valid
func (c *ServerConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("mcp: invalid config: server name is required")
	}

	switch c.Transport {
	case TransportStdio:
		if c.Command == "" {
			return fmt.Errorf("mcp: invalid config: command is required for stdio transport")
		}
	case TransportHTTP, TransportWebSocket:
		if c.URL == "" {
			return fmt.Errorf("mcp: invalid config: URL is required for HTTP/WebSocket transport")
		}
	default:
		return fmt.Errorf("mcp: invalid config: unsupported transport type: %s", c.Transport)
	}

	return nil
}

// SetDefaults sets default values for unspecified configuration options
func (c *ServerConfig) SetDefaults() {
	if c.Timeout == 0 {
		c.Timeout = 30 * time.Second
	}
	if c.MaxRetries == 0 {
		c.MaxRetries = 3
	}
	if c.RetryDelay == 0 {
		c.RetryDelay = time.Second
	}
}
