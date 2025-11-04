package mcp

import (
	"fmt"
	"time"
)

// TransportType defines the communication method for MCP servers.
type TransportType string

const (
	// TransportStdio uses standard input/output for communication.
	TransportStdio TransportType = "stdio"
	// TransportHTTP uses HTTP for communication.
	TransportHTTP TransportType = "http"
	// TransportWebSocket uses WebSocket for communication.
	TransportWebSocket TransportType = "websocket"
)

// ClientConfig configures an MCP server connection
type ClientConfig struct {
	// Name is the unique identifier for the MCP server
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
	Endpoint string
	// Headers are custom HTTP headers to include in requests
	Headers map[string]string
	// Timeout is the request timeout duration
	Timeout time.Duration
}

// validate checks if the configuration is valid
func (c *ClientConfig) validate() error {
	if c.Timeout < 0 {
		c.Timeout = 30 * time.Second
	}
	switch c.Transport {
	case TransportStdio:
		if c.Command == "" {
			return fmt.Errorf("mcp: invalid config: command is required for stdio transport")
		}
	case TransportHTTP, TransportWebSocket:
		if c.Endpoint == "" {
			return fmt.Errorf("mcp: invalid config: URL is required for HTTP/WebSocket transport")
		}
	default:
		return fmt.Errorf("mcp: invalid config: unsupported transport type: %s", c.Transport)
	}
	return nil
}
