package flow

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades"
)

// BranchCondition is a function that selects a branch name based on the context.
type BranchCondition[I any] func(context.Context, I) (string, error)

// Branch represents a branching structure of Runnable runners that process input based on a selector function.
type Branch[I, O, Option any] struct {
	name      string
	condition BranchCondition[I]
	runners   map[string]blades.Runnable[I, O, Option]
}

// NewBranch creates a new Branch with the given selector and runners.
func NewBranch[I, O, Option any](name string, condition BranchCondition[I], runners ...blades.Runnable[I, O, Option]) *Branch[I, O, Option] {
	m := make(map[string]blades.Runnable[I, O, Option])
	for _, runner := range runners {
		m[runner.Name()] = runner
	}
	return &Branch[I, O, Option]{
		name:      name,
		condition: condition,
		runners:   m,
	}
}

// Name returns the name of the Branch.
func (c *Branch[I, O, Option]) Name() string {
	return c.name
}

// Run executes the selected runner based on the selector function.
func (c *Branch[I, O, Option]) Run(ctx context.Context, input I, opts ...Option) (O, error) {
	var (
		err    error
		output O
	)
	name, err := c.condition(ctx, input)
	if err != nil {
		return output, err
	}
	runner, ok := c.runners[name]
	if !ok {
		return output, fmt.Errorf("branch: runner not found: %s", name)
	}
	return runner.Run(ctx, input, opts...)
}

// RunStream executes the selected runner based on the selector function and streams its output.
func (c *Branch[I, O, Option]) RunStream(ctx context.Context, input I, opts ...Option) (blades.Streamable[O], error) {
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
