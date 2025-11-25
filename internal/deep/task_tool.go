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

func NewTaskTool(subAgents ...blades.Agent) (tools.Tool, string, error) {
	if len(subAgents) == 0 {
		return nil, "", fmt.Errorf("at least one subagent is required")
	}
	t := &taskTool{
		subAgents:    subAgents,
		subAgentsMap: make(map[string]blades.Agent),
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
	var builder strings.Builder
	for _, a := range t.subAgents {
		builder.WriteString(fmt.Sprintf("- %s: %s\n", a.Name(), a.Description()))
	}
	if err := taskToolDescriptionTmpl.Execute(&builder, map[string]any{
		"SubAgents": builder.String(),
	}); err != nil {
		return "", err
	}
	return builder.String(), nil
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
