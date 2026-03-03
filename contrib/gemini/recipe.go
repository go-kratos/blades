package gemini

import (
	"context"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/recipe"
)

// RegisterProvider registers a "gemini" provider factory in the model registry.
// Once registered, any recipe YAML with `provider: gemini` can use arbitrary
// model names (e.g., gemini-2.5-pro) and they will be created using the given
// Config. The provided context is used for Gemini client initialization.
//
// Example:
//
//	registry := recipe.NewStaticModelRegistry()
//	gemini.RegisterProvider(ctx, registry, gemini.Config{})
func RegisterProvider(ctx context.Context, registry *recipe.StaticModelRegistry, config Config) {
	registry.RegisterFactory("gemini", func(model string) (blades.ModelProvider, error) {
		return NewModel(ctx, model, config)
	})
}
