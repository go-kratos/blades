package skills

import (
	"fmt"
	"regexp"
	"sort"
)

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Frontmatter describes metadata in SKILL.md.
type Frontmatter struct {
	Name          string
	Description   string
	License       string
	Compatibility string
	AllowedTools  string
	Metadata      map[string]string
}

// Validate validates skill frontmatter.
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

// Resources keeps skill files by relative path.
type Resources struct {
	References map[string]string
	Assets     map[string][]byte
	Scripts    map[string]string
}

func (r Resources) GetReference(path string) (string, bool) {
	v, ok := r.References[path]
	return v, ok
}

func (r Resources) GetAsset(path string) ([]byte, bool) {
	v, ok := r.Assets[path]
	return v, ok
}

func (r Resources) GetScript(path string) (string, bool) {
	v, ok := r.Scripts[path]
	return v, ok
}

func (r Resources) ListReferences() []string {
	return listKeys(r.References)
}

func (r Resources) ListAssets() []string {
	return listKeys(r.Assets)
}

func (r Resources) ListScripts() []string {
	return listKeys(r.Scripts)
}

func listKeys[T any](m map[string]T) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Skill is the minimal skill contract.
type Skill interface {
	Name() string
	Description() string
	Instruction() string
}

// FrontmatterProvider provides skill frontmatter data.
type FrontmatterProvider interface {
	Frontmatter() Frontmatter
}

// ResourcesProvider provides skill resources data.
type ResourcesProvider interface {
	Resources() Resources
}

// staticSkill is the default in-memory skill implementation.
type staticSkill struct {
	frontmatter Frontmatter
	instruction string
	resources   Resources
}

func (s *staticSkill) Name() string {
	if s == nil {
		return ""
	}
	return s.frontmatter.Name
}

func (s *staticSkill) Description() string {
	if s == nil {
		return ""
	}
	return s.frontmatter.Description
}

func (s *staticSkill) Instruction() string {
	if s == nil {
		return ""
	}
	return s.instruction
}

func (s *staticSkill) Frontmatter() Frontmatter {
	if s == nil {
		return Frontmatter{}
	}
	return s.frontmatter
}

func (s *staticSkill) Resources() Resources {
	if s == nil {
		return Resources{}
	}
	return s.resources
}
