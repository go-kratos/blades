package handoff

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)


type handoffTool struct{}

func NewHandoffTool() tools.Tool {
	return &handoffTool{}
}

func (h *handoffTool) Name() string { return "handoff_to_agent" }
func (h *handoffTool) Description() string {
	return `Transfer the question to another agent.
This tool hands off control to another agent when it's more suitable to answer the user's question according to the agent's description.`
}
func (h *handoffTool) InputSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:     "object",
		Required: []string{"agentName"},
		Properties: map[string]*jsonschema.Schema{
			"agentName": {
				Type:        "string",
				Description: "The name of the agent to transfer control to",
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
	agentName := args["agentName"]
	if agentName == "" {
		return "", fmt.Errorf("agentName must be a non-empty string")
	}
	// Set the target agent in the handoff control
	handoff, ok := FromContext(ctx)
	if !ok {
		return "", fmt.Errorf("handoff control not found in context")
	}
	handoff.TargetAgent = agentName
	return "", nil
}
