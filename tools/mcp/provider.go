package mcp

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-kratos/blades/tools"
)

// Provider manages multiple MCP server connections and provides unified tool access
type Provider struct {
	configs []ServerConfig
	clients map[string]*Client // serverName -> client
	tools   []*tools.Tool
	mu      sync.RWMutex
	loaded  bool
}

// NewProvider creates a new MCP tool provider
func NewProvider(configs ...ServerConfig) (*Provider, error) {
	if len(configs) == 0 {
		return nil, fmt.Errorf("at least one server config is required")
	}

	// Validate that server names are unique
	names := make(map[string]bool)
	for _, cfg := range configs {
		if names[cfg.Name] {
			return nil, fmt.Errorf("duplicate server name: %s", cfg.Name)
		}
		names[cfg.Name] = true
	}

	return &Provider{
		configs: configs,
		clients: make(map[string]*Client),
	}, nil
}

// GetTools returns all tools from all configured MCP servers
// Uses lazy loading - connects to servers on first call
func (p *Provider) GetTools(ctx context.Context) ([]*tools.Tool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Return cached tools if already loaded
	if p.loaded {
		return p.tools, nil
	}

	var allTools []*tools.Tool
	var errors []error

	// Connect to each server and collect tools
	for _, config := range p.configs {
		// Create client
		client, err := NewClient(config)
		if err != nil {
			errors = append(errors, fmt.Errorf("server %s: %w", config.Name, err))
			continue
		}

		// Connect to server
		if err := client.Connect(ctx); err != nil {
			errors = append(errors, err)
			continue
		}

		// List tools
		mcpTools, err := client.ListTools(ctx)
		if err != nil {
			errors = append(errors, err)
			client.Close()
			continue
		}

		// Convert MCP tools to Blades tools using client's built-in conversion
		for _, mcpTool := range mcpTools {
			bladesTool, err := client.ToBladesTool(mcpTool)
			if err != nil {
				errors = append(errors, fmt.Errorf("server %s, tool %s: %w",
					config.Name, mcpTool.Name, err))
				continue
			}
			allTools = append(allTools, bladesTool)
		}

		// Save the client for later use
		p.clients[config.Name] = client
	}

	// If we collected errors but also got some tools, log errors but continue
	if len(errors) > 0 && len(allTools) == 0 {
		return nil, fmt.Errorf("failed to load any tools: %v", errors)
	}

	p.tools = allTools
	p.loaded = true

	return allTools, nil
}

// RefreshTools forces a reload of all tools from all servers
func (p *Provider) RefreshTools(ctx context.Context) error {
	p.mu.Lock()
	p.loaded = false
	p.mu.Unlock()

	_, err := p.GetTools(ctx)
	return err
}

// Close closes all client connections
func (p *Provider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var errors []error
	for name, client := range p.clients {
		if err := client.Close(); err != nil {
			errors = append(errors, fmt.Errorf("server %s: %w", name, err))
		}
	}

	p.clients = make(map[string]*Client)
	p.loaded = false

	if len(errors) > 0 {
		return fmt.Errorf("errors closing clients: %v", errors)
	}

	return nil
}

// GetClient returns the client for a specific server
func (p *Provider) GetClient(name string) (*Client, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	client, ok := p.clients[name]
	return client, ok
}
