package recipe

import (
	"fmt"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadFromFile loads and parses a RecipeSpec from a YAML file path.
func LoadFromFile(path string) (*RecipeSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("recipe: failed to read file %q: %w", path, err)
	}
	return Parse(data)
}

// LoadFromFS loads and parses a RecipeSpec from an fs.FS (e.g., embed.FS).
func LoadFromFS(fsys fs.FS, path string) (*RecipeSpec, error) {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, fmt.Errorf("recipe: failed to read %q from fs: %w", path, err)
	}
	return Parse(data)
}

// Parse parses raw YAML bytes into a RecipeSpec and validates it.
func Parse(data []byte) (*RecipeSpec, error) {
	var spec RecipeSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("recipe: failed to parse YAML: %w", err)
	}
	if err := Validate(&spec); err != nil {
		return nil, err
	}
	return &spec, nil
}

// kindProbe is a minimal struct used only to detect the "kind" field in YAML.
type kindProbe struct {
	Kind string `yaml:"kind"`
}

// ParseAny parses raw YAML bytes, routing to AgentSpec or RecipeSpec based on the "kind" field.
// If kind == "AgentSpec", it returns (*AgentSpec, nil, nil).
// Otherwise, it returns (nil, *RecipeSpec, nil).
func ParseAny(data []byte) (*AgentSpec, *RecipeSpec, error) {
	var probe kindProbe
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return nil, nil, fmt.Errorf("recipe: failed to probe YAML kind: %w", err)
	}
	if probe.Kind == "AgentSpec" {
		spec, err := ParseAgentSpec(data)
		if err != nil {
			return nil, nil, err
		}
		return spec, nil, nil
	}
	spec, err := Parse(data)
	if err != nil {
		return nil, nil, err
	}
	return nil, spec, nil
}

// ParseAgentSpec parses raw YAML bytes into an AgentSpec and validates it.
func ParseAgentSpec(data []byte) (*AgentSpec, error) {
	var spec AgentSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("recipe: failed to parse AgentSpec YAML: %w", err)
	}
	if err := validateAgentSpec(&spec); err != nil {
		return nil, err
	}
	return &spec, nil
}

// LoadAgentSpecFromFile loads and parses an AgentSpec from a YAML file path.
func LoadAgentSpecFromFile(path string) (*AgentSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("recipe: failed to read file %q: %w", path, err)
	}
	return ParseAgentSpec(data)
}

// LoadAgentSpecFromFS loads and parses an AgentSpec from an fs.FS (e.g., embed.FS).
func LoadAgentSpecFromFS(fsys fs.FS, path string) (*AgentSpec, error) {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, fmt.Errorf("recipe: failed to read %q from fs: %w", path, err)
	}
	return ParseAgentSpec(data)
}
