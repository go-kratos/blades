package flow

import (
	"context"
	"log"
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
	instructions, err := handoff.BuildInstructions(config.Name, config.SubAgents)
	if err != nil {
		return nil, err
	}
	log.Println(instructions)
	rootAgent, err := blades.NewAgent(
		config.Name,
		blades.WithInstructions(instructions),
		blades.WithModel(config.Model),
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

func (h *HandoffAgent) Run(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
	return func(yield func(*blades.Message, error) bool) {
		control := &handoff.Handoff{}
		for message, err := range h.Agent.Run(handoff.NewContext(ctx, control), invocation) {
			log.Println("Root Agent:", message, err)
			break
		}
		agent, ok := h.targets[control.TargetAgent]
		if !ok {
			log.Println("Unknown target agent:", control.TargetAgent)
		}
		log.Println("Handoff Agent:", agent.Name(), invocation.Message.Text())
		for message, err := range agent.Run(ctx, invocation) {
			log.Println(message, err, control.TargetAgent)
			if !yield(message, err) {
				return
			}
		}

	}
}
