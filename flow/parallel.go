package flow

import (
	"context"

	"github.com/go-kratos/blades"
	"golang.org/x/sync/errgroup"
)

// ParallelConfig is the configuration for a ParallelAgent.
type ParallelConfig struct {
	Name        string
	Description string
	SubAgents   []blades.Agent
}

// parallelAgent is an agent that runs sub-agents in parallel.
type parallelAgent struct {
	config ParallelConfig
}

// NewParallelAgent creates a new ParallelAgent.
func NewParallelAgent(config ParallelConfig) blades.Agent {
	return &parallelAgent{config: config}
}

// Name returns the name of the agent.
func (p *parallelAgent) Name() string {
	return p.config.Name
}

// Description returns the description of the agent.
func (p *parallelAgent) Description() string {
	return p.config.Description
}

// Run runs the sub-agents in parallel.
func (p *parallelAgent) Run(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
	return func(yield func(*blades.Message, error) bool) {
		eg, ctx := errgroup.WithContext(ctx)
		for _, agent := range p.config.SubAgents {
			eg.Go(func() error {
				for message, err := range agent.Run(ctx, invocation) {
					if err != nil {
						return err
					}
					if !yield(message, nil) {
						return nil
					}
				}
				return nil
			})
		}
		if err := eg.Wait(); err != nil {
			yield(nil, err)
		}
	}
}
