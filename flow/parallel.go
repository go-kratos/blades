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
		type result struct {
			message *blades.Message
			err     error
		}
		resultsCh := make(chan result, 16) // buffer size can be tuned
		eg, ctx := errgroup.WithContext(ctx)
		for _, agent := range p.config.SubAgents {
			agent := agent // capture range variable
			eg.Go(func() error {
				for message, err := range agent.Run(ctx, invocation) {
					if err != nil {
						// Send error result and stop
						resultsCh <- result{message: nil, err: err}
						return err
					}
					resultsCh <- result{message: message, err: nil}
				}
				return nil
			})
		}
		go func() {
			eg.Wait()
			close(resultsCh)
		}()
		for res := range resultsCh {
			if !yield(res.message, res.err) {
				break
			}
		}
	}
}
