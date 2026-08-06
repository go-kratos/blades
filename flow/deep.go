package flow

import (
	"context"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/event"
)

// DeepConfig configures a deep agent with task delegation.
type DeepConfig struct {
	Name          string
	Description   string
	SubAgents     []blades.Agent
	MaxIterations int
}

// NewDeepAgent creates an agent that can delegate tasks to sub-agents.
func NewDeepAgent(cfg DeepConfig) (blades.Agent, error) {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 10
	}
	return &deepAgent{cfg: cfg}, nil
}

type deepAgent struct {
	cfg DeepConfig
}

func (a *deepAgent) Name() string        { return a.cfg.Name }
func (a *deepAgent) Description() string { return a.cfg.Description }

func (a *deepAgent) Run(ctx context.Context, input <-chan event.Input) (<-chan event.Output, error) {
	output := make(chan event.Output, 64)
	go a.run(ctx, input, output)
	return output, nil
}

func (a *deepAgent) run(ctx context.Context, input <-chan event.Input, output chan<- event.Output) {
	defer func() {
		output <- event.Done{}
		close(output)
	}()

	if len(a.cfg.SubAgents) == 0 {
		return
	}

	// Deep agent delegates to sub-agents based on handoff events
	currentAgent := a.cfg.SubAgents[0]
	currentInput := input
	for i := 0; i < a.cfg.MaxIterations; i++ {
		subOut, err := currentAgent.Run(ctx, currentInput)
		if err != nil {
			output <- event.Error{Err: err}
			return
		}

		lastTurn := forwardAgentOutput(subOut, output)
		handoff, ok := lastTurn.Action.(event.Handoff)
		if !ok {
			return
		}
		target := a.findAgent(handoff.Agent)
		if target == nil || len(lastTurn.Parts) == 0 {
			return
		}
		currentAgent = target
		currentInput = promptInput(lastTurn.Parts)
	}
}

func (a *deepAgent) findAgent(name string) blades.Agent {
	for _, sub := range a.cfg.SubAgents {
		if sub.Name() == name {
			return sub
		}
	}
	return nil
}

func forwardAgentOutput(input <-chan event.Output, output chan<- event.Output) event.TurnEnd {
	var lastTurn event.TurnEnd
	for item := range input {
		switch value := item.(type) {
		case event.Done:
			continue
		case event.TurnEnd:
			lastTurn = value
			output <- item
		default:
			output <- item
		}
	}
	return lastTurn
}
