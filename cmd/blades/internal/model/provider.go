// Package model provides a factory for blades model providers.
package model

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades"
	bldanthropic "github.com/go-kratos/blades/contrib/anthropic"
	bldgemini "github.com/go-kratos/blades/contrib/gemini"
	bldopenai "github.com/go-kratos/blades/contrib/openai"
	"google.golang.org/genai"

	"github.com/go-kratos/blades/cmd/blades/internal/config"
)

// NewProvider creates a ModelProvider from a Provider config entry and a model name.
func NewProvider(p config.Provider, model string) (blades.ModelProvider, error) {
	switch p.Provider {
	case "anthropic":
		c := bldanthropic.Config{APIKey: p.APIKey, CacheControl: true}
		if p.BaseURL != "" {
			c.BaseURL = p.BaseURL
		}
		return bldanthropic.NewModel(model, c), nil

	case "openai":
		c := bldopenai.Config{APIKey: p.APIKey}
		if p.BaseURL != "" {
			c.BaseURL = p.BaseURL
		}
		return bldopenai.NewModel(model, c), nil

	case "gemini":
		c := bldgemini.Config{}
		if p.APIKey != "" {
			c.ClientConfig.APIKey = p.APIKey
		}
		c.ClientConfig.Backend = genai.BackendGeminiAPI
		return bldgemini.NewModel(context.Background(), model, c)

	default:
		return nil, fmt.Errorf("unsupported provider: %q (want anthropic|openai|gemini)", p.Provider)
	}
}
