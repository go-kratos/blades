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

// NewProvider creates a ModelProvider from the LLM configuration.
func NewProvider(cfg config.LLMConfig) (blades.ModelProvider, error) {
	switch cfg.Provider {
	case "anthropic":
		c := bldanthropic.Config{APIKey: cfg.APIKey}
		if cfg.BaseURL != "" {
			c.BaseURL = cfg.BaseURL
		}
		return bldanthropic.NewModel(cfg.Model, c), nil

	case "openai":
		c := bldopenai.Config{APIKey: cfg.APIKey}
		if cfg.BaseURL != "" {
			c.BaseURL = cfg.BaseURL
		}
		return bldopenai.NewModel(cfg.Model, c), nil

	case "gemini":
		c := bldgemini.Config{}
		if cfg.APIKey != "" {
			c.ClientConfig.APIKey = cfg.APIKey
		}
		c.ClientConfig.Backend = genai.BackendGeminiAPI
		return bldgemini.NewModel(context.Background(), cfg.Model, c)

	default:
		return nil, fmt.Errorf("unsupported provider: %q (want anthropic|openai|gemini)", cfg.Provider)
	}
}
