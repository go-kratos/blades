package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)

const (
	ToolListSkillsName        = "list_skills"
	ToolLoadSkillName         = "load_skill"
	ToolLoadSkillResourceName = "load_skill_resource"
	ToolRunSkillScriptName    = "run_skill_script"

	defaultScriptTimeoutSeconds = 300
	maxScriptTimeoutSeconds     = 1800
)

var coreSkillToolNames = map[string]struct{}{
	ToolListSkillsName:        {},
	ToolLoadSkillName:         {},
	ToolLoadSkillResourceName: {},
	ToolRunSkillScriptName:    {},
}

func isCoreToolName(name string) bool {
	_, ok := coreSkillToolNames[name]
	return ok
}

type skillEntry struct {
	skill       Skill
	frontmatter Frontmatter
	resources   Resources
}

// Toolset provides tools and instructions for loaded skills.
type Toolset struct {
	skills              []Skill
	skillByName         map[string]skillEntry
	tools               []tools.Tool
	allowedToolPatterns []string
	instruction         string
}

// NewToolset creates a new skill toolset.
func NewToolset(skills []Skill) (*Toolset, error) {
	ts := &Toolset{
		skills:      make([]Skill, 0, len(skills)),
		skillByName: make(map[string]skillEntry, len(skills)),
	}
	for _, skill := range skills {
		if skill == nil {
			continue
		}
		frontmatter := resolveFrontmatter(skill)
		if err := frontmatter.Validate(); err != nil {
			return nil, err
		}
		if _, exists := ts.skillByName[skill.Name()]; exists {
			return nil, fmt.Errorf("skills: duplicate skill name %q", skill.Name())
		}
		ts.skillByName[skill.Name()] = skillEntry{
			skill:       skill,
			frontmatter: frontmatter,
			resources:   resolveResources(skill),
		}
		ts.skills = append(ts.skills, skill)
	}
	allowedToolPatternSet := make(map[string]struct{})
	for _, entry := range ts.skillByName {
		for _, pattern := range splitAllowedToolPatterns(entry.frontmatter.AllowedTools) {
			if _, err := path.Match(pattern, "tool-name"); err != nil {
				return nil, fmt.Errorf("skills: invalid allowed-tools pattern %q in skill %q: %w", pattern, entry.skill.Name(), err)
			}
			if _, exists := allowedToolPatternSet[pattern]; exists {
				continue
			}
			allowedToolPatternSet[pattern] = struct{}{}
			ts.allowedToolPatterns = append(ts.allowedToolPatterns, pattern)
		}
	}
	sort.Strings(ts.allowedToolPatterns)
	ts.instruction = strings.Join([]string{
		DefaultSystemInstruction,
		FormatSkillsAsXML(ts.skills),
	}, "\n\n")
	ts.tools = []tools.Tool{
		&listSkillsTool{toolset: ts},
		&loadSkillTool{toolset: ts},
		&loadSkillResourceTool{toolset: ts},
		&runSkillScriptTool{toolset: ts},
	}
	return ts, nil
}

func resolveFrontmatter(skill Skill) Frontmatter {
	f := Frontmatter{
		Name:        skill.Name(),
		Description: skill.Description(),
	}
	provider, ok := skill.(FrontmatterProvider)
	if !ok {
		return f
	}
	frontmatter := provider.Frontmatter()
	f.License = frontmatter.License
	f.Compatibility = frontmatter.Compatibility
	f.AllowedTools = frontmatter.AllowedTools
	if len(frontmatter.Metadata) > 0 {
		f.Metadata = make(map[string]string, len(frontmatter.Metadata))
		for key, value := range frontmatter.Metadata {
			f.Metadata[key] = value
		}
	}
	return f
}

func resolveResources(skill Skill) Resources {
	provider, ok := skill.(ResourcesProvider)
	if !ok {
		return Resources{}
	}
	return provider.Resources()
}

// Tools returns skill tools.
func (t *Toolset) Tools() []tools.Tool {
	out := make([]tools.Tool, 0, len(t.tools))
	out = append(out, t.tools...)
	return out
}

// ComposeTools merges base tools with skill tools and applies allowed-tools filtering.
func (t *Toolset) ComposeTools(base []tools.Tool) []tools.Tool {
	out := make([]tools.Tool, 0, len(base)+len(t.tools))
	out = append(out, base...)
	out = append(out, t.tools...)
	if len(t.allowedToolPatterns) == 0 {
		return out
	}
	filtered := make([]tools.Tool, 0, len(out))
	for _, tool := range out {
		name := tool.Name()
		if isCoreToolName(name) || matchesAllowedPattern(name, t.allowedToolPatterns) {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

func matchesAllowedPattern(toolName string, patterns []string) bool {
	for _, pattern := range patterns {
		match, err := path.Match(pattern, toolName)
		if err != nil {
			continue
		}
		if match {
			return true
		}
	}
	return false
}

// Instruction returns the instruction block for skills.
func (t *Toolset) Instruction() string {
	return t.instruction
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
		"skill_name":   skill.skill.Name(),
		"instructions": skill.skill.Instruction(),
		"frontmatter":  toFrontmatterMap(skill.frontmatter),
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
	resourceType, resourceName, err := normalizeResourcePath(req.Path)
	if err != nil {
		return mustJSON(map[string]any{
			"error":      "Path must start with 'references/', 'assets/', or 'scripts/' and remain within that directory.",
			"error_code": "INVALID_RESOURCE_PATH",
		}), nil
	}
	var (
		content string
		found   bool
	)
	resources := skill.resources
	switch resourceType {
	case "references":
		content, found = resources.GetReference(resourceName)
	case "assets":
		content, found = resources.GetAsset(resourceName)
	case "scripts":
		content, found = resources.GetScript(resourceName)
	default:
		return invalidArgs("Invalid resource type"), nil
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

type runSkillScriptTool struct {
	toolset *Toolset
}

func (t *runSkillScriptTool) Name() string { return ToolRunSkillScriptName }

func (t *runSkillScriptTool) Description() string {
	return "Executes a script from scripts/ in a skill."
}

func (t *runSkillScriptTool) InputSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:     "object",
		Required: []string{"skill_name", "script_path"},
		Properties: map[string]*jsonschema.Schema{
			"skill_name": {
				Type:        "string",
				Description: "The name of the skill.",
			},
			"script_path": {
				Type:        "string",
				Description: "Script path under scripts/.",
			},
			"args": {
				Type:        "array",
				Description: "Optional script args.",
				Items:       &jsonschema.Schema{Type: "string"},
			},
			"env": {
				Type:        "object",
				Description: "Optional environment variables.",
			},
			"timeout_seconds": {
				Type:        "integer",
				Description: fmt.Sprintf("Optional timeout in seconds. Default: %d, max: %d.", defaultScriptTimeoutSeconds, maxScriptTimeoutSeconds),
			},
		},
	}
}

func (t *runSkillScriptTool) OutputSchema() *jsonschema.Schema { return nil }

func (t *runSkillScriptTool) Handle(ctx context.Context, input string) (string, error) {
	var req struct {
		SkillName      string            `json:"skill_name"`
		ScriptPath     string            `json:"script_path"`
		Args           []string          `json:"args"`
		Env            map[string]string `json:"env"`
		TimeoutSeconds int               `json:"timeout_seconds"`
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
	if req.ScriptPath == "" {
		return mustJSON(map[string]any{
			"error":      "Script path is required.",
			"error_code": "MISSING_SCRIPT_PATH",
		}), nil
	}
	skill, ok := t.toolset.skillByName[req.SkillName]
	if !ok {
		return skillNotFound(req.SkillName), nil
	}
	scriptName, fullScriptPath, err := normalizeScriptPath(req.ScriptPath)
	if err != nil {
		return mustJSON(map[string]any{
			"error":      err.Error(),
			"error_code": "INVALID_SCRIPT_PATH",
		}), nil
	}
	resources := skill.resources
	if _, found := resources.GetScript(scriptName); !found {
		return mustJSON(map[string]any{
			"error":      fmt.Sprintf("Script %q not found in skill %q.", fullScriptPath, req.SkillName),
			"error_code": "SCRIPT_NOT_FOUND",
		}), nil
	}

	timeoutSeconds := req.TimeoutSeconds
	if timeoutSeconds == 0 {
		timeoutSeconds = defaultScriptTimeoutSeconds
	}
	if timeoutSeconds < 0 || timeoutSeconds > maxScriptTimeoutSeconds {
		return mustJSON(map[string]any{
			"error":      fmt.Sprintf("timeout_seconds must be between 1 and %d.", maxScriptTimeoutSeconds),
			"error_code": "INVALID_TIMEOUT",
		}), nil
	}
	for key, value := range req.Env {
		if key == "" ||
			strings.Contains(key, "=") ||
			strings.ContainsRune(key, 0) ||
			strings.ContainsRune(value, 0) {
			return mustJSON(map[string]any{
				"error":      "Environment variable names must be non-empty, must not contain '=', and keys/values must not contain NUL.",
				"error_code": "INVALID_ENV",
			}), nil
		}
	}

	tmpRoot, err := os.MkdirTemp("", "blades-skill-*")
	if err != nil {
		return mustJSON(map[string]any{
			"error":      fmt.Sprintf("Failed to prepare skill workspace: %v", err),
			"error_code": "WORKSPACE_ERROR",
		}), nil
	}
	defer os.RemoveAll(tmpRoot)

	if err := materializeSkillWorkspace(tmpRoot, resources); err != nil {
		return mustJSON(map[string]any{
			"error":      fmt.Sprintf("Failed to materialize skill workspace: %v", err),
			"error_code": "WORKSPACE_ERROR",
		}), nil
	}

	return executeSkillScript(ctx, tmpRoot, req.SkillName, fullScriptPath, req.Args, req.Env, timeoutSeconds), nil
}

func normalizeResourcePath(resourcePath string) (resourceType string, resourceName string, err error) {
	resourcePath = strings.TrimSpace(resourcePath)
	resourcePath = strings.ReplaceAll(resourcePath, "\\", "/")
	switch {
	case strings.HasPrefix(resourcePath, "references/"):
		resourceType = "references"
		resourcePath = strings.TrimPrefix(resourcePath, "references/")
	case strings.HasPrefix(resourcePath, "assets/"):
		resourceType = "assets"
		resourcePath = strings.TrimPrefix(resourcePath, "assets/")
	case strings.HasPrefix(resourcePath, "scripts/"):
		resourceType = "scripts"
		resourcePath = strings.TrimPrefix(resourcePath, "scripts/")
	default:
		return "", "", fmt.Errorf("resource path must start with references/, assets/, or scripts/")
	}
	resourceName, err = normalizeSkillRelativePath(resourcePath)
	if err != nil {
		return "", "", fmt.Errorf("resource path must be a relative path within %s/", resourceType)
	}
	return resourceType, resourceName, nil
}

func splitAllowedToolPatterns(raw string) []string {
	items := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func normalizeScriptPath(scriptPath string) (scriptName string, fullScriptPath string, err error) {
	scriptPath = strings.TrimSpace(scriptPath)
	scriptPath = strings.ReplaceAll(scriptPath, "\\", "/")
	scriptPath = strings.TrimPrefix(scriptPath, "scripts/")
	clean, err := normalizeSkillRelativePath(scriptPath)
	if err != nil {
		return "", "", fmt.Errorf("script path must be a relative path under scripts/")
	}
	return clean, path.Join("scripts", clean), nil
}

func materializeSkillWorkspace(root string, resources Resources) error {
	for rel, content := range resources.References {
		if err := writeWorkspaceFile(root, "references", rel, content, 0o644); err != nil {
			return err
		}
	}
	for rel, content := range resources.Assets {
		if err := writeWorkspaceFile(root, "assets", rel, content, 0o644); err != nil {
			return err
		}
	}
	for rel, content := range resources.Scripts {
		if err := writeWorkspaceFile(root, "scripts", rel, content, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func writeWorkspaceFile(root string, dir string, rel string, content string, mode fs.FileMode) error {
	clean, err := normalizeSkillRelativePath(rel)
	if err != nil {
		return fmt.Errorf("invalid file path %q", rel)
	}

	baseDir := filepath.Join(root, filepath.FromSlash(dir))
	targetPath := filepath.Join(baseDir, filepath.FromSlash(clean))
	relToBase, err := filepath.Rel(baseDir, targetPath)
	if err != nil {
		return err
	}
	if relToBase == ".." || strings.HasPrefix(relToBase, ".."+string(filepath.Separator)) {
		return fmt.Errorf("invalid file path %q", rel)
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(targetPath, []byte(content), mode)
}

func normalizeSkillRelativePath(rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	rel = strings.ReplaceAll(rel, "\\", "/")
	clean := path.Clean(rel)
	if isInvalidSkillRelativePath(clean) {
		return "", fmt.Errorf("invalid relative path")
	}
	return clean, nil
}

func isInvalidSkillRelativePath(clean string) bool {
	return clean == "" ||
		clean == "." ||
		clean == ".." ||
		strings.HasPrefix(clean, "../") ||
		path.IsAbs(clean) ||
		hasWindowsVolumePrefix(clean)
}

func hasWindowsVolumePrefix(p string) bool {
	if len(p) < 2 {
		return false
	}
	return ((p[0] >= 'a' && p[0] <= 'z') || (p[0] >= 'A' && p[0] <= 'Z')) && p[1] == ':'
}

func mergeCommandEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		out := make([]string, len(base))
		copy(out, base)
		return out
	}
	out := make([]string, 0, len(base)+len(overrides))
	for _, item := range base {
		key := item
		if i := strings.IndexByte(item, '='); i >= 0 {
			key = item[:i]
		}
		if _, overridden := overrides[key]; overridden {
			continue
		}
		out = append(out, item)
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out = append(out, key+"="+overrides[key])
	}
	return out
}

func executeSkillScript(
	ctx context.Context,
	tmpRoot string,
	skillName string,
	scriptPath string,
	args []string,
	env map[string]string,
	timeoutSeconds int,
) string {
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	commandName := scriptPath
	commandArgs := append([]string{}, args...)
	switch strings.ToLower(path.Ext(scriptPath)) {
	case ".py":
		commandName = "python3"
		commandArgs = append([]string{scriptPath}, commandArgs...)
	case ".sh", ".bash":
		commandName = "bash"
		commandArgs = append([]string{scriptPath}, commandArgs...)
	}

	cmd := exec.CommandContext(timeoutCtx, commandName, commandArgs...)
	cmd.Dir = tmpRoot
	cmd.Env = mergeCommandEnv(os.Environ(), env)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	status := "success"
	if err != nil {
		switch {
		case errors.Is(timeoutCtx.Err(), context.DeadlineExceeded):
			exitCode = -1
			status = "timeout"
		default:
			status = "error"
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			} else {
				return mustJSON(map[string]any{
					"error":      fmt.Sprintf("Failed to execute script %q: %v", scriptPath, err),
					"error_code": "EXECUTION_ERROR",
				})
			}
		}
	}

	return mustJSON(map[string]any{
		"skill_name":  skillName,
		"script_path": scriptPath,
		"args":        args,
		"stdout":      stdout.String(),
		"stderr":      stderr.String(),
		"exit_code":   exitCode,
		"status":      status,
	})
}
