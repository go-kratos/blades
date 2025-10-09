package flow

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades"
)

// BranchSelector is a function that selects a branch name based on the context.
type BranchSelector[I any] func(context.Context, I) (string, error)

// Branch represents a branching structure of Runnable runners that process input based on a selector function.
type Branch[I, O, Option any] struct {
	selector BranchSelector[I]
	runners  map[string]blades.Runner[I, O, Option]
}

// NewBranch creates a new Branch with the given selector and runners.
func NewBranch[I, O, Option any](selector BranchSelector[I], runners map[string]blades.Runner[I, O, Option]) *Branch[I, O, Option] {
	return &Branch[I, O, Option]{
		selector: selector,
		runners:  runners,
	}
}

// Run executes the selected runner based on the selector function.
func (c *Branch[I, O, Option]) Run(ctx context.Context, input I, opts ...Option) (O, error) {
	var (
		err    error
		output O
	)
	name, err := c.selector(ctx, input)
	if err != nil {
		return output, err
	}
	runner, ok := c.runners[name]
	if !ok {
		return output, fmt.Errorf("Branch: runner not found: %s", name)
	}
	return runner.Run(ctx, input, opts...)
}

// RunStream executes the selected runner based on the selector function and streams its output.
func (c *Branch[I, O, Option]) RunStream(ctx context.Context, input I, opts ...Option) (blades.Streamer[O], error) {
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
