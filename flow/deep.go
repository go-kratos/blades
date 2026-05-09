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

	// Deep agent delegates to sub-agents based on handoff events
	currentInput := input
	for i := 0; i < a.cfg.MaxIterations; i++ {
		for _, sub := range a.cfg.SubAgents {
			subOut, err := sub.Run(ctx, currentInput)
			if err != nil {
				output <- event.Error{Err: err}
				return
			}

			var lastTurn event.TurnEnd
			for o := range subOut {
				switch v := o.(type) {
				case event.Done:
					continue
				case event.TurnEnd:
					lastTurn = v
					output <- o
				default:
					output <- o
				}
			}

			if h, ok := lastTurn.Action.(event.Handoff); ok {
				target := a.findAgent(h.Agent)
				if target != nil {
					ch := make(chan event.Input, 1)
					ch <- event.Prompt{Parts: lastTurn.Parts}
					close(ch)
					currentInput = ch
					break
				}
			}
			return
		}
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
