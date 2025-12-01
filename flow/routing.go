package flow

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/internal/handoff"
)

type RoutingConfig struct {
	Name        string
	Description string
	Model       blades.ModelProvider
	SubAgents   []blades.Agent
}

type RoutingAgent struct {
	blades.Agent
	targets map[string]blades.Agent
}

func NewRoutingAgent(config RoutingConfig) (blades.Agent, error) {
	instruction, err := handoff.BuildInstruction(config.SubAgents)
	if err != nil {
		return nil, err
	}
	rootAgent, err := blades.NewAgent(
		config.Name,
		blades.WithModel(config.Model),
		blades.WithDescription(config.Description),
		blades.WithInstruction(instruction),
		blades.WithTools(handoff.NewHandoffTool()),
	)
	if err != nil {
		return nil, err
	}
	targets := make(map[string]blades.Agent)
	for _, agent := range config.SubAgents {
		targets[strings.TrimSpace(agent.Name())] = agent
	}
	return &RoutingAgent{
		Agent:   rootAgent,
		targets: targets,
	}, nil
}

func (a *RoutingAgent) Run(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
	return func(yield func(*blades.Message, error) bool) {
		var (
			err         error
			targetAgent string
			message     *blades.Message
		)
		for message, err = range a.Agent.Run(ctx, invocation) {
			if err != nil {
				yield(nil, err)
				return
			}
			if target, ok := message.Actions[handoff.ActionHandoffToAgent]; ok {
				targetAgent, _ = target.(string)
				break
			}
		}
		agent, ok := a.targets[targetAgent]
		if !ok {
			// If no target agent found, return the last message from the root agent
			if message != nil && message.Text() != "" {
				yield(message, nil)
				return
			}
			yield(nil, fmt.Errorf("target agent not found: %s", targetAgent))
			return
		}
		for message, err := range agent.Run(ctx, invocation) {
			if !yield(message, err) {
				return
			}
		}
	}
}
