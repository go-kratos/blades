package blades

import (
	"context"

	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)

// agentTool is a tool that wraps an Agent.
type agentTool struct {
	Agent
}

// NewAgentTool creates a new tool that wraps the given Agent.
func NewAgentTool(agent Agent) tools.Tool {
	return &agentTool{Agent: agent}
}

// InputSchema returns the input schema of the underlying Agent, if it has one.
func (a *agentTool) InputSchema() *jsonschema.Schema {
	if agent, ok := a.Agent.(interface {
		InputSchema() *jsonschema.Schema
	}); ok {
		return agent.InputSchema()
	}
	return nil
}

// OutputSchema returns the output schema of the underlying Agent, if it has one.
func (a *agentTool) OutputSchema() *jsonschema.Schema {
	if agent, ok := a.Agent.(interface {
		OutputSchema() *jsonschema.Schema
	}); ok {
		return agent.OutputSchema()
	}
	return nil
}

// Handle runs the underlying Agent with the given input and returns the output.
func (a *agentTool) Handle(ctx context.Context, input string) (string, error) {
	iter := a.Agent.Run(ctx, NewInvocation(UserMessage(input)))
	for output, err := range iter {
		if err != nil {
			return "", err
		}
		return output.Text(), nil
	}
	return "", ErrNoFinalResponse
}
