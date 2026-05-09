package flow

import (
	"context"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/event"
)

// SequentialConfig configures a sequential agent.
type SequentialConfig struct {
	Name        string
	Description string
	SubAgents   []blades.Agent
	Bridge      Bridge
}

// Bridge converts the output of one agent into input for the next.
type Bridge interface {
	NextInput(ctx context.Context, from <-chan event.Output) (<-chan event.Input, error)
}

// BridgeFunc is a function adapter for Bridge.
type BridgeFunc func(ctx context.Context, from <-chan event.Output) (<-chan event.Input, error)

func (f BridgeFunc) NextInput(ctx context.Context, from <-chan event.Output) (<-chan event.Input, error) {
	return f(ctx, from)
}

// NewSequentialAgent creates an agent that runs sub-agents in sequence.
func NewSequentialAgent(cfg SequentialConfig) blades.Agent {
	return &sequentialAgent{cfg: cfg}
}

type sequentialAgent struct {
	cfg SequentialConfig
}

func (a *sequentialAgent) Name() string        { return a.cfg.Name }
func (a *sequentialAgent) Description() string { return a.cfg.Description }

func (a *sequentialAgent) Run(ctx context.Context, input <-chan event.Input) (<-chan event.Output, error) {
	output := make(chan event.Output, 64)
	go a.run(ctx, input, output)
	return output, nil
}

func (a *sequentialAgent) run(ctx context.Context, input <-chan event.Input, output chan<- event.Output) {
	defer func() {
		output <- event.Done{}
		close(output)
	}()

	currentInput := input
	for i, sub := range a.cfg.SubAgents {
		subOut, err := sub.Run(ctx, currentInput)
		if err != nil {
			output <- event.Error{Err: err}
			return
		}

		// Last agent: forward output directly
		if i == len(a.cfg.SubAgents)-1 {
			for o := range subOut {
				if _, ok := o.(event.Done); ok {
					continue
				}
				output <- o
			}
			return
		}

		// Intermediate agent: bridge output to next input
		if a.cfg.Bridge != nil {
			nextIn, err := a.cfg.Bridge.NextInput(ctx, subOut)
			if err != nil {
				output <- event.Error{Err: err}
				return
			}
			currentInput = nextIn
		} else {
			currentInput = defaultBridge(subOut)
		}
	}
}

func defaultBridge(from <-chan event.Output) <-chan event.Input {
	ch := make(chan event.Input, 1)
	go func() {
		defer close(ch)
		var lastTurn event.TurnEnd
		for o := range from {
			if te, ok := o.(event.TurnEnd); ok {
				lastTurn = te
			}
		}
		if len(lastTurn.Parts) > 0 {
			ch <- event.Prompt{Parts: lastTurn.Parts}
		}
	}()
	return ch
}
