package blades

import (
	"context"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/event"
)

// RunnerOption configures a Runner.
type RunnerOption func(*Runner)

// Runner provides convenience methods for running an Agent.
type Runner struct {
	agent Agent
}

// Result is the final result collected from an Agent output stream.
type Result struct {
	event.TurnEnd
}

// Text returns the concatenated text parts from the final turn.
func (r Result) Text() string {
	return content.TextFromParts(r.Parts)
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
// Runtime errors are extracted from event.Error.
func (r *Runner) Run(ctx context.Context, in event.Input) (Result, error) {
	output, err := r.RunStream(ctx, in)
	if err != nil {
		return Result{}, err
	}
	var (
		result Result
		ok     bool
	)
	for out := range output {
		switch v := out.(type) {
		case event.Error:
			if err == nil {
				err = v.Err
			}
		case event.TurnEnd:
			result = Result{TurnEnd: v}
			ok = true
		}
	}
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{}, ErrNoResult
	}
	return result, nil
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
