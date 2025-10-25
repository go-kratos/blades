package flow

import (
	"context"

	"github.com/go-kratos/blades"
)

// Sequential represents a sequence of Runnable runners that process input sequentially.
type Sequential struct {
	runners []blades.Runnable
}

// NewSequential creates a new Sequential with the given runners.
func NewSequential(runners ...blades.Runnable) *Sequential {
	return &Sequential{
		runners: runners,
	}
}

// Run executes the chain of runners sequentially, passing the output of one as the input to the next.
func (c *Sequential) Run(ctx context.Context, input *blades.Prompt, opts ...blades.ModelOption) (*blades.Message, error) {
	var (
		err    error
		output *blades.Message
	)
	for _, runner := range c.runners {
		if output, err = runner.Run(ctx, input, opts...); err != nil {
			return output, err
		}
		input = blades.NewPrompt(output)
	}
	return output, nil
}

// RunStream executes the chain of runners sequentially, streaming the output of the last runner.
func (c *Sequential) RunStream(ctx context.Context, input *blades.Prompt, opts ...blades.ModelOption) (blades.Streamable[*blades.Message], error) {
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
