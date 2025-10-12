package flow

import (
	"context"

	"github.com/go-kratos/blades"
)

// Sequential represents a sequence of Runnable runners that process input sequentially.
type Sequential[I, O, Option any] struct {
	name         string
	runners      []blades.Runner[I, O, Option]
	stateHandler StateHandler[I, O]
}

// NewSequential creates a new Sequential with the given runners.
func NewSequential[I, O, Option any](name string, stateHandler StateHandler[I, O], runners ...blades.Runner[I, O, Option]) *Sequential[I, O, Option] {
	return &Sequential[I, O, Option]{
		name:         name,
		runners:      runners,
		stateHandler: stateHandler,
	}
}

// Name returns the name of the chain.
func (c *Sequential[I, O, Option]) Name() string {
	return c.name
}

// Run executes the chain of runners sequentially, passing the output of one as the input to the next.
func (c *Sequential[I, O, Option]) Run(ctx context.Context, input I, opts ...Option) (O, error) {
	var (
		err    error
		output O
	)
	state, ctx := EnsureState[I, O](ctx)
	for idx, runner := range c.runners {
		if idx > 0 {
			if input, err = c.stateHandler(ctx, runner.Name(), output, state); err != nil {
				return output, err
			}
		}
		if output, err = runner.Run(ctx, input, opts...); err != nil {
			return output, err
		}
		state.Inputs.Store(runner.Name(), input)
		state.Outputs.Store(runner.Name(), output)
	}
	return output, nil
}

// RunStream executes the chain of runners sequentially, streaming the output of the last runner.
func (c *Sequential[I, O, Option]) RunStream(ctx context.Context, input I, opts ...Option) (blades.Streamer[O], error) {
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
