package blades

import (
	"context"

	"github.com/go-kratos/blades/event"
)

// RunnerOption configures a Runner.
type RunnerOption func(*Runner)

// Runner provides convenience methods for running an Agent.
type Runner struct {
	agent Agent
}

// NewRunner creates a Runner wrapping the given Agent.
func NewRunner(agent Agent, opts ...RunnerOption) *Runner {
	r := &Runner{agent: agent}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Run sends a single input and blocks until the agent produces a final result.
// Returns the last TurnEnd output. Runtime errors are extracted from event.Error.
func (r *Runner) Run(ctx context.Context, in event.Input) (event.Output, error) {
	ch := make(chan event.Input, 1)
	ch <- in
	close(ch)

	output, err := r.agent.Run(ctx, ch)
	if err != nil {
		return nil, err
	}

	var last event.Output
	for out := range output {
		switch v := out.(type) {
		case event.Error:
			return nil, v.Err
		case event.Done:
			return last, nil
		case event.TurnEnd:
			last = v
		default:
			last = out
		}
	}
	return last, nil
}

// RunStream sends a single input and returns the output channel for streaming consumption.
func (r *Runner) RunStream(ctx context.Context, in event.Input) (<-chan event.Output, error) {
	ch := make(chan event.Input, 1)
	ch <- in
	close(ch)

	return r.agent.Run(ctx, ch)
}

// RunLive passes through an input channel for bidirectional interaction.
func (r *Runner) RunLive(ctx context.Context, in <-chan event.Input) (<-chan event.Output, error) {
	return r.agent.Run(ctx, in)
}
