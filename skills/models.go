// Package skills provides progressive disclosure for reusable agent instructions.
package skills

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
)

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Frontmatter describes the metadata declared by a SKILL.md file.
type Frontmatter struct {
	Name          string
	Description   string
	License       string
	Compatibility string
	AllowedTools  string
	Metadata      map[string]any
}

// Validate validates frontmatter according to the Agent Skills conventions.
func (f Frontmatter) Validate() error {
	if len(f.Name) > 64 {
		return fmt.Errorf("skills: name must be at most 64 characters")
	}
	if !skillNamePattern.MatchString(f.Name) {
		return fmt.Errorf("skills: name must be lowercase kebab-case")
	}
	if f.Description == "" {
		return fmt.Errorf("skills: description must not be empty")
	}
	if len(f.Description) > 1024 {
		return fmt.Errorf("skills: description must be at most 1024 characters")
	}
	if len(f.Compatibility) > 500 {
		return fmt.Errorf("skills: compatibility must be at most 500 characters")
	}
	return nil
}

// Resources contains optional files bundled with a skill.
type Resources struct {
	References map[string]string
	Assets     map[string][]byte
	Scripts    map[string]string
}

// ListReferences returns sorted reference paths.
func (r Resources) ListReferences() []string { return sortedKeys(r.References) }

// ListAssets returns sorted asset paths.
func (r Resources) ListAssets() []string { return sortedKeys(r.Assets) }

// ListScripts returns sorted script paths.
func (r Resources) ListScripts() []string { return sortedKeys(r.Scripts) }

func sortedKeys[T any](items map[string]T) []string {
	if len(items) == 0 {
		return nil
	}
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Skill is the minimal contract required by the agent runtime.
type Skill interface {
	Name() string
	Description() string
	Instruction() string
}

// FrontmatterProvider exposes complete frontmatter for a skill.
type FrontmatterProvider interface {
	Frontmatter() Frontmatter
}

// ResourcesProvider exposes optional resources for a skill.
type ResourcesProvider interface {
	Resources() Resources
}

// Definition is an in-memory Skill implementation suitable for registries and databases.
type Definition struct {
	Meta         Frontmatter
	Instructions string
	Files        Resources
}

// New creates and validates an in-memory skill.
func New(frontmatter Frontmatter, instruction string, resources Resources) (Skill, error) {
	skill := Definition{Meta: frontmatter, Instructions: instruction, Files: resources}
	if err := skill.Meta.Validate(); err != nil {
		return nil, err
	}
	metadata, err := normalizeMetadataMap(skill.Meta.Metadata)
	if err != nil {
		return nil, err
	}
	skill.Meta.Metadata = metadata
	return skill, nil
}

// Name returns the stable skill name.
func (d Definition) Name() string { return d.Meta.Name }

// Description returns the short catalog description.
func (d Definition) Description() string { return d.Meta.Description }

// Instruction returns the full skill instructions.
func (d Definition) Instruction() string { return d.Instructions }

// Frontmatter returns the complete skill metadata.
func (d Definition) Frontmatter() Frontmatter { return d.Meta }

// Resources returns files bundled with the skill.
func (d Definition) Resources() Resources { return d.Files }

func normalizeMetadataMap(value any) (map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("skills: metadata must be JSON-compatible: %w", err)
	}
	var normalized map[string]any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return nil, fmt.Errorf("skills: metadata must be a map: %w", err)
	}
	return normalized, nil
}
