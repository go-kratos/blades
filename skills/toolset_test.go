package skills

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

func TestLoadSkillResourcePathNormalizationAndTraversal(t *testing.T) {
	t.Parallel()

	skill := &staticSkill{
		frontmatter: Frontmatter{Name: "skill1", Description: "Skill 1"},
		instruction: "",
		resources: Resources{
			References: map[string]string{"ref.md": "ref"},
		},
	}
	toolset, err := NewToolset([]Skill{skill})
	if err != nil {
		t.Fatalf("new toolset: %v", err)
	}
	tool := toolset.Tools()[2]

	resp, err := tool.Handle(context.Background(), mustJSON(map[string]any{
		"skill_name": "skill1",
		"path":       `references\ref.md`,
	}))
	if err != nil {
		t.Fatalf("tool error: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(resp), &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if obj["content"] != "ref" {
		t.Fatalf("unexpected content: %v", obj["content"])
	}

	for _, p := range []string{
		`references\..\secret.md`,
		`references\..\..\secret.md`,
		`scripts\..\run.sh`,
		`C:\secret.txt`,
		`C:secret.txt`,
	} {
		resp, err := tool.Handle(context.Background(), mustJSON(map[string]any{
			"skill_name": "skill1",
			"path":       p,
		}))
		if err != nil {
			t.Fatalf("tool error for %q: %v", p, err)
		}
		if err := json.Unmarshal([]byte(resp), &obj); err != nil {
			t.Fatalf("unmarshal for %q: %v", p, err)
		}
		if obj["error_code"] != "INVALID_RESOURCE_PATH" {
			t.Fatalf("unexpected error_code for %q: %v", p, obj["error_code"])
		}
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

	var obj map[string]any
	for _, scriptPath := range []string{"../hack.sh", "..", "scripts/..", "a/../.."} {
		resp, err := tool.Handle(context.Background(), mustJSON(map[string]any{
			"skill_name":  "skill1",
			"script_path": scriptPath,
		}))
		if err != nil {
			t.Fatalf("tool error for %q: %v", scriptPath, err)
		}
		if err := json.Unmarshal([]byte(resp), &obj); err != nil {
			t.Fatalf("unmarshal for %q: %v", scriptPath, err)
		}
		if obj["error_code"] != "INVALID_SCRIPT_PATH" {
			t.Fatalf("unexpected error_code for %q: %v", scriptPath, obj["error_code"])
		}
	}

	resp, err := tool.Handle(context.Background(), `{"skill_name":"skill1","script_path":"scripts/missing.sh"}`)
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

func TestNormalizeScriptPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		input          string
		wantScriptName string
		wantFullPath   string
		wantError      bool
	}{
		{
			name:           "plain file",
			input:          "run.sh",
			wantScriptName: "run.sh",
			wantFullPath:   "scripts/run.sh",
		},
		{
			name:           "scripts prefix",
			input:          "scripts/run.sh",
			wantScriptName: "run.sh",
			wantFullPath:   "scripts/run.sh",
		},
		{
			name:           "nested",
			input:          "nested/run.sh",
			wantScriptName: "nested/run.sh",
			wantFullPath:   "scripts/nested/run.sh",
		},
		{
			name:           "windows separator nested",
			input:          `scripts\nested\run.sh`,
			wantScriptName: "nested/run.sh",
			wantFullPath:   "scripts/nested/run.sh",
		},
		{name: "dot", input: ".", wantError: true},
		{name: "dot dot", input: "..", wantError: true},
		{name: "parent", input: "../x.sh", wantError: true},
		{name: "windows parent", input: `..\x.sh`, wantError: true},
		{name: "absolute", input: "/x.sh", wantError: true},
		{name: "windows drive absolute", input: `C:\x.sh`, wantError: true},
		{name: "windows drive relative", input: `C:x.sh`, wantError: true},
		{name: "cleaned parent", input: "a/../..", wantError: true},
		{name: "scripts dot dot", input: "scripts/..", wantError: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotName, gotPath, err := normalizeScriptPath(tt.input)
			if tt.wantError {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeScriptPath: %v", err)
			}
			if gotName != tt.wantScriptName {
				t.Fatalf("unexpected script name: %q", gotName)
			}
			if gotPath != tt.wantFullPath {
				t.Fatalf("unexpected full path: %q", gotPath)
			}
		})
	}
}

func TestWriteWorkspaceFilePathValidation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, rel := range []string{
		"..",
		"a/../..",
		`..\x.sh`,
		`a\..\..\x.sh`,
		`C:\x.sh`,
		`C:x.sh`,
	} {
		err := writeWorkspaceFile(root, "scripts", rel, "echo no", 0o755)
		if err == nil {
			t.Fatalf("expected error for %q", rel)
		}
	}

	const rel = "nested/run.sh"
	if err := writeWorkspaceFile(root, "scripts", rel, "echo ok", 0o755); err != nil {
		t.Fatalf("writeWorkspaceFile: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, "scripts", "nested", "run.sh"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(b) != "echo ok" {
		t.Fatalf("unexpected file content: %q", string(b))
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

func TestRunSkillScriptToolEnvAppliedAndOverrides(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("direct executable script test is not supported on windows")
	}

	skill := &staticSkill{
		frontmatter: Frontmatter{Name: "skill1", Description: "Skill 1"},
		instruction: "",
		resources: Resources{
			Scripts: map[string]string{
				"run": "#!/bin/sh\necho PATH=$PATH\necho FOO=$FOO\n",
			},
		},
	}
	toolset, err := NewToolset([]Skill{skill})
	if err != nil {
		t.Fatalf("new toolset: %v", err)
	}
	tool := toolset.Tools()[3]
	resp, err := tool.Handle(context.Background(), mustJSON(map[string]any{
		"skill_name":  "skill1",
		"script_path": "run",
		"env": map[string]string{
			"PATH": "custom-path",
			"FOO":  "tool-foo",
		},
	}))
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
	if !strings.Contains(stdout, "PATH=custom-path") {
		t.Fatalf("expected PATH override, got stdout: %q", stdout)
	}
	if !strings.Contains(stdout, "FOO=tool-foo") {
		t.Fatalf("expected FOO env, got stdout: %q", stdout)
	}
}

func TestRunSkillScriptToolInvalidEnvNUL(t *testing.T) {
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

	cases := []map[string]string{
		{string([]byte{'B', 'A', 'D', 0, 'K'}): "ok"},
		{"BAD": string([]byte{'o', 'k', 0})},
	}
	for _, env := range cases {
		resp, err := tool.Handle(context.Background(), mustJSON(map[string]any{
			"skill_name":  "skill1",
			"script_path": "run.sh",
			"env":         env,
		}))
		if err != nil {
			t.Fatalf("tool error: %v", err)
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(resp), &obj); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if obj["error_code"] != "INVALID_ENV" {
			t.Fatalf("unexpected error_code: %v", obj["error_code"])
		}
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
