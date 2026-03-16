package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Load reads a YAML config file, expanding ${ENV_VAR} references.
//
// Resolution order:
//  1. If path is provided, use it directly
//  2. If path is empty, search ~/.blades/agent.yaml
//  3. If no config file found, return defaults
//
// Default values (when config file missing or fields empty):
//   - provider: anthropic
//   - model: claude-sonnet-4-6
//   - maxIterations: 10
//   - compressThreshold: 40000
func Load(path string) (*Config, error) {
	cfg := defaultConfig()

	if path == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			candidate := filepath.Join(home, ".blades", "agent.yaml")
			if _, err2 := os.Stat(candidate); err2 == nil {
				path = candidate
			}
		}
	}

	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	expanded := ExpandEnv(string(data))
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}

	applyDefaults(cfg)
	return cfg, nil
}

func defaultConfig() *Config {
	return &Config{
		LLM: LLMConfig{
			Provider: "anthropic",
			Model:    "claude-sonnet-4-6",
		},
		Defaults: DefaultsConfig{
			MaxIterations:     10,
			CompressThreshold: 40000,
		},
	}
}

// applyDefaults fills zero values after YAML parsing.
func applyDefaults(cfg *Config) {
	if cfg.LLM.Provider == "" {
		cfg.LLM.Provider = "anthropic"
	}
	if cfg.LLM.Model == "" {
		cfg.LLM.Model = "claude-sonnet-4-6"
	}
	if cfg.Defaults.MaxIterations == 0 {
		cfg.Defaults.MaxIterations = 10
	}
	if cfg.Defaults.CompressThreshold == 0 {
		cfg.Defaults.CompressThreshold = 40000
	}
}

// ExpandTilde expands ~ to the user's home directory.
func ExpandTilde(path string) string {
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		if path == "~" {
			return home
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
