package flow

import (
	"context"

	"github.com/go-kratos/blades"
)

// LoopCondition is a function that determines whether to continue looping.
type LoopCondition func(ctx context.Context, output *blades.Message) (bool, error)

// LoopConfig is the configuration for a LoopAgent.
type LoopConfig struct {
	Name          string
	Description   string
	MaxIterations int
	Condition     LoopCondition
	SubAgents     []blades.Agent
}

// loopAgent is an agent that runs sub-agents in a loop.
type loopAgent struct {
	config LoopConfig
}

// NewLoopAgent creates a new LoopAgent.
func NewLoopAgent(config LoopConfig) blades.Agent {
	if config.MaxIterations <= 0 {
		config.MaxIterations = 1
	}
	return &loopAgent{config: config}
}

// Name returns the name of the agent.
func (a *loopAgent) Name() string {
	return a.config.Name
}

// Description returns the description of the agent.
func (a *loopAgent) Description() string {
	return a.config.Description
}

// Run runs the sub-agents loop.
func (a *loopAgent) Run(ctx context.Context, input *blades.Invocation) blades.Generator[*blades.Message, error] {
	return func(yield func(*blades.Message, error) bool) {
		for iteration := 0; iteration < a.config.MaxIterations; iteration++ {
			for _, agent := range a.config.SubAgents {
				var (
					err        error
					message    *blades.Message
					invocation = input.Clone()
				)
				for message, err = range agent.Run(ctx, invocation) {
					if err != nil {
						yield(nil, err)
						return
					}
					if !yield(message, nil) {
						return
					}
				}
				if a.config.Condition != nil && message != nil {
					shouldContinue, err := a.config.Condition(ctx, message)
					if err != nil {
						yield(nil, err)
						return
					}
					if !shouldContinue {
						return
					}
				}
			}
		}
	}
}
