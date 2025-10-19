package flow

import (
	"context"

	"github.com/go-kratos/blades"
	"golang.org/x/sync/errgroup"
)

// Parallel represents a sequence of Runnable runners that process input sequentially.
type Parallel struct {
	name    string
	runners []blades.Runnable
}

// NewParallel creates a new Parallel with the given runners.
func NewParallel(name string, runners ...blades.Runnable) *Parallel {
	return &Parallel{
		name:    name,
		runners: runners,
	}
}

// Name returns the name of the Parallel.
func (c *Parallel) Name() string {
	return c.name
}

// Run executes the chain of runners sequentially, passing the output of one as the input to the next.
func (c *Parallel) Run(ctx context.Context, input *blades.Prompt, opts ...blades.ModelOption) (o *blades.Message, err error) {
	var (
		outputs = make([]*blades.Message, len(c.runners))
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
	result := &blades.Message{
		ID:   blades.NewMessageID(),
		Role: blades.RoleAssistant,
	}
	for _, output := range outputs {
		result.Parts = append(result.Parts, output.Parts...)
	}
	return result, nil
}

// RunStream executes the runners sequentially, streaming each output as it is produced.
// Note: Although this method belongs to the Parallel struct, it runs runners one after another, not in parallel.
func (c *Parallel) RunStream(ctx context.Context, input *blades.Prompt, opts ...blades.ModelOption) (blades.Streamable[*blades.Message], error) {
	pipe := blades.NewStreamPipe[*blades.Message]()
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
