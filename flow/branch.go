package flow

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades"
)

// BranchCondition is a function that selects a branch name based on the context.
type BranchCondition func(ctx context.Context, input *blades.Prompt) (string, error)

// Branch represents a branching structure of Runnable runners that process input based on a selector function.
type Branch struct {
	name      string
	condition BranchCondition
	runners   map[string]blades.Runnable
}

// NewBranch creates a new Branch with the given selector and runners.
func NewBranch(name string, condition BranchCondition, runners ...blades.Runnable) *Branch {
	m := make(map[string]blades.Runnable)
	for _, runner := range runners {
		m[runner.Name()] = runner
	}
	return &Branch{
		name:      name,
		condition: condition,
		runners:   m,
	}
}

// Name returns the name of the Branch.
func (c *Branch) Name() string {
	return c.name
}

// Run executes the selected runner based on the selector function.
func (c *Branch) Run(ctx context.Context, input *blades.Prompt, opts ...blades.ModelOption) (*blades.Message, error) {
	var (
		err    error
		output *blades.Message
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
func (c *Branch) RunStream(ctx context.Context, input *blades.Prompt, opts ...blades.ModelOption) (blades.Streamable[*blades.Message], error) {
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
