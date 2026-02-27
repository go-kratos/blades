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

type testSkill struct {
	frontmatter bladeskills.Frontmatter
	instruction string
	resources   bladeskills.Resources
}

func (s testSkill) Name() string                         { return s.frontmatter.Name }
func (s testSkill) Description() string                  { return s.frontmatter.Description }
func (s testSkill) Instruction() string                  { return s.instruction }
func (s testSkill) Frontmatter() bladeskills.Frontmatter { return s.frontmatter }
func (s testSkill) Resources() bladeskills.Resources     { return s.resources }

func TestAgentWithSkillsInjectsToolsAndInstructions(t *testing.T) {
	t.Parallel()

	model := &captureModel{}
	skill := testSkill{
		frontmatter: bladeskills.Frontmatter{
			Name:        "planner-skill",
			Description: "Plan things",
		},
		instruction: "Follow this checklist.",
		resources:   bladeskills.Resources{},
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
	skill := testSkill{
		frontmatter: bladeskills.Frontmatter{
			Name:        "planner-skill",
			Description: "Plan things",
		},
		instruction: "Follow this checklist.",
		resources:   bladeskills.Resources{},
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
	skill := testSkill{
		frontmatter: bladeskills.Frontmatter{
			Name:         "planner-skill",
			Description:  "Plan things",
			AllowedTools: "allowed-*",
		},
		instruction: "Follow this checklist.",
		resources:   bladeskills.Resources{},
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
	skill := testSkill{
		frontmatter: bladeskills.Frontmatter{
			Name:         "planner-skill",
			Description:  "Plan things",
			AllowedTools: "[bad",
		},
		instruction: "Follow this checklist.",
		resources:   bladeskills.Resources{},
	}
	if _, err := NewAgent("agent", WithModel(model), WithSkills(skill)); err == nil {
		t.Fatalf("expected new agent error for invalid allowed-tools pattern")
	}
}

func TestAgentWithSkillsInvalidFrontmatterAtConstruction(t *testing.T) {
	t.Parallel()

	model := &captureModel{}
	skill := testSkill{
		frontmatter: bladeskills.Frontmatter{
			Name:        "invalid_name",
			Description: "Plan things",
		},
		instruction: "Follow this checklist.",
		resources:   bladeskills.Resources{},
	}
	if _, err := NewAgent("agent", WithModel(model), WithSkills(skill)); err == nil {
		t.Fatalf("expected new agent error for invalid skill frontmatter")
	}
}
