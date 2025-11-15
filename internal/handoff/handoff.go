package handoff

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)

const StateHandoffToAgent = "handoff_to_agent"

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
		Type: "object",
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
	log.Println("Handoff tool invoked with input:", input)
	args := map[string]string{}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", err
	}
	agentName := args["agentName"]
	// Set the target agent in the handoff control
	handoff, ok := FromContext(ctx)
	if !ok {
		return "", fmt.Errorf("handoff control not found in context")
	}
	handoff.TargetAgent = agentName
	return "", nil
}
