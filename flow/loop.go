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
	name          string
	maxIterations int
	condition     LoopCondition
	runner        blades.Runnable
}

// NewLoop creates a new Loop instance with the specified name, condition, runner, and options.
func NewLoop(name string, condition LoopCondition, runner blades.Runnable, opts ...LoopOption) *Loop {
	l := &Loop{
		name:          name,
		condition:     condition,
		runner:        runner,
		maxIterations: 3,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Name returns the name of the Loop.
func (l *Loop) Name() string {
	return l.name
}

// Run executes the Loop, repeatedly running the runner until the condition is met or an error occurs.
func (l *Loop) Run(ctx context.Context, input *blades.Prompt, opts ...blades.ModelOption) (*blades.Message, error) {
	var (
		err    error
		output *blades.Message
	)
	for i := 0; i < l.maxIterations; i++ {
		if output, err = l.runner.Run(ctx, input, opts...); err != nil {
			return output, err
		}
		ok, err := l.condition(ctx, output)
		if err != nil {
			return output, err
		}
		if !ok {
			break
		}
	}
	return output, nil
}

// RunStream executes the Loop in a streaming manner, returning a Streamable that emits the final output.
func (l *Loop) RunStream(ctx context.Context, input *blades.Prompt, opts ...blades.ModelOption) (blades.Streamable[*blades.Message], error) {
	pipe := blades.NewStreamPipe[*blades.Message]()
	pipe.Go(func() error {
		output, err := l.Run(ctx, input, opts...)
		if err != nil {
			return err
		}
		pipe.Send(output)
		return nil
	})
	return pipe, nil
}
