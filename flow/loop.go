package flow

import (
	"context"

	"github.com/go-kratos/blades"
)

type LoopOption[I, O, Option any] func(*Loop[I, O, Option])

func WithLoopMaxIterations[I, O, Option any](n int) LoopOption[I, O, Option] {
	return func(l *Loop[I, O, Option]) {
		l.maxIterations = n
	}
}

type LoopCondition[O any] func(ctx context.Context, output O) (bool, error)

type Loop[I, O, Option any] struct {
	name          string
	maxIterations int
	condition     LoopCondition[O]
	runner        blades.Runner[I, O, Option]
}

func NewLoop[I, O, Option any](name string, condition LoopCondition[O], runner blades.Runner[I, O, Option], opts ...LoopOption[I, O, Option]) *Loop[I, O, Option] {
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

func (l *Loop[I, O, Option]) Run(ctx context.Context, input I, opts ...Option) (O, error) {
	var (
		err    error
		exit   bool
		output O
	)
	for !exit {
		if output, err = l.runner.Run(ctx, input, opts...); err != nil {
			return output, err
		}
		if exit, err = l.condition(ctx, output); err != nil {
			return output, err
		}
	}
	return output, nil
}

func (l *Loop[I, O, Option]) RunStream(ctx context.Context, input I, opts ...Option) (blades.Streamer[O], error) {
	pipe := blades.NewStreamPipe[O]()
	pipe.Go(func() error {
		output, err := l.runner.Run(ctx, input, opts...)
		if err != nil {
			return err
		}
		pipe.Send(output)
		return nil
	})
	return pipe, nil
}
