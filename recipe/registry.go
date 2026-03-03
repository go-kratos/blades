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

// ModelCreator is an optional interface that ModelRegistry implementations
// can support to create models on-the-fly via a named provider factory.
type ModelCreator interface {
	Create(provider, model string) (blades.ModelProvider, error)
}

// ModelFactory creates a ModelProvider for a given model name.
// Used to support provider-based model creation (e.g., any OpenAI-compatible model).
type ModelFactory func(model string) (blades.ModelProvider, error)

// ToolRegistry resolves tool names from YAML to actual Tool instances.
type ToolRegistry interface {
	Resolve(name string) (tools.Tool, error)
}

// StaticModelRegistry is a simple in-memory ModelRegistry that supports
// both exact-name registration and provider-based factories.
type StaticModelRegistry struct {
	mu        sync.RWMutex
	providers map[string]blades.ModelProvider
	factories map[string]ModelFactory
}

// NewStaticModelRegistry creates a new empty StaticModelRegistry.
func NewStaticModelRegistry() *StaticModelRegistry {
	return &StaticModelRegistry{
		providers: make(map[string]blades.ModelProvider),
		factories: make(map[string]ModelFactory),
	}
}

// Register adds a model provider under the given name.
func (r *StaticModelRegistry) Register(name string, provider blades.ModelProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[name] = provider
}

// RegisterFactory registers a model factory under the given provider name.
// When Create is called with this provider, the factory creates a ModelProvider
// using the given model name.
//
// Example:
//
//	registry.RegisterFactory("zhipu", func(model string) (blades.ModelProvider, error) {
//	    return openai.NewModel(model, openai.Config{
//	        BaseURL: "https://open.bigmodel.cn/api/paas/v4",
//	    }), nil
//	})
func (r *StaticModelRegistry) RegisterFactory(name string, factory ModelFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[name] = factory
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

// Create creates a ModelProvider using the named provider factory.
func (r *StaticModelRegistry) Create(provider, model string) (blades.ModelProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	factory, ok := r.factories[provider]
	if !ok {
		return nil, fmt.Errorf("recipe: provider %q not found in registry", provider)
	}
	return factory(model)
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
