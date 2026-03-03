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

// StaticModelRegistry is a simple in-memory ModelRegistry.
type StaticModelRegistry struct {
	mu        sync.RWMutex
	providers map[string]blades.ModelProvider
}

// NewStaticModelRegistry creates a new empty StaticModelRegistry.
func NewStaticModelRegistry() *StaticModelRegistry {
	return &StaticModelRegistry{
		providers: make(map[string]blades.ModelProvider),
	}
}

// Register adds a model provider under the given name.
func (r *StaticModelRegistry) Register(name string, provider blades.ModelProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[name] = provider
}

// Resolve returns the ModelProvider registered under the given name.
func (r *StaticModelRegistry) Resolve(name string) (blades.ModelProvider, error) {
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
