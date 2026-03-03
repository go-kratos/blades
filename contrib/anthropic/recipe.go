package anthropic

import (
	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/recipe"
)

// RegisterProvider registers an "anthropic" provider factory in the model registry.
// Once registered, any recipe YAML with `provider: anthropic` can use arbitrary
// model names (e.g., claude-sonnet-4-5-20250514) and they will be created
// using the given Config.
//
// Example:
//
//	registry := recipe.NewStaticModelRegistry()
//	anthropic.RegisterProvider(registry, anthropic.Config{
//	    APIKey: os.Getenv("ANTHROPIC_API_KEY"),
//	})
func RegisterProvider(registry *recipe.StaticModelRegistry, config Config) {
	registry.RegisterFactory("anthropic", func(model string) (blades.ModelProvider, error) {
		return NewModel(model, config), nil
	})
}
