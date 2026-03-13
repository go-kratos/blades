package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Load reads a YAML config file, expanding ${ENV_VAR} references.
// If path is empty, it searches for config.yaml in the workspace dir
// (~/.blades/config.yaml), then returns defaults.
func Load(path string) (*Config, error) {
	cfg := defaultConfig()

	if path == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			candidate := filepath.Join(home, ".blades", "config.yaml")
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

	expanded := os.ExpandEnv(string(data))
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}

	applyDefaults(cfg)
	return cfg, nil
}

func defaultConfig() *Config {
	home, _ := os.UserHomeDir()
	ws := filepath.Join(home, ".blades")
	return &Config{
		Workspace: ws,
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
	if cfg.Workspace == "" {
		home, _ := os.UserHomeDir()
		cfg.Workspace = filepath.Join(home, ".blades")
	}
}
