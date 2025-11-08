package flow

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/stream"
)

// BranchCondition is a function that selects a branch name based on the context.
type BranchCondition func(ctx context.Context, input *blades.Prompt) (string, error)

// Branch represents a branching structure of Runnable runners that process input based on a selector function.
type Branch struct {
	condition BranchCondition
	runners   map[string]blades.Runnable
}

// NewBranch creates a new Branch with the given selector and runners.
func NewBranch(condition BranchCondition, runners map[string]blades.Runnable) *Branch {
	return &Branch{
		condition: condition,
		runners:   runners,
	}
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
func (c *Branch) RunStream(ctx context.Context, input *blades.Prompt, opts ...blades.ModelOption) stream.Streamable[*blades.Message] {
	return func(yield func(*blades.Message, error) bool) {
		message, err := c.Run(ctx, input, opts...)
		if err != nil {
			yield(nil, err)
			return
		}
		yield(message, nil)
	}
}
