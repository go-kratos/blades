package flow

import (
	"context"

	"github.com/go-kratos/blades"
)

// Pipeline represents a sequence of Runnable runners that process input sequentially.
type Pipeline[I, O, Option any] struct {
	name    string
	runners []blades.Runner[I, O, Option]
}

// NewPipeline creates a new Pipeline with the given runners.
func NewPipeline[I, O, Option any](name string, runners ...blades.Runner[I, O, Option]) *Pipeline[I, O, Option] {
	return &Pipeline[I, O, Option]{
		name:    name,
		runners: runners,
	}
}

// Name returns the name of the chain.
func (c *Pipeline[I, O, Option]) Name() string {
	return c.name
}

// Run executes the chain of runners sequentially, passing the output of one as the input to the next.
func (c *Pipeline[I, O, Option]) Run(ctx context.Context, input I, opts ...Option) (O, error) {
	var (
		err    error
		output O
	)
	for _, runner := range c.runners {
		if output, err = runner.Run(ctx, input, opts...); err != nil {
			return output, err
		}
	}
	return output, nil
}

// RunStream executes the chain of runners sequentially, streaming the output of the last runner.
func (c *Pipeline[I, O, Option]) RunStream(ctx context.Context, input I, opts ...Option) (blades.Streamer[O], error) {
	pipe := blades.NewStreamPipe[O]()
	pipe.Go(func() error {
		for _, runner := range c.runners {
			output, err := runner.Run(ctx, input, opts...)
			if err != nil {
				return err
			}
			pipe.Send(output)
		}
		return nil
	})
	return pipe, nil
}
