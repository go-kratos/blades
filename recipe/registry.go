package recipe

import (
	"fmt"
	"sync"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/tools"
)

// ModelRegistry resolves model names from YAML to actual ModelProvider instances.
type ModelRegistry interface {
	Resolve(name string) (blades.ModelProvider, error)
}

// ToolRegistry resolves tool names from YAML to actual Tool instances.
type ToolRegistry interface {
	Resolve(name string) (tools.Tool, error)
}

// MiddlewareFactory constructs a blades.Middleware from YAML options.
// The options map contains the key-value pairs from the middleware's `options:` block.
// A nil or empty map is passed when no options are declared.
type MiddlewareFactory func(options map[string]any) (blades.Middleware, error)

// MiddlewareRegistry resolves middleware names from YAML to Middleware instances,
// passing the per-declaration options to the registered factory.
type MiddlewareRegistry interface {
	Resolve(name string, options map[string]any) (blades.Middleware, error)
}

// StaticMiddlewareRegistry is a simple in-memory MiddlewareRegistry backed by factories.
type StaticMiddlewareRegistry struct {
	mu        sync.RWMutex
	factories map[string]MiddlewareFactory
}

// NewStaticMiddlewareRegistry creates a new empty StaticMiddlewareRegistry.
func NewStaticMiddlewareRegistry() *StaticMiddlewareRegistry {
	return &StaticMiddlewareRegistry{
		factories: make(map[string]MiddlewareFactory),
	}
}

// Register adds a middleware factory under the given name.
func (r *StaticMiddlewareRegistry) Register(name string, factory MiddlewareFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[name] = factory
}

// Resolve calls the registered factory for name, passing options, and returns the Middleware.
func (r *StaticMiddlewareRegistry) Resolve(name string, options map[string]any) (blades.Middleware, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.factories[name]
	if !ok {
		return nil, fmt.Errorf("recipe: middleware %q not found in registry", name)
	}
	return f(options)
}

// Registry is a simple in-memory ModelRegistry.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]blades.ModelProvider
}

// NewRegistry creates a new empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]blades.ModelProvider),
	}
}

// Register adds a model provider under the given name.
func (r *Registry) Register(name string, provider blades.ModelProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[name] = provider
}

// Resolve returns the ModelProvider registered under the given name.
func (r *Registry) Resolve(name string) (blades.ModelProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("recipe: model %q not found in registry", name)
	}
	return p, nil
}

// StaticToolRegistry is a simple in-memory ToolRegistry.
type StaticToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]tools.Tool
}

// NewStaticToolRegistry creates a new empty StaticToolRegistry.
func NewStaticToolRegistry() *StaticToolRegistry {
	return &StaticToolRegistry{
		tools: make(map[string]tools.Tool),
	}
}

// Register adds a tool under the given name.
func (r *StaticToolRegistry) Register(name string, tool tools.Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[name] = tool
}

// Resolve returns the Tool registered under the given name.
func (r *StaticToolRegistry) Resolve(name string) (tools.Tool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("recipe: tool %q not found in registry", name)
	}
	return t, nil
}
