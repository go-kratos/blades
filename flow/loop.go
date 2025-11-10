package flow

import (
	"context"

	"github.com/go-kratos/blades"
)

// LoopOption defines a function type for configuring Loop instances.
type LoopOption func(*Loop)

// WithLoopMaxIterations sets the maximum number of iterations for the Loop.
func WithLoopMaxIterations(n int) LoopOption {
	return func(l *Loop) {
		l.maxIterations = n
	}
}

// LoopCondition defines a function type for evaluating the loop condition.
type LoopCondition func(ctx context.Context, output *blades.Message) (bool, error)

// Loop represents a looping construct that repeatedly executes a runner until a condition is met.
type Loop struct {
	maxIterations int
	condition     LoopCondition
	agent         blades.Agent
}

// NewLoop creates a new Loop instance with the specified condition, runner, and options.
func NewLoop(condition LoopCondition, agent blades.Agent, opts ...LoopOption) *Loop {
	l := &Loop{
		condition:     condition,
		agent:         agent,
		maxIterations: 3,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Run executes the Loop, repeatedly running the runner until the condition is met or an error occurs.
func (l *Loop) Run(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
	return func(yield func(*blades.Message, error) bool) {
		var (
			err    error
			output *blades.Message
		)
		for i := 0; i < l.maxIterations; i++ {
			for output, err = range l.agent.Run(ctx, invocation) {
				if err != nil {
					yield(nil, err)
					return
				}
				ok, err := l.condition(ctx, output)
				if err != nil {
					yield(nil, err)
					return
				}
				if !ok {
					goto end
				}
			}
		}
	end:
		if err != nil {
			yield(nil, err)
		} else {
			yield(output, nil)
		}
	}
}
