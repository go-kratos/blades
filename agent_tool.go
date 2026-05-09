package blades

import (
	"context"
	"encoding/json"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/event"
	"github.com/go-kratos/blades/tools"
)

// NewAgentTool wraps an Agent as a tools.Tool so it can be called by another Agent.
func NewAgentTool(agent Agent) tools.Tool {
	return &agentTool{agent: agent}
}

type agentTool struct {
	agent Agent
}

func (t *agentTool) Spec() tools.ToolSpec {
	return tools.ToolSpec{
		Name:        t.agent.Name(),
		Description: t.agent.Description(),
	}
}

func (t *agentTool) Handle(ctx context.Context, input json.RawMessage) (*tools.Result, error) {
	var text string
	if err := json.Unmarshal(input, &text); err != nil {
		text = string(input)
	}

	in := make(chan event.Input, 1)
	in <- event.NewPromptText(text)
	close(in)

	output, err := t.agent.Run(ctx, in)
	if err != nil {
		return nil, err
	}

	var parts []content.Part
	for out := range output {
		switch v := out.(type) {
		case event.TurnEnd:
			parts = append(parts, v.Parts...)
		case event.Error:
			return nil, v.Err
		}
	}

	return &tools.Result{Parts: parts}, nil
}
