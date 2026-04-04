package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadNormalizesProviderName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("providers:\n  - provider: openai\n    models: [gpt-4o]\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.Providers[0].Name, "openai"; got != want {
		t.Fatalf("provider name = %q, want %q", got, want)
	}
}

func TestLoadRejectsDuplicateProviderNames(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := "providers:\n" +
		"  - name: primary\n    provider: openai\n    models: [gpt-4o]\n" +
		"  - name: primary\n    provider: anthropic\n    models: [claude-sonnet-4-6]\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("expected duplicate provider name error")
	}
}

func TestLoadRejectsUnsupportedProvider(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("providers:\n  - provider: unknown\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("expected unsupported provider error")
	}
}

func TestLoadDefaultsExecTimeoutAndExpandTilde(t *testing.T) {
	t.Parallel()

	oldHome := os.Getenv("HOME")
	newHome := t.TempDir()
	_ = os.Setenv("HOME", newHome)
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load empty path: %v", err)
	}
	if got, want := cfg.Exec.ExecTimeout(), 60*time.Second; got != want {
		t.Fatalf("ExecTimeout = %s, want %s", got, want)
	}

	if got := ExpandTilde("~/agent"); !strings.HasPrefix(got, newHome) {
		t.Fatalf("ExpandTilde = %q, want prefix %q", got, newHome)
	}
}

func TestLoadUsesDefaultConfigPathFromHome(t *testing.T) {
	t.Parallel()

	oldHome := os.Getenv("HOME")
	newHome := t.TempDir()
	_ = os.Setenv("HOME", newHome)
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})

	cfgDir := filepath.Join(newHome, ".blades")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("providers:\n  - provider: openai\n    models: [gpt-4o]\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load default path: %v", err)
	}
	if len(cfg.Providers) != 1 || cfg.Providers[0].Provider != "openai" {
		t.Fatalf("loaded providers = %+v", cfg.Providers)
	}
}
