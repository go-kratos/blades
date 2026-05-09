package flow

import (
	"context"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/event"
)

// LoopCondition determines whether the loop should continue.
type LoopCondition func(ctx context.Context, state LoopState) (bool, error)

// LoopState carries iteration state for the condition check.
type LoopState struct {
	Iteration int
	LastOutput event.TurnEnd
}

// LoopConfig configures a loop agent.
type LoopConfig struct {
	Name          string
	Description   string
	SubAgents     []blades.Agent
	MaxIterations int
	Condition     LoopCondition
}

// NewLoopAgent creates an agent that iterates sub-agents until a condition is met.
func NewLoopAgent(cfg LoopConfig) blades.Agent {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 10
	}
	return &loopAgent{cfg: cfg}
}

type loopAgent struct {
	cfg LoopConfig
}

func (a *loopAgent) Name() string        { return a.cfg.Name }
func (a *loopAgent) Description() string { return a.cfg.Description }

func (a *loopAgent) Run(ctx context.Context, input <-chan event.Input) (<-chan event.Output, error) {
	output := make(chan event.Output, 64)
	go a.run(ctx, input, output)
	return output, nil
}

func (a *loopAgent) run(ctx context.Context, input <-chan event.Input, output chan<- event.Output) {
	defer func() {
		output <- event.Done{}
		close(output)
	}()

	currentInput := input
	for i := 0; i < a.cfg.MaxIterations; i++ {
		var lastTurn event.TurnEnd

		for _, sub := range a.cfg.SubAgents {
			subOut, err := sub.Run(ctx, currentInput)
			if err != nil {
				output <- event.Error{Err: err}
				return
			}
			for o := range subOut {
				switch v := o.(type) {
				case event.Done:
					continue
				case event.LoopExit:
					output <- v
					return
				case event.TurnEnd:
					lastTurn = v
					output <- o
				default:
					output <- o
				}
			}
		}

		// Check condition
		if a.cfg.Condition != nil {
			cont, err := a.cfg.Condition(ctx, LoopState{Iteration: i, LastOutput: lastTurn})
			if err != nil {
				output <- event.Error{Err: err}
				return
			}
			if !cont {
				return
			}
		}

		// Bridge last output to next input
		if len(lastTurn.Parts) > 0 {
			ch := make(chan event.Input, 1)
			ch <- event.Prompt{Parts: lastTurn.Parts}
			close(ch)
			currentInput = ch
		} else {
			return
		}
	}
}
