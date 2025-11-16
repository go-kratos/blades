package flow

import (
	"context"

	"github.com/go-kratos/blades"
)

// SequentialConfig is the configuration for a SequentialAgent.
type SequentialConfig struct {
	Name        string
	Description string
	SubAgents   []blades.Agent
}

// sequentialAgent is an agent that runs sub-agents sequentially.
type sequentialAgent struct {
	config SequentialConfig
}

// NewSequentialAgent creates a new SequentialAgent.
func NewSequentialAgent(config SequentialConfig) blades.Agent {
	return &sequentialAgent{
		config: config,
	}
}

// Name returns the name of the agent.
func (a *sequentialAgent) Name() string {
	return a.config.Name
}

// Description returns the description of the agent.
func (a *sequentialAgent) Description() string {
	return a.config.Description
}

// Run runs the sub-agents sequentially.
func (a *sequentialAgent) Run(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
	return func(yield func(*blades.Message, error) bool) {
		for _, agent := range a.config.SubAgents {
			var (
				err     error
				message *blades.Message
			)
			for message, err = range agent.Run(ctx, invocation.Clone()) {
				if err != nil {
					yield(nil, err)
					return
				}
				if !yield(message, nil) {
					return
				}
			}
			invocation.Message = message
		}
	}
}
