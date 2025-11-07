package flow

import (
	"context"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/stream"
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
func (c *Sequential) RunStream(ctx context.Context, input *blades.Prompt, opts ...blades.ModelOption) (stream.Streamable[*blades.Message], error) {
	return stream.Go(func(yield func(*blades.Message, error) bool) {
		message, err := c.Run(ctx, input, opts...)
		if err != nil {
			yield(nil, err)
			return
		}
		yield(message, nil)
	}), nil
}
