package flow

import (
	"context"

	"github.com/go-kratos/blades"
)

// LoopOption defines a function type for configuring Loop instances.
type LoopOption[I, O, Option any] func(*Loop[I, O, Option])

// WithLoopMaxIterations sets the maximum number of iterations for the Loop.
func WithLoopMaxIterations[I, O, Option any](n int) LoopOption[I, O, Option] {
	return func(l *Loop[I, O, Option]) {
		l.maxIterations = n
	}
}

// LoopCondition defines a function type for evaluating the loop condition.
type LoopCondition[O any] func(ctx context.Context, output O) (bool, error)

// Loop represents a looping construct that repeatedly executes a runner until a condition is met.
type Loop[I, O, Option any] struct {
	name          string
	maxIterations int
	condition     LoopCondition[O]
	runner        blades.Runnable[I, O, Option]
}

// NewLoop creates a new Loop instance with the specified name, condition, runner, and options.
func NewLoop[I, O, Option any](name string, condition LoopCondition[O], runner blades.Runnable[I, O, Option], opts ...LoopOption[I, O, Option]) *Loop[I, O, Option] {
	l := &Loop[I, O, Option]{
		name:          name,
		maxIterations: 3,
		condition:     condition,
		runner:        runner,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Name returns the name of the Loop.
func (l *Loop[I, O, Option]) Name() string {
	return l.name
}

// Run executes the Loop, repeatedly running the runner until the condition is met or an error occurs.
func (l *Loop[I, O, Option]) Run(ctx context.Context, input I, opts ...Option) (O, error) {
	var (
		err    error
		output O
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
func (l *Loop[I, O, Option]) RunStream(ctx context.Context, input I, opts ...Option) (blades.Streamable[O], error) {
	pipe := blades.NewStreamPipe[O]()
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
