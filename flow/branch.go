package flow

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades"
)

// BranchCondition is a function that selects a branch name based on the context.
type BranchCondition func(ctx context.Context, input *blades.Message) (string, error)

// Branch represents a branching structure of Runnable runners that process input based on a selector function.
type Branch struct {
	condition BranchCondition
	routes    map[string]blades.Agent
}

// NewBranch creates a new Branch with the given selector and runners.
func NewBranch(condition BranchCondition, agents ...blades.Agent) *Branch {
	routes := make(map[string]blades.Agent, len(agents))
	for _, agent := range agents {
		routes[agent.Name()] = agent
	}
	return &Branch{
		condition: condition,
		routes:    routes,
	}
}

// Run executes the selected runner based on the selector function.
func (c *Branch) Run(ctx context.Context, invocation *blades.Invocation) blades.Sequence[*blades.Message, error] {
	return func(yield func(*blades.Message, error) bool) {
		name, err := c.condition(ctx, invocation.Message)
		if err != nil {
			yield(nil, err)
			return
		}
		agent, ok := c.routes[name]
		if !ok {
			yield(nil, fmt.Errorf("branch: runner not found: %s", name))
			return
		}
		for output, err := range agent.Run(ctx, invocation) {
			if !yield(output, err) {
				return
			}
		}
	}
}
