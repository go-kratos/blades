package tools

import "context"

// Resolver defines the interface for dynamically resolving tools from various sources.
// Implementations can provide tools from MCP servers, plugins, remote services, etc.
type Resolver interface {
	// Resolve returns a list of tools available from this resolver.
	Resolve(ctx context.Context) ([]Tool, error)
}
