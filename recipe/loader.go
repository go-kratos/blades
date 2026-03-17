package recipe

import (
	"fmt"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadFromFile loads and parses a AgentSpec from a YAML file path.
func LoadFromFile(path string) (*AgentSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("recipe: failed to read file %q: %w", path, err)
	}
	return Parse(data)
}

// LoadFromFS loads and parses a AgentSpec from an fs.FS (e.g., embed.FS).
func LoadFromFS(fsys fs.FS, path string) (*AgentSpec, error) {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, fmt.Errorf("recipe: failed to read %q from fs: %w", path, err)
	}
	return Parse(data)
}

// Parse parses raw YAML bytes into a AgentSpec and validates it.
func Parse(data []byte) (*AgentSpec, error) {
	var spec AgentSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("recipe: failed to parse YAML: %w", err)
	}
	if err := Validate(&spec); err != nil {
		return nil, err
	}
	return &spec, nil
}
