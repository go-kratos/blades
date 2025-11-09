package flow

import (
	"context"

	"github.com/go-kratos/blades"
	"golang.org/x/sync/errgroup"
)

// Parallel represents a collection of Runnable runners that process input concurrently.
type Parallel struct {
	agents []blades.Agent
}

// NewParallel creates a new Parallel with the given runners.
func NewParallel(agents ...blades.Agent) *Parallel {
	return &Parallel{
		agents: agents,
	}
}

// Run executes the chain of runners sequentially, passing the output of one as the input to the next.
func (p *Parallel) Run(ctx context.Context, invocation *blades.Invocation) blades.Sequence[*blades.Message, error] {
	return func(yield func(*blades.Message, error) bool) {
		eg, ctx := errgroup.WithContext(ctx)
		for _, agent := range p.agents {
			eg.Go(func() error {
				for output, err := range agent.Run(ctx, invocation) {
					if err != nil {
						return err
					}
					if !yield(output, nil) {
						return nil
					}
				}
				return nil
			})
		}
		if err := eg.Wait(); err != nil {
			return
		}
	}
}
