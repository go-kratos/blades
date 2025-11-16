package flow

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/internal/handoff"
)

type HandoffConfig struct {
	Name        string
	Description string
	Model       blades.ModelProvider
	SubAgents   []blades.Agent
}

type HandoffAgent struct {
	blades.Agent
	targets map[string]blades.Agent
}

func NewHandoffAgent(config HandoffConfig) (blades.Agent, error) {
	instructions, err := handoff.BuildInstructions(config.SubAgents)
	if err != nil {
		return nil, err
	}
	rootAgent, err := blades.NewAgent(
		config.Name,
		blades.WithModel(config.Model),
		blades.WithDescription(config.Description),
		blades.WithInstructions(instructions),
		blades.WithTools(handoff.NewHandoffTool()),
	)
	if err != nil {
		return nil, err
	}
	targets := make(map[string]blades.Agent)
	for _, agent := range config.SubAgents {
		targets[strings.TrimSpace(agent.Name())] = agent
	}
	return &HandoffAgent{
		Agent:   rootAgent,
		targets: targets,
	}, nil
}

func (a *HandoffAgent) Run(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
	return func(yield func(*blades.Message, error) bool) {
		h := &handoff.Handoff{}
		for _, err := range a.Agent.Run(handoff.NewContext(ctx, h), invocation) {
			if err != nil {
				yield(nil, err)
				return
			}
		}
		agent, ok := a.targets[strings.TrimSpace(h.TargetAgent)]
		if !ok {
			yield(nil, fmt.Errorf("target agent not found: %s", h.TargetAgent))
			return
		}
		for message, err := range agent.Run(ctx, invocation) {
			if !yield(message, err) {
				return
			}
		}
	}
}
