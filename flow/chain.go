package flow

import (
	"context"

	"github.com/go-kratos/blades"
)

// Chain represents a sequence of Runnable runners that process input sequentially.
type Chain[I, O, Option any] struct {
	name         string
	stateHandler StateHandler[I, O]
	runners      []blades.Runner[I, O, Option]
}

// NewChain creates a new Chain with the given runners.
func NewChain[I, O, Option any](name string, stateHandler StateHandler[I, O], runners ...blades.Runner[I, O, Option]) *Chain[I, O, Option] {
	return &Chain[I, O, Option]{
		name:         name,
		runners:      runners,
		stateHandler: stateHandler,
	}
}

// Name returns the name of the chain.
func (c *Chain[I, O, Option]) Name() string {
	return c.name
}

// Run executes the chain of runners sequentially, passing the output of one as the input to the next.
func (c *Chain[I, O, Option]) Run(ctx context.Context, input I, opts ...Option) (O, error) {
	var (
		err    error
		output O
	)
	for _, runner := range c.runners {
		output, err = runner.Run(ctx, input, opts...)
		if err != nil {
			return output, err
		}
		input, err = c.stateHandler(ctx, output)
		if err != nil {
			return output, err
		}
	}
	return output, nil
}

// RunStream executes the chain of runners sequentially, streaming the output of the last runner.
func (c *Chain[I, O, Option]) RunStream(ctx context.Context, input I, opts ...Option) (blades.Streamer[O], error) {
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
