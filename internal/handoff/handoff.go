package handoff

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)

// ActionHandoffToAgent is the action name for handing off to a sub-agent.
const ActionHandoffToAgent = "handoff_to_agent"

type handoffTool struct{}

func NewHandoffTool() tools.Tool {
	return &handoffTool{}
}

func (h *handoffTool) Name() string { return "handoff_to_agent" }
func (h *handoffTool) Description() string {
	return `Transfer the question to another agent.
Use this tool to hand off control to a more suitable agent based on the agents' descriptions.`
}
func (h *handoffTool) InputSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:     "object",
		Required: []string{"agentName"},
		Properties: map[string]*jsonschema.Schema{
			"agentName": {
				Type:        "string",
				Description: "The name of the target agent to hand off the request to.",
			},
		},
	}
}
func (h *handoffTool) OutputSchema() *jsonschema.Schema { return nil }
func (h *handoffTool) Handle(ctx context.Context, input string) (string, error) {
	args := map[string]string{}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", err
	}
	agentName := strings.TrimSpace(args["agentName"])
	if agentName == "" {
		return "", fmt.Errorf("agentName must be a non-empty string")
	}
	// Set the target agent in the handoff control
	toolCtx, ok := blades.FromToolContext(ctx)
	if !ok {
		return "", fmt.Errorf("tool context not found in context")
	}
	toolCtx.Actions()[ActionHandoffToAgent] = agentName
	return "", nil
}
