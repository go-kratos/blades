package blades

import (
	"context"
	"sync"
)

var (
	_ Runner = (*Chain)(nil)
)

// Chain represents a sequence of Runnable runners that process input sequentially.
type Chain struct {
	runners []Runner
}

// NewChain creates a new Chain with the given runners.
func NewChain(runners ...Runner) *Chain {
	return &Chain{
		runners: runners,
	}
}

// Run executes the chain of runners sequentially, passing the output of one as the input to the next.
func (c *Chain) Run(ctx context.Context, prompt *Prompt, opts ...ModelOption) (*Generation, error) {
	var (
		err  error
		last *Generation
	)
	for _, runner := range c.runners {
		last, err = runner.Run(ctx, prompt, opts...)
		if err != nil {
			return nil, err
		}
		prompt = NewPrompt(last.Message)
	}
	return last, nil
}

// RunStream executes the chain of runners sequentially, streaming the output of the last runner.
func (c *Chain) RunStream(ctx context.Context, prompt *Prompt, opts ...ModelOption) (Streamer[*Generation], error) {
	var (
		wg  sync.WaitGroup
		out Streamer[*Generation]
	)
	for _, runner := range c.runners {
		stream, err := runner.RunStream(ctx, prompt, opts...)
		if err != nil {
			return nil, err
		}
		wg.Add(1)
		out = NewMappedStream[*Generation, *Generation](stream, func(m *Generation) (*Generation, error) {
			// Update the prompt with the latest message for the next runner
			if m.Message.Status == StatusCompleted {
				prompt = NewPrompt(m.Message)
				wg.Done()
			}
			return m, nil
		})
		// Wait for the current runner to complete before moving to the next
		wg.Wait()
	}
	return out, nil
}
