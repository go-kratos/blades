package flow

import (
	"context"

	"github.com/go-kratos/blades"
	"golang.org/x/sync/errgroup"
)

// ParallelMerger is a function that merges the outputs of multiple runners into a single output.
type ParallelMerger[O any] func(ctx context.Context, outputs []O) (O, error)

// Parallel represents a sequence of Runnable runners that process input sequentially.
type Parallel[I, O, Option any] struct {
	name    string
	merger  ParallelMerger[O]
	runners []blades.Runnable[I, O, Option]
}

// NewParallel creates a new Parallel with the given runners.
func NewParallel[I, O, Option any](name string, merger ParallelMerger[O], runners ...blades.Runnable[I, O, Option]) *Parallel[I, O, Option] {
	return &Parallel[I, O, Option]{
		name:    name,
		merger:  merger,
		runners: runners,
	}
}

// Name returns the name of the Parallel.
func (c *Parallel[I, O, Option]) Name() string {
	return c.name
}

// Run executes the chain of runners sequentially, passing the output of one as the input to the next.
func (c *Parallel[I, O, Option]) Run(ctx context.Context, input I, opts ...Option) (o O, err error) {
	var (
		outputs = make([]O, len(c.runners))
	)
	eg, ctx := errgroup.WithContext(ctx)
	for idx, runner := range c.runners {
		idxCopy := idx
		eg.Go(func() error {
			output, err := runner.Run(ctx, input, opts...)
			if err != nil {
				return err
			}
			outputs[idxCopy] = output
			return nil
		})
	}
	if err = eg.Wait(); err != nil {
		return
	}
	return c.merger(ctx, outputs)
}

// RunStream executes the runners sequentially, streaming each output as it is produced.
// Note: Although this method belongs to the Parallel struct, it runs runners one after another, not in parallel.
func (c *Parallel[I, O, Option]) RunStream(ctx context.Context, input I, opts ...Option) (blades.Streamable[O], error) {
	pipe := blades.NewStreamPipe[O]()
	pipe.Go(func() error {
		output, err := c.Run(ctx, input, opts...)
		if err != nil {
			return err
		}
		pipe.Send(output)
		return nil
	})
	return pipe, nil
}
