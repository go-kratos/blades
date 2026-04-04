package model

import (
	"strings"
	"testing"

	"github.com/go-kratos/blades/cmd/blades/internal/config"
)

func TestRegistryResolveAdditionalPaths(t *testing.T) {
	t.Parallel()

	reg := NewRegistry([]config.Provider{
		{
			Name:     "primary",
			Provider: "openai",
			Models:   []string{"gpt-4o"},
			APIKey:   "test-key",
		},
		{
			Name:     "backup",
			Provider: "anthropic",
			Models:   []string{"claude-sonnet-4-6"},
			APIKey:   "test-key",
		},
	})

	if _, err := reg.Resolve("gpt-4o"); err != nil {
		t.Fatalf("Resolve bare model: %v", err)
	}
	if _, err := reg.Resolve("backup/missing-model"); err != nil {
		t.Fatalf("Resolve direct name fallback: %v", err)
	}
	if _, err := reg.Resolve("missing/model"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}
	if !matchesProviderName(config.Provider{Name: "primary", Provider: "openai"}, "primary") {
		t.Fatal("expected provider name to match")
	}
	if matchesProviderName(config.Provider{Name: "primary", Provider: "openai"}, "openai") {
		t.Fatal("expected provider type not to match")
	}
}

func TestNewProviderBranches(t *testing.T) {
	t.Parallel()

	for _, provider := range []config.Provider{
		{Provider: "openai", APIKey: "key"},
		{Provider: "anthropic", APIKey: "key"},
		{Provider: "gemini", APIKey: "key"},
	} {
		if _, err := NewProvider(provider, "test-model"); err != nil {
			t.Fatalf("NewProvider(%s): %v", provider.Provider, err)
		}
	}

	if _, err := NewProvider(config.Provider{Provider: "unknown"}, "test-model"); err == nil {
		t.Fatal("expected unsupported provider error")
	}
}
