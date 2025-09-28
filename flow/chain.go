package flow

import (
	"context"

	"github.com/go-kratos/blades"
)

var _ Flowable = (*Chain)(nil)

// Chain represents a sequence of Runnable runners that process input sequentially.
type Chain struct {
	next    Node
	runners []blades.Runner
}

// NewChain creates a new Chain with the given runners.
func NewChain(runners ...blades.Runner) *Chain {
	return &Chain{
		runners: runners,
	}
}

// isNode is a marker method to indicate that Chain implements the Node interface.
func (c *Chain) isNode() {}

// To sets the next node in the chain.
func (c *Chain) To(next Node) {
	c.next = next
}

// Run executes the chain of runners sequentially, passing the output of one as the input to the next.
func (c *Chain) Run(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (*blades.Generation, error) {
	var (
		err  error
		last *blades.Generation
	)
	for _, runner := range c.runners {
		last, err = runner.Run(ctx, prompt, opts...)
		if err != nil {
			return nil, err
		}
		prompt = blades.NewPrompt(last.Messages...)
	}
	return last, nil
}

// RunStream executes the chain of runners sequentially, streaming the output of the last runner.
func (c *Chain) RunStream(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (blades.Streamer[*blades.Generation], error) {
	pipe := blades.NewStreamPipe[*blades.Generation]()
	pipe.Go(func() error {
		for _, runner := range c.runners {
			last, err := runner.Run(ctx, prompt, opts...)
			if err != nil {
				return err
			}
			pipe.Send(last)
			prompt = blades.NewPrompt(last.Messages...)
		}
		return nil
	})
	return pipe, nil
}
