package mcp

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/go-kratos/blades/tools"
)

// ToolsResolver manages multiple MCP server connections and provides unified tool access.
type ToolsResolver struct {
	mu      sync.RWMutex
	clients []*Client
	tools   []tools.Tool
	loaded  atomic.Bool
}

// NewToolsResolver creates a new MCP tools resolver.
func NewToolsResolver(configs ...ClientConfig) (*ToolsResolver, error) {
	if len(configs) == 0 {
		return nil, fmt.Errorf("at least one server config is required")
	}
	clients := make([]*Client, 0, len(configs))
	for _, config := range configs {
		client, err := NewClient(config)
		if err != nil {
			return nil, err
		}
		clients = append(clients, client)
	}
	return &ToolsResolver{
		clients: clients,
	}, nil
}

func (r *ToolsResolver) getTools() []tools.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools
}

func (r *ToolsResolver) setTools(tools []tools.Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools = tools
}

// Resolve implements the tools.Resolver interface.
// Returns all tools from all configured MCP servers using lazy loading.
func (r *ToolsResolver) Resolve(ctx context.Context) ([]tools.Tool, error) {
	// Return cached tools if already loaded
	if r.loaded.Load() {
		return r.getTools(), nil
	}
	var (
		errors   []error
		allTools []tools.Tool
	)
	for _, client := range r.clients {
		if err := client.Connect(ctx); err != nil {
			errors = append(errors, err)
			continue
		}
		mcpTools, err := client.ListTools(ctx)
		if err != nil {
			errors = append(errors, err)
			client.Close()
			continue
		}
		// Convert MCP tools to Blades tools using client's built-in conversion
		for _, mcpTool := range mcpTools {
			handler := client.handler(mcpTool.Name)
			tool, err := toBladesTool(mcpTool, handler)
			if err != nil {
				errors = append(errors, fmt.Errorf("failed to convert MCP tool [%s]: %w", mcpTool.Name, err))
				continue
			}
			allTools = append(allTools, tool)
		}
	}
	// If we collected errors but also got some tools, log errors but continue
	if len(errors) > 0 && len(allTools) == 0 {
		return nil, fmt.Errorf("failed to load any tools: %v", errors)
	}
	if len(errors) > 0 {
		fmt.Printf("Some errors occurred while loading tools: %v\n", errors)
	}
	r.setTools(allTools)
	r.loaded.Store(true)
	return allTools, nil
}

// Close closes all client connections.
func (r *ToolsResolver) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var errors []error
	for _, client := range r.clients {
		if err := client.Close(); err != nil {
			errors = append(errors, fmt.Errorf("server %w", err))
		}
	}
	if len(errors) > 0 {
		return fmt.Errorf("errors closing clients: %v", errors)
	}
	return nil
}
