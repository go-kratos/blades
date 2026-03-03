package openai

import (
	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/recipe"
)

// RegisterProvider registers an "openai" provider factory in the model registry.
// Once registered, any recipe YAML with `provider: openai` can use arbitrary
// model names (e.g., gpt-4o, glm-5, deepseek-r2) and they will be created
// using the given Config.
//
// Example:
//
//	registry := recipe.NewStaticModelRegistry()
//	openai.RegisterProvider(registry, openai.Config{
//	    BaseURL: "https://open.bigmodel.cn/api/paas/v4",
//	})
func RegisterProvider(registry *recipe.StaticModelRegistry, config Config) {
	registry.RegisterFactory("openai", func(model string) (blades.ModelProvider, error) {
		return NewModel(model, config), nil
	})
}
