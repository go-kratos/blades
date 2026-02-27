package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)

const (
	ToolListSkillsName        = "list_skills"
	ToolLoadSkillName         = "load_skill"
	ToolLoadSkillResourceName = "load_skill_resource"
)

// Toolset provides tools and instructions for loaded skills.
type Toolset struct {
	skills      []*Skill
	skillByName map[string]*Skill
	tools       []tools.Tool
}

// NewToolset creates a new skill toolset.
func NewToolset(skills []*Skill) (*Toolset, error) {
	ts := &Toolset{
		skills:      make([]*Skill, 0, len(skills)),
		skillByName: make(map[string]*Skill, len(skills)),
	}
	for _, skill := range skills {
		if skill == nil {
			continue
		}
		if err := skill.Frontmatter.Validate(); err != nil {
			return nil, err
		}
		if _, exists := ts.skillByName[skill.Name()]; exists {
			return nil, fmt.Errorf("skills: duplicate skill name %q", skill.Name())
		}
		ts.skillByName[skill.Name()] = skill
		ts.skills = append(ts.skills, skill)
	}
	ts.tools = []tools.Tool{
		&listSkillsTool{toolset: ts},
		&loadSkillTool{toolset: ts},
		&loadSkillResourceTool{toolset: ts},
	}
	return ts, nil
}

// Tools returns skill tools.
func (t *Toolset) Tools() []tools.Tool {
	out := make([]tools.Tool, 0, len(t.tools))
	out = append(out, t.tools...)
	return out
}

// Instruction returns the instruction block for skills.
func (t *Toolset) Instruction() string {
	return strings.Join([]string{
		DefaultSystemInstruction,
		FormatSkillsAsXML(t.skills),
	}, "\n\n")
}

func skillNotFound(name string) string {
	return mustJSON(map[string]any{
		"error":      fmt.Sprintf("Skill %q not found.", name),
		"error_code": "SKILL_NOT_FOUND",
	})
}

func invalidArgs(msg string) string {
	return mustJSON(map[string]any{
		"error":      msg,
		"error_code": "INVALID_ARGUMENTS",
	})
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"error":"failed to marshal response","error_code":"INTERNAL_ERROR"}`
	}
	return string(b)
}

func toFrontmatterMap(f Frontmatter) map[string]any {
	out := map[string]any{
		"name":        f.Name,
		"description": f.Description,
	}
	if f.License != "" {
		out["license"] = f.License
	}
	if f.Compatibility != "" {
		out["compatibility"] = f.Compatibility
	}
	if f.AllowedTools != "" {
		out["allowed-tools"] = f.AllowedTools
	}
	if len(f.Metadata) > 0 {
		out["metadata"] = f.Metadata
	}
	return out
}

type listSkillsTool struct {
	toolset *Toolset
}

func (t *listSkillsTool) Name() string { return ToolListSkillsName }

func (t *listSkillsTool) Description() string {
	return "Lists all available skills with their names and descriptions."
}

func (t *listSkillsTool) InputSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:       "object",
		Properties: map[string]*jsonschema.Schema{},
	}
}

func (t *listSkillsTool) OutputSchema() *jsonschema.Schema { return nil }

func (t *listSkillsTool) Handle(ctx context.Context, input string) (string, error) {
	return FormatSkillsAsXML(t.toolset.skills), nil
}

type loadSkillTool struct {
	toolset *Toolset
}

func (t *loadSkillTool) Name() string { return ToolLoadSkillName }

func (t *loadSkillTool) Description() string {
	return "Loads the SKILL.md instructions for a given skill."
}

func (t *loadSkillTool) InputSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:     "object",
		Required: []string{"name"},
		Properties: map[string]*jsonschema.Schema{
			"name": {
				Type:        "string",
				Description: "The name of the skill to load.",
			},
		},
	}
}

func (t *loadSkillTool) OutputSchema() *jsonschema.Schema { return nil }

func (t *loadSkillTool) Handle(ctx context.Context, input string) (string, error) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return invalidArgs(fmt.Sprintf("Invalid tool arguments: %v", err)), nil
	}
	if req.Name == "" {
		return mustJSON(map[string]any{
			"error":      "Skill name is required.",
			"error_code": "MISSING_SKILL_NAME",
		}), nil
	}
	skill, ok := t.toolset.skillByName[req.Name]
	if !ok {
		return skillNotFound(req.Name), nil
	}
	return mustJSON(map[string]any{
		"skill_name":   skill.Name(),
		"instructions": skill.Instructions,
		"frontmatter":  toFrontmatterMap(skill.Frontmatter),
	}), nil
}

type loadSkillResourceTool struct {
	toolset *Toolset
}

func (t *loadSkillResourceTool) Name() string { return ToolLoadSkillResourceName }

func (t *loadSkillResourceTool) Description() string {
	return "Loads a resource file from references/, assets/, or scripts/ in a skill."
}

func (t *loadSkillResourceTool) InputSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:     "object",
		Required: []string{"skill_name", "path"},
		Properties: map[string]*jsonschema.Schema{
			"skill_name": {
				Type:        "string",
				Description: "The name of the skill.",
			},
			"path": {
				Type:        "string",
				Description: "Resource path under references/, assets/, or scripts/.",
			},
		},
	}
}

func (t *loadSkillResourceTool) OutputSchema() *jsonschema.Schema { return nil }

func (t *loadSkillResourceTool) Handle(ctx context.Context, input string) (string, error) {
	var req struct {
		SkillName string `json:"skill_name"`
		Path      string `json:"path"`
	}
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return invalidArgs(fmt.Sprintf("Invalid tool arguments: %v", err)), nil
	}
	if req.SkillName == "" {
		return mustJSON(map[string]any{
			"error":      "Skill name is required.",
			"error_code": "MISSING_SKILL_NAME",
		}), nil
	}
	if req.Path == "" {
		return mustJSON(map[string]any{
			"error":      "Resource path is required.",
			"error_code": "MISSING_RESOURCE_PATH",
		}), nil
	}
	skill, ok := t.toolset.skillByName[req.SkillName]
	if !ok {
		return skillNotFound(req.SkillName), nil
	}
	var (
		content string
		found   bool
	)
	switch {
	case strings.HasPrefix(req.Path, "references/"):
		content, found = skill.Resources.GetReference(strings.TrimPrefix(req.Path, "references/"))
	case strings.HasPrefix(req.Path, "assets/"):
		content, found = skill.Resources.GetAsset(strings.TrimPrefix(req.Path, "assets/"))
	case strings.HasPrefix(req.Path, "scripts/"):
		content, found = skill.Resources.GetScript(strings.TrimPrefix(req.Path, "scripts/"))
	default:
		return mustJSON(map[string]any{
			"error":      "Path must start with 'references/', 'assets/', or 'scripts/'.",
			"error_code": "INVALID_RESOURCE_PATH",
		}), nil
	}
	if !found {
		return mustJSON(map[string]any{
			"error":      fmt.Sprintf("Resource %q not found in skill %q.", req.Path, req.SkillName),
			"error_code": "RESOURCE_NOT_FOUND",
		}), nil
	}
	return mustJSON(map[string]any{
		"skill_name": req.SkillName,
		"path":       req.Path,
		"content":    content,
	}), nil
}
