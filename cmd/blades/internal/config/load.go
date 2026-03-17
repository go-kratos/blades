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
//  2. If path is empty, search ~/.blades/config.yaml
//  3. If no config file found, return empty defaults
func Load(path string) (*Config, error) {
	cfg := &Config{}

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

	expanded := ExpandEnv(string(data))
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}

	return cfg, nil
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
