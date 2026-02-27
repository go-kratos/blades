package skills

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	bladestools "github.com/go-kratos/blades/tools"
)

type minimalSkill struct {
	name        string
	description string
	instruction string
}

func (s minimalSkill) Name() string        { return s.name }
func (s minimalSkill) Description() string { return s.description }
func (s minimalSkill) Instruction() string { return s.instruction }

func TestNewToolsetDuplicateSkillName(t *testing.T) {
	t.Parallel()

	_, err := NewToolset([]Skill{
		&staticSkill{frontmatter: Frontmatter{Name: "dup", Description: "a"}, instruction: "", resources: Resources{}},
		&staticSkill{frontmatter: Frontmatter{Name: "dup", Description: "b"}, instruction: "", resources: Resources{}},
	})
	if err == nil {
		t.Fatalf("expected duplicate error")
	}
}

func TestSkillTools(t *testing.T) {
	t.Parallel()

	skill := &staticSkill{
		frontmatter: Frontmatter{Name: "skill1", Description: "Skill 1"},
		instruction: "Do something",
		resources: Resources{
			References: map[string]string{"ref.md": "ref"},
			Assets:     map[string]string{"asset.txt": "asset"},
			Scripts:    map[string]string{"run.sh": "echo"},
		},
	}
	toolset, err := NewToolset([]Skill{skill})
	if err != nil {
		t.Fatalf("new toolset: %v", err)
	}
	tools := toolset.Tools()
	if len(tools) != 4 {
		t.Fatalf("unexpected tool count: %d", len(tools))
	}

	listResp, err := tools[0].Handle(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("list_skills error: %v", err)
	}
	if listResp == "" {
		t.Fatalf("expected list response")
	}

	loadResp, err := tools[1].Handle(context.Background(), `{"name":"skill1"}`)
	if err != nil {
		t.Fatalf("load_skill error: %v", err)
	}
	var loadObj map[string]any
	if err := json.Unmarshal([]byte(loadResp), &loadObj); err != nil {
		t.Fatalf("unmarshal load response: %v", err)
	}
	if loadObj["skill_name"] != "skill1" {
		t.Fatalf("unexpected skill_name: %v", loadObj["skill_name"])
	}

	resourceResp, err := tools[2].Handle(context.Background(), `{"skill_name":"skill1","path":"references/ref.md"}`)
	if err != nil {
		t.Fatalf("load_skill_resource error: %v", err)
	}
	var resourceObj map[string]any
	if err := json.Unmarshal([]byte(resourceResp), &resourceObj); err != nil {
		t.Fatalf("unmarshal resource response: %v", err)
	}
	if resourceObj["content"] != "ref" {
		t.Fatalf("unexpected content: %v", resourceObj["content"])
	}

	scriptResp, err := tools[3].Handle(context.Background(), `{"skill_name":"skill1","script_path":"scripts/run.sh"}`)
	if err != nil {
		t.Fatalf("run_skill_script error: %v", err)
	}
	var scriptObj map[string]any
	if err := json.Unmarshal([]byte(scriptResp), &scriptObj); err != nil {
		t.Fatalf("unmarshal run response: %v", err)
	}
	if scriptObj["status"] == "" {
		t.Fatalf("expected run script status")
	}
}

func TestLoadSkillResourceErrors(t *testing.T) {
	t.Parallel()

	skill := &staticSkill{
		frontmatter: Frontmatter{Name: "skill1", Description: "Skill 1"},
		instruction: "",
		resources:   Resources{},
	}
	toolset, err := NewToolset([]Skill{skill})
	if err != nil {
		t.Fatalf("new toolset: %v", err)
	}
	tool := toolset.Tools()[2]
	resp, err := tool.Handle(context.Background(), `{"skill_name":"skill1","path":"unknown/x"}`)
	if err != nil {
		t.Fatalf("tool error: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(resp), &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if obj["error_code"] != "INVALID_RESOURCE_PATH" {
		t.Fatalf("unexpected error_code: %v", obj["error_code"])
	}
}

func TestToolsetComposeToolsWithAllowedPatterns(t *testing.T) {
	t.Parallel()

	toolset, err := NewToolset([]Skill{
		&staticSkill{frontmatter: Frontmatter{Name: "skill1", Description: "Skill 1", AllowedTools: "tool-* search-*"}, instruction: "", resources: Resources{}},
		&staticSkill{frontmatter: Frontmatter{Name: "skill2", Description: "Skill 2", AllowedTools: "search-* , db-*"}, instruction: "", resources: Resources{}},
	})
	if err != nil {
		t.Fatalf("new toolset: %v", err)
	}
	baseTools := []bladestools.Tool{
		bladestools.NewTool("tool-foo", "allowed", bladestools.HandleFunc(func(ctx context.Context, input string) (string, error) {
			return "ok", nil
		})),
		bladestools.NewTool("blocked-foo", "blocked", bladestools.HandleFunc(func(ctx context.Context, input string) (string, error) {
			return "ok", nil
		})),
	}
	composed := toolset.ComposeTools(baseTools)
	names := make([]string, 0, len(composed))
	for _, tool := range composed {
		names = append(names, tool.Name())
	}
	if strings.Contains(strings.Join(names, ","), "blocked-foo") {
		t.Fatalf("blocked tool should be filtered, got: %v", names)
	}
	for _, name := range []string{
		"tool-foo",
		ToolListSkillsName,
		ToolLoadSkillName,
		ToolLoadSkillResourceName,
		ToolRunSkillScriptName,
	} {
		found := false
		for _, item := range names {
			if item == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing expected tool %q in composed list: %v", name, names)
		}
	}
}

func TestToolsetComposeToolsNoAllowedPatterns(t *testing.T) {
	t.Parallel()

	toolset, err := NewToolset([]Skill{
		&staticSkill{frontmatter: Frontmatter{Name: "skill1", Description: "Skill 1"}, instruction: "", resources: Resources{}},
	})
	if err != nil {
		t.Fatalf("new toolset: %v", err)
	}
	baseTools := []bladestools.Tool{
		bladestools.NewTool("blocked-foo", "blocked", bladestools.HandleFunc(func(ctx context.Context, input string) (string, error) {
			return "ok", nil
		})),
	}
	composed := toolset.ComposeTools(baseTools)
	found := false
	for _, tool := range composed {
		if tool.Name() == "blocked-foo" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("base tool should remain when allowed-tools is empty")
	}
}

func TestNewToolsetInvalidAllowedToolPattern(t *testing.T) {
	t.Parallel()

	_, err := NewToolset([]Skill{
		&staticSkill{frontmatter: Frontmatter{Name: "skill1", Description: "Skill 1", AllowedTools: "[invalid"}, instruction: "", resources: Resources{}},
	})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunSkillScriptToolPathAndLookupErrors(t *testing.T) {
	t.Parallel()

	skill := &staticSkill{
		frontmatter: Frontmatter{Name: "skill1", Description: "Skill 1"},
		instruction: "",
		resources: Resources{
			Scripts: map[string]string{"run.sh": "echo ok"},
		},
	}
	toolset, err := NewToolset([]Skill{skill})
	if err != nil {
		t.Fatalf("new toolset: %v", err)
	}
	tool := toolset.Tools()[3]

	resp, err := tool.Handle(context.Background(), `{"skill_name":"skill1","script_path":"../hack.sh"}`)
	if err != nil {
		t.Fatalf("tool error: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(resp), &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if obj["error_code"] != "INVALID_SCRIPT_PATH" {
		t.Fatalf("unexpected error_code: %v", obj["error_code"])
	}

	resp, err = tool.Handle(context.Background(), `{"skill_name":"skill1","script_path":"scripts/missing.sh"}`)
	if err != nil {
		t.Fatalf("tool error: %v", err)
	}
	if err := json.Unmarshal([]byte(resp), &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if obj["error_code"] != "SCRIPT_NOT_FOUND" {
		t.Fatalf("unexpected error_code: %v", obj["error_code"])
	}
}

func TestRunSkillScriptToolExecutesScript(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell script execution is not supported on windows in this test")
	}

	skill := &staticSkill{
		frontmatter: Frontmatter{Name: "skill1", Description: "Skill 1"},
		instruction: "",
		resources: Resources{
			Scripts: map[string]string{
				"run.sh": "#!/bin/sh\necho hello\n",
			},
		},
	}
	toolset, err := NewToolset([]Skill{skill})
	if err != nil {
		t.Fatalf("new toolset: %v", err)
	}
	tool := toolset.Tools()[3]
	resp, err := tool.Handle(context.Background(), `{"skill_name":"skill1","script_path":"scripts/run.sh"}`)
	if err != nil {
		t.Fatalf("tool error: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(resp), &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if obj["status"] != "success" {
		t.Fatalf("unexpected status: %v", obj["status"])
	}
	stdout, _ := obj["stdout"].(string)
	if !strings.Contains(stdout, "hello") {
		t.Fatalf("unexpected stdout: %q", stdout)
	}
}

func TestRunSkillScriptToolAnyExecutable(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("direct executable script test is not supported on windows")
	}

	skill := &staticSkill{
		frontmatter: Frontmatter{Name: "skill1", Description: "Skill 1"},
		instruction: "",
		resources: Resources{
			Scripts: map[string]string{
				"run": "#!/bin/sh\necho direct\n",
			},
		},
	}
	toolset, err := NewToolset([]Skill{skill})
	if err != nil {
		t.Fatalf("new toolset: %v", err)
	}
	tool := toolset.Tools()[3]
	resp, err := tool.Handle(context.Background(), `{"skill_name":"skill1","script_path":"run"}`)
	if err != nil {
		t.Fatalf("tool error: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(resp), &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if obj["status"] != "success" {
		t.Fatalf("unexpected status: %v", obj["status"])
	}
}

func TestToolsetMinimalSkillDefaults(t *testing.T) {
	t.Parallel()

	toolset, err := NewToolset([]Skill{
		minimalSkill{
			name:        "minimal-skill",
			description: "Minimal skill",
			instruction: "Do something minimal",
		},
	})
	if err != nil {
		t.Fatalf("new toolset: %v", err)
	}
	tools := toolset.Tools()
	loadResp, err := tools[1].Handle(context.Background(), `{"name":"minimal-skill"}`)
	if err != nil {
		t.Fatalf("load_skill error: %v", err)
	}
	var loadObj map[string]any
	if err := json.Unmarshal([]byte(loadResp), &loadObj); err != nil {
		t.Fatalf("unmarshal load response: %v", err)
	}
	if loadObj["instructions"] != "Do something minimal" {
		t.Fatalf("unexpected instructions: %v", loadObj["instructions"])
	}

	resourceResp, err := tools[2].Handle(context.Background(), `{"skill_name":"minimal-skill","path":"references/missing.md"}`)
	if err != nil {
		t.Fatalf("load_skill_resource error: %v", err)
	}
	var resourceObj map[string]any
	if err := json.Unmarshal([]byte(resourceResp), &resourceObj); err != nil {
		t.Fatalf("unmarshal resource response: %v", err)
	}
	if resourceObj["error_code"] != "RESOURCE_NOT_FOUND" {
		t.Fatalf("unexpected resource error_code: %v", resourceObj["error_code"])
	}

	scriptResp, err := tools[3].Handle(context.Background(), `{"skill_name":"minimal-skill","script_path":"scripts/missing.sh"}`)
	if err != nil {
		t.Fatalf("run_skill_script error: %v", err)
	}
	var scriptObj map[string]any
	if err := json.Unmarshal([]byte(scriptResp), &scriptObj); err != nil {
		t.Fatalf("unmarshal script response: %v", err)
	}
	if scriptObj["error_code"] != "SCRIPT_NOT_FOUND" {
		t.Fatalf("unexpected script error_code: %v", scriptObj["error_code"])
	}
}

func TestToolsetMinimalSkillValidation(t *testing.T) {
	t.Parallel()

	_, err := NewToolset([]Skill{
		minimalSkill{
			name:        "Invalid_Name",
			description: "desc",
			instruction: "Do something",
		},
	})
	if err == nil {
		t.Fatalf("expected validation error for minimal skill name")
	}
}
