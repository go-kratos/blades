package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/tools"
)

// Runtime owns the skill disclosure state for one agent run.
type Runtime struct {
	toolset *Toolset
	tools   []tools.Tool
}

// Disclosure is an immutable view of the tools visible at one model or tool wave.
type Disclosure struct {
	toolset *Toolset
	loaded  map[string]struct{}
}

// NewRuntime creates isolated disclosure state for one agent run.
func (t *Toolset) NewRuntime() *Runtime {
	runtime := &Runtime{toolset: t}
	if t == nil || len(t.skills) == 0 {
		return runtime
	}
	runtime.tools = []tools.Tool{
		listSkillsTool{runtime: runtime},
		loadSkillTool{runtime: runtime},
		loadSkillResourceTool{runtime: runtime},
	}
	return runtime
}

// Tools returns the core skill tools bound to this runtime.
func (r *Runtime) Tools() []tools.Tool {
	return append([]tools.Tool(nil), r.tools...)
}

// DisclosureFromMessages derives an immutable disclosure view from successful
// load_skill results in the exact model context. Stale results from a different
// Skill definition are ignored.
func (r *Runtime) DisclosureFromMessages(messages []*model.Message) Disclosure {
	disclosure := Disclosure{loaded: make(map[string]struct{})}
	if r == nil || r.toolset == nil {
		return disclosure
	}
	disclosure.toolset = r.toolset
	for _, message := range messages {
		if message == nil || message.Role != model.RoleTool {
			continue
		}
		for _, part := range message.Parts {
			result, ok := part.(content.ToolResult)
			if !ok || result.IsError || result.Name != ToolLoadSkillName {
				continue
			}
			if name, ok := r.restorableSkill(result.Parts); ok {
				disclosure.loaded[name] = struct{}{}
			}
		}
	}
	return disclosure
}

func (r *Runtime) restorableSkill(parts []content.Part) (string, bool) {
	for _, part := range parts {
		textPart, ok := part.(content.Text)
		if !ok {
			continue
		}
		var loaded struct {
			Name         string         `json:"skill_name"`
			Instructions string         `json:"instructions"`
			Frontmatter  map[string]any `json:"frontmatter"`
		}
		if err := json.Unmarshal([]byte(textPart.Text), &loaded); err != nil {
			continue
		}
		entry, found := r.toolset.skillByName[loaded.Name]
		if !found || loaded.Instructions != entry.skill.Instruction() {
			continue
		}
		allowedTools, _ := loaded.Frontmatter["allowed-tools"].(string)
		if allowedTools != entry.frontmatter.AllowedTools {
			continue
		}
		return loaded.Name, true
	}
	return "", false
}

// FilterTools removes skill-gated tools that have not been disclosed yet.
func (d Disclosure) FilterTools(allTools []tools.Tool) []tools.Tool {
	visible := make([]tools.Tool, 0, len(allTools))
	for _, tool := range allTools {
		if tool != nil && d.AllowsTool(tool.Spec().Name) {
			if resourceTool, ok := tool.(loadSkillResourceTool); ok {
				resourceTool.disclosure = d
				tool = resourceTool
			}
			visible = append(visible, tool)
		}
	}
	return visible
}

func (d Disclosure) hasLoadedSkill(name string) bool {
	_, loaded := d.loaded[name]
	return loaded
}

// AllowsTool reports whether a tool is visible in this disclosure snapshot.
func (d Disclosure) AllowsTool(name string) bool {
	if d.toolset == nil || isCoreToolName(name) {
		return true
	}
	if !matchesAllowedPattern(name, d.toolset.gatedToolPatterns) {
		return true
	}
	for skillName := range d.loaded {
		if matchesAllowedPattern(name, d.toolset.toolPatternsBySkill[skillName]) {
			return true
		}
	}
	return false
}

// FilterResolver applies the same disclosure snapshot to lazy tool resolution.
func (d Disclosure) FilterResolver(resolver tools.Resolver) tools.Resolver {
	if resolver == nil || d.toolset == nil {
		return resolver
	}
	return disclosureResolver{disclosure: d, resolver: resolver}
}

type disclosureResolver struct {
	disclosure Disclosure
	resolver   tools.Resolver
}

func (r disclosureResolver) List(ctx context.Context) ([]tools.Tool, error) {
	resolved, err := r.resolver.List(ctx)
	if err != nil {
		return nil, err
	}
	return r.disclosure.FilterTools(resolved), nil
}

func (r disclosureResolver) Resolve(ctx context.Context, name string) (tools.Tool, error) {
	if !r.disclosure.AllowsTool(name) {
		return nil, nil
	}
	return r.resolver.Resolve(ctx, name)
}

func splitAllowedToolPatterns(value string) []string {
	return strings.FieldsFunc(value, func(char rune) bool {
		return char == ',' || unicode.IsSpace(char)
	})
}

func validateAllowedToolPatterns(skillName, value string) ([]string, error) {
	patterns := splitAllowedToolPatterns(value)
	for _, pattern := range patterns {
		if _, err := path.Match(pattern, "tool-name"); err != nil {
			return nil, fmt.Errorf("skills: invalid allowed-tools pattern %q in skill %q: %w", pattern, skillName, err)
		}
	}
	return patterns, nil
}

func matchesAllowedPattern(toolName string, patterns []string) bool {
	for _, pattern := range patterns {
		matched, err := path.Match(pattern, toolName)
		if err == nil && matched {
			return true
		}
	}
	return false
}

func uniqueSortedPatterns(patterns []string) []string {
	seen := make(map[string]struct{}, len(patterns))
	result := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		if _, exists := seen[pattern]; exists {
			continue
		}
		seen[pattern] = struct{}{}
		result = append(result, pattern)
	}
	sort.Strings(result)
	return result
}

func isCoreToolName(name string) bool {
	switch name {
	case ToolListSkillsName, ToolLoadSkillName, ToolLoadSkillResourceName:
		return true
	default:
		return false
	}
}
