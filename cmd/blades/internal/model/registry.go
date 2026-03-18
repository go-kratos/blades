package model

import (
	"fmt"
	"strings"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/cmd/blades/internal/config"
)

// Registry resolves "name/model" strings using a list of Provider configs.
type Registry struct {
	providers []config.Provider
}

// NewRegistry creates a Registry from the providers list in config.yaml.
func NewRegistry(providers []config.Provider) *Registry {
	return &Registry{providers: providers}
}

// Resolve looks up a "name/model" reference and returns a ModelProvider.
// If name has no "/" prefix, the first provider whose models list contains it is used.
// When a prefix is provided, it must match config.providers[].name.
func (r *Registry) Resolve(name string) (blades.ModelProvider, error) {
	providerRef, modelName, _ := strings.Cut(name, "/")
	if modelName == "" {
		// bare model name — search all providers
		modelName = providerRef
		providerRef = ""
	}

	for _, p := range r.providers {
		if providerRef != "" && !matchesProviderName(p, providerRef) {
			continue
		}
		for _, m := range p.Models {
			if m == modelName {
				return NewProvider(p, modelName)
			}
		}
	}

	// If no match found but a provider name is set, try constructing directly.
	// This allows name/model even when models
	// is omitted or incomplete in config.yaml.
	if providerRef != "" {
		for _, p := range r.providers {
			if matchesProviderName(p, providerRef) {
				return NewProvider(p, modelName)
			}
		}
	}

	return nil, fmt.Errorf("model %q not found in any configured provider", name)
}

func matchesProviderName(p config.Provider, ref string) bool {
	return p.Name == ref
}
