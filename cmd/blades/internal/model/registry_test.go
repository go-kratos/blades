package model

import (
	"testing"

	"github.com/go-kratos/blades/cmd/blades/internal/config"
)

func TestRegistryResolveByProviderName(t *testing.T) {
	t.Parallel()

	reg := NewRegistry([]config.Provider{
		{
			Name:     "primary",
			Provider: "openai",
			Models:   []string{"gpt-4o"},
			APIKey:   "test-key",
		},
	})

	if _, err := reg.Resolve("primary/gpt-4o"); err != nil {
		t.Fatalf("Resolve by provider name: %v", err)
	}
}

func TestRegistryRejectsProviderTypePrefix(t *testing.T) {
	t.Parallel()

	reg := NewRegistry([]config.Provider{
		{
			Name:     "primary",
			Provider: "openai",
			Models:   []string{"gpt-4o"},
			APIKey:   "test-key",
		},
	})

	if _, err := reg.Resolve("openai/gpt-4o"); err == nil {
		t.Fatal("expected provider type prefix to be rejected")
	}
}
