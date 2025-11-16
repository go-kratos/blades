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
		ch := make(chan result, len(p.config.SubAgents)*8)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		eg, ctx := errgroup.WithContext(ctx)
		for _, agent := range p.config.SubAgents {
			eg.Go(func() error {
				for message, err := range agent.Run(ctx, invocation.Clone()) {
					if err != nil {
						// Send error result and stop
						ch <- result{message: nil, err: err}
						return err
					}
					ch <- result{message: message, err: nil}
				}
				return nil
			})
		}
		go func() {
			eg.Wait()
			close(ch)
		}()
		for res := range ch {
			if !yield(res.message, res.err) {
				cancel()
				break
			}
		}
	}
}
