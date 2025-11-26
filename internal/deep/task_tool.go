package deep

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)

var _ tools.Tool = (*taskTool)(nil)

type TaskToolConfig struct {
	Model                  blades.ModelProvider
	SubAgents              []blades.Agent
	Tools                  []tools.Tool
	Instructions           []string
	MaxIterations          int
	WithoutGeneralSubAgent bool
}

func newGeneralPurposeAgent(tc TaskToolConfig) (blades.Agent, error) {
	return blades.NewAgent(generalAgentName,
		blades.WithModel(tc.Model),
		blades.WithDescription(generalAgentDescription),
		blades.WithInstruction(strings.Join(tc.Instructions, "\n\n")),
		blades.WithTools(tc.Tools...),
		blades.WithMaxIterations(tc.MaxIterations),
	)
}

func NewTaskTool(tc TaskToolConfig) (tools.Tool, string, error) {
	t := &taskTool{
		subAgents:    tc.SubAgents,
		subAgentsMap: make(map[string]blades.Agent),
	}
	if !tc.WithoutGeneralSubAgent {
		generalAgent, err := newGeneralPurposeAgent(tc)
		if err != nil {
			return nil, "", err
		}
		t.subAgents = append(t.subAgents, generalAgent)
	}
	for _, a := range t.subAgents {
		t.subAgentsMap[a.Name()] = a
	}
	description, err := t.buildDescription()
	if err != nil {
		return nil, "", err
	}
	t.description = description
	return t, taskPrompt, nil
}

type taskTool struct {
	description  string
	subAgents    []blades.Agent
	subAgentsMap map[string]blades.Agent
}

func (t *taskTool) Name() string { return "task" }

func (t *taskTool) buildDescription() (string, error) {
	descs := make([]string, 0, len(t.subAgents))
	for _, a := range t.subAgents {
		descs = append(descs, fmt.Sprintf("- %s: %s", a.Name(), a.Description()))
	}
	var sb strings.Builder
	if err := taskToolDescriptionTmpl.Execute(&sb, map[string]any{
		"SubAgents": strings.Join(descs, "\n"),
	}); err != nil {
		return "", err
	}
	return sb.String(), nil
}

func (t *taskTool) Description() string {
	return t.description
}

type taskToolRequest struct {
	SubagentType string `json:"subagent_type"`
	Description  string `json:"description"`
}

func (t *taskTool) InputSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:     "object",
		Required: []string{"subagent_type", "description"},
		Properties: map[string]*jsonschema.Schema{
			"subagent_type": {
				Type:        "string",
				Description: "The type of subagent to use",
			},
			"description": {
				Type:        "string",
				Description: "A short description of the task",
			},
		},
	}
}

func (t *taskTool) OutputSchema() *jsonschema.Schema { return nil }

func (t *taskTool) Handle(ctx context.Context, input string) (string, error) {
	var req taskToolRequest
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return "", err
	}
	agent, ok := t.subAgentsMap[req.SubagentType]
	if !ok {
		return "", fmt.Errorf("subagent type %s not found", req.SubagentType)
	}
	return blades.NewAgentTool(agent).Handle(ctx, req.Description)
}
