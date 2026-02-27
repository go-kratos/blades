package blades

import (
	"context"
	"strings"
	"testing"

	bladeskills "github.com/go-kratos/blades/skills"
	bladestools "github.com/go-kratos/blades/tools"
)

type captureModel struct {
	req *ModelRequest
}

func (m *captureModel) Name() string { return "capture" }

func (m *captureModel) Generate(ctx context.Context, req *ModelRequest) (*ModelResponse, error) {
	m.req = req
	msg := NewAssistantMessage(StatusCompleted)
	msg.Parts = append(msg.Parts, TextPart{Text: "ok"})
	return &ModelResponse{Message: msg}, nil
}

func (m *captureModel) NewStreaming(context.Context, *ModelRequest) Generator[*ModelResponse, error] {
	return nil
}

func TestAgentWithSkillsInjectsToolsAndInstructions(t *testing.T) {
	t.Parallel()

	model := &captureModel{}
	skill := &bladeskills.Skill{
		Frontmatter: bladeskills.Frontmatter{
			Name:        "planner-skill",
			Description: "Plan things",
		},
		Instructions: "Follow this checklist.",
	}
	agent, err := NewAgent("agent", WithModel(model), WithSkills(skill))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	runner := NewRunner(agent)
	if _, err := runner.Run(context.Background(), UserMessage("hi")); err != nil {
		t.Fatalf("runner run: %v", err)
	}
	if model.req == nil {
		t.Fatalf("model request not captured")
	}
	names := make(map[string]struct{}, len(model.req.Tools))
	for _, tool := range model.req.Tools {
		names[tool.Name()] = struct{}{}
	}
	for _, name := range []string{
		bladeskills.ToolListSkillsName,
		bladeskills.ToolLoadSkillName,
		bladeskills.ToolLoadSkillResourceName,
		bladeskills.ToolRunSkillScriptName,
	} {
		if _, ok := names[name]; !ok {
			t.Fatalf("expected injected tool %q", name)
		}
	}
	if model.req.Instruction == nil {
		t.Fatalf("expected instruction")
	}
	instructionText := model.req.Instruction.Text()
	if !strings.Contains(instructionText, "<available_skills>") {
		t.Fatalf("expected available_skills block")
	}
	if !strings.Contains(instructionText, "planner-skill") {
		t.Fatalf("expected skill name in instruction")
	}
}

func TestAgentWithSkillsDuplicateToolNameAllowed(t *testing.T) {
	t.Parallel()

	model := &captureModel{}
	skill := &bladeskills.Skill{
		Frontmatter: bladeskills.Frontmatter{
			Name:        "planner-skill",
			Description: "Plan things",
		},
		Instructions: "Follow this checklist.",
	}
	dupTool := bladestools.NewTool(
		bladeskills.ToolListSkillsName,
		"duplicate list skills",
		bladestools.HandleFunc(func(ctx context.Context, input string) (string, error) {
			return "ok", nil
		}),
	)
	agent, err := NewAgent("agent", WithModel(model), WithTools(dupTool), WithSkills(skill))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	runner := NewRunner(agent)
	if _, err := runner.Run(context.Background(), UserMessage("hi")); err != nil {
		t.Fatalf("runner run: %v", err)
	}
}

func TestAgentWithSkillsAllowedToolsStrictAtStart(t *testing.T) {
	t.Parallel()

	model := &captureModel{}
	skill := &bladeskills.Skill{
		Frontmatter: bladeskills.Frontmatter{
			Name:         "planner-skill",
			Description:  "Plan things",
			AllowedTools: "allowed-*",
		},
		Instructions: "Follow this checklist.",
	}
	allowedTool := bladestools.NewTool(
		"allowed-tool",
		"allowed tool",
		bladestools.HandleFunc(func(ctx context.Context, input string) (string, error) {
			return "ok", nil
		}),
	)
	blockedTool := bladestools.NewTool(
		"blocked-tool",
		"blocked tool",
		bladestools.HandleFunc(func(ctx context.Context, input string) (string, error) {
			return "ok", nil
		}),
	)
	agent, err := NewAgent("agent", WithModel(model), WithTools(allowedTool, blockedTool), WithSkills(skill))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	runner := NewRunner(agent)
	if _, err := runner.Run(context.Background(), UserMessage("hi")); err != nil {
		t.Fatalf("runner run: %v", err)
	}
	if model.req == nil {
		t.Fatalf("model request not captured")
	}
	names := make(map[string]struct{}, len(model.req.Tools))
	for _, tool := range model.req.Tools {
		names[tool.Name()] = struct{}{}
	}
	if _, ok := names["allowed-tool"]; !ok {
		t.Fatalf("expected allowed tool to remain")
	}
	if _, ok := names["blocked-tool"]; ok {
		t.Fatalf("expected blocked tool to be filtered")
	}
	for _, core := range []string{
		bladeskills.ToolListSkillsName,
		bladeskills.ToolLoadSkillName,
		bladeskills.ToolLoadSkillResourceName,
		bladeskills.ToolRunSkillScriptName,
	} {
		if _, ok := names[core]; !ok {
			t.Fatalf("expected core skill tool %q", core)
		}
	}
}

func TestAgentWithSkillsInvalidAllowedToolsPattern(t *testing.T) {
	t.Parallel()

	model := &captureModel{}
	skill := &bladeskills.Skill{
		Frontmatter: bladeskills.Frontmatter{
			Name:         "planner-skill",
			Description:  "Plan things",
			AllowedTools: "[bad",
		},
		Instructions: "Follow this checklist.",
	}
	agent, err := NewAgent("agent", WithModel(model), WithSkills(skill))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	runner := NewRunner(agent)
	if _, err := runner.Run(context.Background(), UserMessage("hi")); err == nil {
		t.Fatalf("expected runner error for invalid allowed-tools pattern")
	}
}
