package flow

import (
	"context"

	"github.com/go-kratos/blades"
)

// Sequential represents a sequence of Runnable runners that process input sequentially.
type Sequential struct {
	agents []blades.Agent
}

// NewSequential creates a new Sequential with the given runners.
func NewSequential(agents ...blades.Agent) *Sequential {
	return &Sequential{
		agents: agents,
	}
}

func (c *Sequential) Name() string {
	return "sequential"
}

func (c *Sequential) Description() string {
	return "A chain of runners that execute sequentially."
}

// Run executes the chain of runners sequentially, passing the output of one as the input to the next.
func (c *Sequential) Run(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
	return func(yield func(*blades.Message, error) bool) {
		var (
			err    error
			output *blades.Message
		)
		for _, agent := range c.agents {
			for output, err = range agent.Run(ctx, invocation) {
				if err != nil {
					yield(nil, err)
					return
				}
			}
			invocation = invocation.Clone()
			invocation.Message = output
		}
		yield(output, nil)
	}
}
