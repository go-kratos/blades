package flow

import (
	"context"

	"github.com/go-kratos/blades"
)

// Sequential represents a sequence of Runnable runners that process input sequentially.
type Sequential struct {
	name       string
	runners    []blades.Runnable
	transition TransitionHandler
}

// NewSequential creates a new Sequential with the given runners.
func NewSequential(name string, transition TransitionHandler, runners ...blades.Runnable) *Sequential {
	return &Sequential{
		name:       name,
		runners:    runners,
		transition: transition,
	}
}

// Name returns the name of the chain.
func (c *Sequential) Name() string {
	return c.name
}

// Run executes the chain of runners sequentially, passing the output of one as the input to the next.
func (c *Sequential) Run(ctx context.Context, input *blades.Prompt, opts ...blades.ModelOption) (*blades.Message, error) {
	var (
		err    error
		output *blades.Message
		last   blades.Runnable
	)
	for idx, runner := range c.runners {
		if idx > 0 {
			if input, err = c.transition(ctx, Transition{From: last.Name(), To: runner.Name()}, output); err != nil {
				return output, err
			}
		}
		if output, err = runner.Run(ctx, input, opts...); err != nil {
			return output, err
		}
		last = runner
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
