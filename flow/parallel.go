package flow

import (
	"context"

	"github.com/go-kratos/blades"
	"golang.org/x/sync/errgroup"
)

// Parallel represents a sequence of Runnable runners that process input sequentially.
type Parallel[I, O, Option any] struct {
	name    string
	runners []blades.Runner[I, O, Option]
}

// NewParallel creates a new Parallel with the given runners.
func NewParallel[I, O, Option any](name string, runners ...blades.Runner[I, O, Option]) *Parallel[I, O, Option] {
	return &Parallel[I, O, Option]{
		name:    name,
		runners: runners,
	}
}

// Name returns the name of the Parallel.
func (c *Parallel[I, O, Option]) Name() string {
	return c.name
}

// Run executes the chain of runners sequentially, passing the output of one as the input to the next.
func (c *Parallel[I, O, Option]) Run(ctx context.Context, input I, opts ...Option) (O, error) {
	var (
		err    error
		output O
	)
	eg, ctx := errgroup.WithContext(ctx)
	for _, runner := range c.runners {
		eg.Go(func() error {
			output, err = runner.Run(ctx, input, opts...)
			if err != nil {
				return err
			}
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return output, err
	}
	return output, nil
}

// RunStream executes the chain of runners sequentially, streaming the output of the last runner.
func (c *Parallel[I, O, Option]) RunStream(ctx context.Context, input I, opts ...Option) (blades.Streamer[O], error) {
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
