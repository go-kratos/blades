package model

import (
	"fmt"
	"strings"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/cmd/blades/internal/config"
)

// Registry resolves "provider/model" strings using a list of Provider configs.
type Registry struct {
	providers []config.Provider
}

// NewRegistry creates a Registry from the providers list in config.yaml.
func NewRegistry(providers []config.Provider) *Registry {
	return &Registry{providers: providers}
}

// Resolve looks up a "provider/model" reference and returns a ModelProvider.
// If name has no "/" prefix, the first provider whose models list contains name is used.
func (r *Registry) Resolve(name string) (blades.ModelProvider, error) {
	providerName, modelName, _ := strings.Cut(name, "/")
	if modelName == "" {
		// bare model name — search all providers
		modelName = providerName
		providerName = ""
	}

	for _, p := range r.providers {
		if providerName != "" && p.Provider != providerName {
			continue
		}
		for _, m := range p.Models {
			if m == modelName {
				return NewProvider(p, modelName)
			}
		}
	}

	// If no match found but providerName is set, try constructing directly
	if providerName != "" {
		for _, p := range r.providers {
			if p.Provider == providerName {
				return NewProvider(p, modelName)
			}
		}
	}

	return nil, fmt.Errorf("model %q not found in any configured provider", name)
}
