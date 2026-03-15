// Package channel defines the Channel interface and a registry for daemon channels.
package channel

import (
	"context"
	"fmt"
	"sync"
)

// Factory builds a Channel from an opaque config. Callers type-assert to *config.Config as needed.
type Factory func(cfg interface{}) (Channel, error)

// Registry holds named channel factories for daemon mode.
type Registry struct {
	mu   sync.RWMutex
	fns  map[string]Factory
}

// NewRegistry returns an empty channel registry.
func NewRegistry() *Registry {
	return &Registry{fns: make(map[string]Factory)}
}

// Register adds a factory for the given name (e.g. "lark"). Panics if name is empty or already registered.
func (r *Registry) Register(name string, f Factory) {
	if name == "" {
		panic("channel: empty name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.fns[name]; ok {
		panic("channel: already registered " + name)
	}
	r.fns[name] = f
}

// Build builds the channel for name with cfg. Caller can type-assert to SessionNotifier and then Start in a goroutine.
func (r *Registry) Build(name string, cfg interface{}) (Channel, error) {
	r.mu.RLock()
	f, ok := r.fns[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("channel: unknown %q", name)
	}
	return f(cfg)
}

// Start builds the channel for name with cfg and runs Start(ctx, handler). Blocks until the channel exits.
func (r *Registry) Start(ctx context.Context, name string, cfg interface{}, handler StreamHandler) error {
	ch, err := r.Build(name, cfg)
	if err != nil {
		return err
	}
	return ch.Start(ctx, handler)
}

// Names returns all registered channel names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.fns))
	for n := range r.fns {
		names = append(names, n)
	}
	return names
}
