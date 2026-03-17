package flow

import (
	"context"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/tools"
)

// LoopState captures the observable state available to a LoopCondition.
type LoopState struct {
	// Iteration is the 0-based index of the iteration that just completed.
	Iteration int
	// Input is the original input message to the LoopAgent.
	Input *blades.Message
	// Output is the last message produced in the current iteration.
	Output *blades.Message
}

// LoopCondition is called once after every complete iteration.
// Return true to run another iteration, false to stop normally,
// or a non-nil error (e.g. blades.ErrLoopEscalated) to abort with an error.
type LoopCondition func(ctx context.Context, state LoopState) (bool, error)

// LoopConfig is the configuration for a LoopAgent.
type LoopConfig struct {
	Name          string
	Description   string
	MaxIterations int
	// Condition is evaluated after every iteration. It takes priority over ExitTool signals.
	Condition LoopCondition
	SubAgents []blades.Agent
}

// LoopAgent is an agent that runs sub-agents in a loop.
type LoopAgent struct {
	config LoopConfig
}

// NewLoopAgent creates a new LoopAgent.
func NewLoopAgent(config LoopConfig) blades.Agent {
	if config.MaxIterations <= 0 {
		config.MaxIterations = 10
	}
	return &LoopAgent{config: config}
}

func (a *LoopAgent) Name() string        { return a.config.Name }
func (a *LoopAgent) Description() string { return a.config.Description }

// Run runs the sub-agents in a loop. After each message yielded by a sub-agent
// the loop checks message.Actions for an ActionLoopExit signal set by ExitTool.
// Context management across iterations is delegated to the ContextManager
// configured on the Runner (via blades.WithContextManager).
func (a *LoopAgent) Run(ctx context.Context, input *blades.Invocation) blades.Generator[*blades.Message, error] {
	return func(yield func(*blades.Message, error) bool) {
		state := LoopState{}
		for state.Iteration = 0; state.Iteration < a.config.MaxIterations; state.Iteration++ {
			exitRequested := false
			escalated := false
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
					if exit, ok := message.Actions[tools.ActionLoopExit]; ok {
						if exitEscalated, ok := exit.(bool); ok {
							exitRequested = true
							escalated = exitEscalated
						}
					}
				}
				state.Input = input.Message
				state.Output = message
			}
			if a.config.Condition != nil {
				shouldContinue, err := a.config.Condition(ctx, state)
				if err != nil {
					yield(nil, err)
					return
				}
				if !shouldContinue {
					return
				}
				continue
			}
			if exitRequested {
				if escalated {
					yield(nil, blades.ErrLoopEscalated)
					return
				}
				return
			}
		}
	}
}
