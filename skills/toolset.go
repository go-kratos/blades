package skills

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)

const (
	// ToolListSkillsName is the tool used to inspect the current catalog.
	ToolListSkillsName = "list_skills"
	// ToolLoadSkillName is the tool used to load full instructions.
	ToolLoadSkillName = "load_skill"
	// ToolLoadSkillResourceName is the tool used to load an optional skill resource.
	ToolLoadSkillResourceName = "load_skill_resource"
)

type skillEntry struct {
	skill       Skill
	frontmatter Frontmatter
	resources   Resources
}

// Toolset exposes progressive skill discovery as regular Blades tools.
type Toolset struct {
	skills              []Skill
	skillByName         map[string]skillEntry
	toolPatternsBySkill map[string][]string
	gatedToolPatterns   []string
	instruction         string
}

// NewToolset validates skills and creates their prompt catalog and tools.
func NewToolset(skillList []Skill) (*Toolset, error) {
	toolset := &Toolset{
		skills:              make([]Skill, 0, len(skillList)),
		skillByName:         make(map[string]skillEntry, len(skillList)),
		toolPatternsBySkill: make(map[string][]string, len(skillList)),
	}
	for _, skill := range skillList {
		if skill == nil {
			continue
		}
		frontmatter, err := resolveFrontmatter(skill)
		if err != nil {
			return nil, err
		}
		if err := frontmatter.Validate(); err != nil {
			return nil, err
		}
		if _, exists := toolset.skillByName[skill.Name()]; exists {
			return nil, fmt.Errorf("skills: duplicate skill name %q", skill.Name())
		}
		toolset.skills = append(toolset.skills, skill)
		toolset.skillByName[skill.Name()] = skillEntry{
			skill:       skill,
			frontmatter: frontmatter,
			resources:   resolveResources(skill),
		}
		patterns, err := validateAllowedToolPatterns(skill.Name(), frontmatter.AllowedTools)
		if err != nil {
			return nil, err
		}
		toolset.toolPatternsBySkill[skill.Name()] = patterns
		toolset.gatedToolPatterns = append(toolset.gatedToolPatterns, patterns...)
	}
	if len(toolset.skills) == 0 {
		return toolset, nil
	}
	toolset.instruction = strings.Join([]string{
		DefaultSystemInstruction,
		FormatSkillsAsXML(toolset.skills),
	}, "\n\n")
	toolset.gatedToolPatterns = uniqueSortedPatterns(toolset.gatedToolPatterns)
	return toolset, nil
}

// Tools returns core skill tools with fresh disclosure state.
func (t *Toolset) Tools() []tools.Tool {
	return t.NewRuntime().Tools()
}

// Instruction returns the system instruction and compact skill catalog.
func (t *Toolset) Instruction() string { return t.instruction }

func resolveFrontmatter(skill Skill) (Frontmatter, error) {
	frontmatter := Frontmatter{Name: skill.Name(), Description: skill.Description()}
	provider, ok := skill.(FrontmatterProvider)
	if !ok {
		return frontmatter, nil
	}
	provided := provider.Frontmatter()
	frontmatter.License = provided.License
	frontmatter.Compatibility = provided.Compatibility
	frontmatter.AllowedTools = provided.AllowedTools
	metadata, err := normalizeMetadataMap(provided.Metadata)
	if err != nil {
		return Frontmatter{}, err
	}
	frontmatter.Metadata = metadata
	return frontmatter, nil
}

func resolveResources(skill Skill) Resources {
	provider, ok := skill.(ResourcesProvider)
	if !ok {
		return Resources{}
	}
	return provider.Resources()
}

type listSkillsTool struct{ runtime *Runtime }

func (t listSkillsTool) Spec() tools.ToolSpec {
	return tools.ToolSpec{
		Name:        ToolListSkillsName,
		Description: "List the skills available to this agent with their names and descriptions.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"query": {Type: "string", Description: "Optional case-insensitive substring used to filter skill names and descriptions."},
			},
		},
	}
}

func (t listSkillsTool) Handle(_ context.Context, input json.RawMessage) (*tools.Result, error) {
	var request struct {
		Query string `json:"query"`
	}
	if len(input) > 0 {
		if err := json.Unmarshal(input, &request); err != nil {
			return nil, newToolError("INVALID_ARGUMENTS", fmt.Sprintf("invalid tool arguments: %v", err))
		}
	}
	query := strings.ToLower(strings.TrimSpace(request.Query))
	if query == "" {
		return tools.TextResult(FormatSkillsAsXML(t.runtime.toolset.skills)), nil
	}
	matched := make([]Skill, 0, len(t.runtime.toolset.skills))
	for _, skill := range t.runtime.toolset.skills {
		if strings.Contains(strings.ToLower(skill.Name()), query) || strings.Contains(strings.ToLower(skill.Description()), query) {
			matched = append(matched, skill)
		}
	}
	return tools.TextResult(FormatSkillsAsXML(matched)), nil
}

type loadSkillTool struct{ runtime *Runtime }

func (t loadSkillTool) Spec() tools.ToolSpec {
	return tools.ToolSpec{
		Name:        ToolLoadSkillName,
		Description: "Load the complete instructions and resource index for one relevant skill.",
		InputSchema: &jsonschema.Schema{
			Type:     "object",
			Required: []string{"name"},
			Properties: map[string]*jsonschema.Schema{
				"name": {Type: "string", Description: "Name of the skill to load."},
			},
		},
	}
}

func (t loadSkillTool) Handle(_ context.Context, input json.RawMessage) (*tools.Result, error) {
	var request struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, newToolError("INVALID_ARGUMENTS", fmt.Sprintf("invalid tool arguments: %v", err))
	}
	if request.Name == "" {
		return nil, newToolError("MISSING_SKILL_NAME", "skill name is required")
	}
	entry, found := t.runtime.toolset.skillByName[request.Name]
	if !found {
		return nil, newToolError("SKILL_NOT_FOUND", fmt.Sprintf("skill %q not found", request.Name))
	}
	result, err := jsonResult(map[string]any{
		"skill_name":   entry.skill.Name(),
		"instructions": entry.skill.Instruction(),
		"frontmatter":  frontmatterMap(entry.frontmatter),
		"resources": map[string]any{
			"references": entry.resources.ListReferences(),
			"assets":     entry.resources.ListAssets(),
			"scripts":    entry.resources.ListScripts(),
		},
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type loadSkillResourceTool struct {
	runtime    *Runtime
	disclosure Disclosure
}

func (t loadSkillResourceTool) Spec() tools.ToolSpec {
	return tools.ToolSpec{
		Name:        ToolLoadSkillResourceName,
		Description: "Load a file referenced by a previously loaded skill.",
		InputSchema: &jsonschema.Schema{
			Type:     "object",
			Required: []string{"skill_name", "path"},
			Properties: map[string]*jsonschema.Schema{
				"skill_name": {Type: "string", Description: "Name of the loaded skill."},
				"path":       {Type: "string", Description: "Path under references/, assets/, or scripts/."},
			},
		},
	}
}

func (t loadSkillResourceTool) Handle(_ context.Context, input json.RawMessage) (*tools.Result, error) {
	var request struct {
		SkillName string `json:"skill_name"`
		Path      string `json:"path"`
	}
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, newToolError("INVALID_ARGUMENTS", fmt.Sprintf("invalid tool arguments: %v", err))
	}
	if request.SkillName == "" || request.Path == "" {
		return nil, newToolError("INVALID_ARGUMENTS", "skill_name and path are required")
	}
	entry, found := t.runtime.toolset.skillByName[request.SkillName]
	if !found {
		return nil, newToolError("SKILL_NOT_FOUND", fmt.Sprintf("skill %q not found", request.SkillName))
	}
	resourceType, resourceName, err := normalizeResourcePath(request.Path)
	if err != nil {
		return nil, newToolError("INVALID_RESOURCE_PATH", err.Error())
	}
	if !t.disclosure.hasLoadedSkill(request.SkillName) {
		return nil, newToolError("SKILL_NOT_LOADED", fmt.Sprintf("skill %q must be loaded before reading its resources", request.SkillName))
	}
	content, found := readResource(entry.resources, resourceType, resourceName)
	if !found {
		return nil, newToolError("RESOURCE_NOT_FOUND", fmt.Sprintf("resource %q not found in skill %q", request.Path, request.SkillName))
	}
	response := map[string]any{"skill_name": request.SkillName, "path": path.Join(resourceType, resourceName)}
	if utf8.Valid(content) {
		response["encoding"] = "utf-8"
		response["content"] = string(content)
	} else {
		response["encoding"] = "base64"
		response["content_base64"] = base64.StdEncoding.EncodeToString(content)
	}
	return jsonResult(response)
}

func frontmatterMap(frontmatter Frontmatter) map[string]any {
	result := map[string]any{"name": frontmatter.Name, "description": frontmatter.Description}
	if frontmatter.License != "" {
		result["license"] = frontmatter.License
	}
	if frontmatter.Compatibility != "" {
		result["compatibility"] = frontmatter.Compatibility
	}
	if frontmatter.AllowedTools != "" {
		result["allowed-tools"] = frontmatter.AllowedTools
	}
	if len(frontmatter.Metadata) > 0 {
		result["metadata"] = frontmatter.Metadata
	}
	return result
}

type skillToolError struct {
	code    string
	message string
}

func newToolError(code, message string) error {
	return skillToolError{code: code, message: message}
}

func (e skillToolError) Error() string {
	data, err := json.Marshal(map[string]string{"error_code": e.code, "error": e.message})
	if err != nil {
		return e.code + ": " + e.message
	}
	return string(data)
}

func jsonResult(value any) (*tools.Result, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, newToolError("INTERNAL_ERROR", fmt.Sprintf("failed to marshal tool result: %v", err))
	}
	return tools.TextResult(string(data)), nil
}

func normalizeResourcePath(resourcePath string) (string, string, error) {
	resourcePath = strings.ReplaceAll(strings.TrimSpace(resourcePath), "\\", "/")
	for _, resourceType := range []string{"references", "assets", "scripts"} {
		prefix := resourceType + "/"
		if !strings.HasPrefix(resourcePath, prefix) {
			continue
		}
		name := path.Clean(strings.TrimPrefix(resourcePath, prefix))
		if name == "." || name == ".." || strings.HasPrefix(name, "../") || path.IsAbs(name) {
			break
		}
		return resourceType, name, nil
	}
	return "", "", fmt.Errorf("path must remain under references/, assets/, or scripts/")
}

func readResource(resources Resources, resourceType, name string) ([]byte, bool) {
	switch resourceType {
	case "references":
		value, found := resources.References[name]
		return []byte(value), found
	case "assets":
		value, found := resources.Assets[name]
		return value, found
	case "scripts":
		value, found := resources.Scripts[name]
		return []byte(value), found
	default:
		return nil, false
	}
}
