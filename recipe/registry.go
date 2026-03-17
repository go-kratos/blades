package recipe

import (
	"fmt"
	"sync"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/tools"
)

// ModelResolver resolves model names from YAML to actual ModelProvider instances.
type ModelResolver interface {
	Resolve(name string) (blades.ModelProvider, error)
}

// ToolResolver resolves tool names from YAML to actual Tool instances.
type ToolResolver interface {
	Resolve(name string) (tools.Tool, error)
}

// MiddlewareFactory constructs a blades.Middleware from YAML options.
// The options map contains the key-value pairs from the middleware's `options:` block.
// A nil or empty map is passed when no options are declared.
type MiddlewareFactory func(options map[string]any) (blades.Middleware, error)

// MiddlewareResolver resolves middleware names from YAML to Middleware instances,
// passing the per-declaration options to the registered factory.
type MiddlewareResolver interface {
	Resolve(name string, options map[string]any) (blades.Middleware, error)
}

// MiddlewareRegistry is a simple in-memory MiddlewareResolver backed by factories.
type MiddlewareRegistry struct {
	mu        sync.RWMutex
	factories map[string]MiddlewareFactory
}

// NewMiddlewareRegistry creates a new empty MiddlewareRegistry.
func NewMiddlewareRegistry() *MiddlewareRegistry {
	return &MiddlewareRegistry{
		factories: make(map[string]MiddlewareFactory),
	}
}

// Register adds a middleware factory under the given name.
func (r *MiddlewareRegistry) Register(name string, factory MiddlewareFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[name] = factory
}

// Resolve calls the registered factory for name, passing options, and returns the Middleware.
func (r *MiddlewareRegistry) Resolve(name string, options map[string]any) (blades.Middleware, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.factories[name]
	if !ok {
		return nil, fmt.Errorf("recipe: middleware %q not found in registry", name)
	}
	return f(options)
}

// ModelRegistry is a simple in-memory ModelResolver.
type ModelRegistry struct {
	mu        sync.RWMutex
	providers map[string]blades.ModelProvider
}

// NewModelRegistry creates a new empty ModelRegistry.
func NewModelRegistry() *ModelRegistry {
	return &ModelRegistry{
		providers: make(map[string]blades.ModelProvider),
	}
}

// Register adds a model provider under the given name.
func (r *ModelRegistry) Register(name string, provider blades.ModelProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[name] = provider
}

// Resolve returns the ModelProvider registered under the given name.
func (r *ModelRegistry) Resolve(name string) (blades.ModelProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("recipe: model %q not found in registry", name)
	}
	return p, nil
}

// ToolRegistry is a simple in-memory ToolResolver.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]tools.Tool
}

// NewToolRegistry creates a new empty ToolRegistry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]tools.Tool),
	}
}

// Register adds a tool under the given name.
func (r *ToolRegistry) Register(name string, tool tools.Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[name] = tool
}

// Resolve returns the Tool registered under the given name.
func (r *ToolRegistry) Resolve(name string) (tools.Tool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("recipe: tool %q not found in registry", name)
	}
	return t, nil
}
