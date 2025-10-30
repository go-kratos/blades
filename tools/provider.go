package tools

import "context"

// MCPToolProvider defines the interface for MCP tool providers
type MCPToolProvider interface {
	// GetTools returns all tools from MCP servers
	GetTools(ctx context.Context) ([]*Tool, error)
	// Close closes all MCP server connections
	Close() error
}
