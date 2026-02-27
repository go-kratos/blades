package skills

import (
	"context"
	"encoding/json"
	"testing"
)

func TestNewToolsetDuplicateSkillName(t *testing.T) {
	t.Parallel()

	_, err := NewToolset([]*Skill{
		{Frontmatter: Frontmatter{Name: "dup", Description: "a"}},
		{Frontmatter: Frontmatter{Name: "dup", Description: "b"}},
	})
	if err == nil {
		t.Fatalf("expected duplicate error")
	}
}

func TestSkillTools(t *testing.T) {
	t.Parallel()

	skill := &Skill{
		Frontmatter:  Frontmatter{Name: "skill1", Description: "Skill 1"},
		Instructions: "Do something",
		Resources: Resources{
			References: map[string]string{"ref.md": "ref"},
			Assets:     map[string]string{"asset.txt": "asset"},
			Scripts:    map[string]string{"run.sh": "echo"},
		},
	}
	toolset, err := NewToolset([]*Skill{skill})
	if err != nil {
		t.Fatalf("new toolset: %v", err)
	}
	tools := toolset.Tools()
	if len(tools) != 3 {
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
}

func TestLoadSkillResourceErrors(t *testing.T) {
	t.Parallel()

	skill := &Skill{
		Frontmatter: Frontmatter{Name: "skill1", Description: "Skill 1"},
	}
	toolset, err := NewToolset([]*Skill{skill})
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
