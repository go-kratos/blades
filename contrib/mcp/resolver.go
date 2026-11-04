package mcp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/go-kratos/blades/tools"
)

// ToolsResolver manages multiple MCP server connections and provides unified tool access.
type ToolsResolver struct {
	mu           sync.RWMutex
	resolveMu    sync.Mutex
	clients      []*Client
	clientTools  [][]tools.Tool
	clientLoaded []bool
	tools        []tools.Tool
	onError      func(error)
}

// ResolverOption configures a ToolsResolver.
type ResolverOption func(*ToolsResolver)

// WithErrorHandler returns a ResolverOption that registers fn as the handler
// invoked with the aggregated error whenever a Resolve call partially fails:
// at least one server loaded its tools while one or more other servers
// failed. Partial failures are otherwise invisible to callers, because
// Resolve keeps returning the successfully loaded tools with a nil error
// (consumers such as the agent treat any returned error as total failure and
// drop all tools). The handler is invoked synchronously before Resolve
// returns. It is not invoked when every server fails, because that failure
// is reported through the returned error instead.
func WithErrorHandler(fn func(error)) ResolverOption {
	return func(r *ToolsResolver) {
		r.onError = fn
	}
}

// NewToolsResolver creates a new MCP tools resolver.
func NewToolsResolver(configs ...ClientConfig) (*ToolsResolver, error) {
	return NewToolsResolverWithOptions(configs)
}

// NewToolsResolverWithOptions creates a new MCP tools resolver with the
// given server configs and options.
func NewToolsResolverWithOptions(configs []ClientConfig, opts ...ResolverOption) (*ToolsResolver, error) {
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
	r := &ToolsResolver{
		clients:      clients,
		clientTools:  make([][]tools.Tool, len(clients)),
		clientLoaded: make([]bool, len(clients)),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r, nil
}

func (r *ToolsResolver) getTools() []tools.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.tools)
}

func (r *ToolsResolver) setTools(tools []tools.Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools = slices.Clone(tools)
}

// allLoaded reports whether every configured client has loaded its tools.
func (r *ToolsResolver) allLoaded() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, loaded := range r.clientLoaded {
		if !loaded {
			return false
		}
	}
	return len(r.clients) > 0
}

// anyLoaded reports whether at least one client has loaded its tools.
func (r *ToolsResolver) anyLoaded() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, loaded := range r.clientLoaded {
		if loaded {
			return true
		}
	}
	return false
}

func (r *ToolsResolver) isLoaded(i int) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.clientLoaded[i]
}

// setClientTools records the tools loaded from one client, marks that client
// as loaded, and rebuilds the aggregated tool list in config order.
func (r *ToolsResolver) setClientTools(i int, clientTools []tools.Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clientTools[i] = slices.Clone(clientTools)
	r.clientLoaded[i] = true
	all := make([]tools.Tool, 0, len(r.tools))
	for _, tools := range r.clientTools {
		all = append(all, tools...)
	}
	r.tools = all
}

func (r *ToolsResolver) errorHandler() func(error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.onError
}

// loadClientTools connects to one MCP server and converts its tools. A nil
// slice with collected errors means the server could not be loaded at all;
// a non-nil slice means the server loaded and only the collected conversion
// errors (if any) went wrong.
func loadClientTools(ctx context.Context, client *Client) ([]tools.Tool, []error) {
	if err := client.Connect(ctx); err != nil {
		return nil, []error{err}
	}
	mcpTools, err := client.ListTools(ctx)
	if err != nil {
		client.Close()
		return nil, []error{err}
	}
	// Convert MCP tools to Blades tools using client's built-in conversion
	converted := make([]tools.Tool, 0, len(mcpTools))
	var errs []error
	for _, mcpTool := range mcpTools {
		handler := client.handler(mcpTool.Name)
		tool, err := toBladesTool(mcpTool, handler)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to convert MCP tool [%s]: %w", mcpTool.Name, err))
			continue
		}
		converted = append(converted, tool)
	}
	return converted, errs
}

// Resolve implements the tools.Resolver interface.
// Returns all tools from all configured MCP servers using lazy loading.
//
// Only servers that loaded successfully are cached: a server that fails is
// retried on the next Resolve call, so a server that recovers later still
// contributes its tools. When some servers fail but at least one succeeds,
// the loaded tools are returned with a nil error and the failure is reported
// to the handler registered with WithErrorHandler, if any. When no server
// can be loaded, Resolve returns an error aggregating every failure.
func (r *ToolsResolver) Resolve(ctx context.Context) ([]tools.Tool, error) {
	r.resolveMu.Lock()
	defer r.resolveMu.Unlock()
	// Return cached tools if every server has already loaded
	if r.allLoaded() {
		return r.getTools(), nil
	}
	var errs []error
	for i, client := range r.clients {
		// Skip servers that already loaded; retry only the failed ones
		if r.isLoaded(i) {
			continue
		}
		clientTools, loadErrs := loadClientTools(ctx, client)
		errs = append(errs, loadErrs...)
		if clientTools != nil {
			r.setClientTools(i, clientTools)
		}
	}
	if len(errs) > 0 {
		// If no server could be loaded, report the failure as an error
		if !r.anyLoaded() {
			return nil, fmt.Errorf("failed to load any tools: %w", errors.Join(errs...))
		}
		// Keep partial success behavior: return the loaded tools, but surface
		// the failure through the error handler instead of swallowing it.
		if handler := r.errorHandler(); handler != nil {
			handler(errors.Join(errs...))
		}
	}
	return r.getTools(), nil
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
