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
	// ExitRequested is true when ExitTool was invoked during this iteration.
	ExitRequested bool
	// Escalated is true when ExitTool was invoked with Escalate=true.
	Escalated bool
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

// loopAgent is an agent that runs sub-agents in a loop.
type loopAgent struct {
	config LoopConfig
}

// NewLoopAgent creates a new LoopAgent.
func NewLoopAgent(config LoopConfig) blades.Agent {
	if config.MaxIterations <= 0 {
		config.MaxIterations = 10
	}
	return &loopAgent{config: config}
}

func (a *loopAgent) Name() string        { return a.config.Name }
func (a *loopAgent) Description() string { return a.config.Description }

func loopExitInput(action any) (*tools.ExitInput, bool) {
	switch v := action.(type) {
	case *tools.ExitInput:
		return v, true
	case tools.ExitInput:
		exit := v
		return &exit, true
	default:
		return nil, false
	}
}

// Run runs the sub-agents in a loop. After each message yielded by a sub-agent
// the loop checks message.Actions for an ActionLoopExit signal set by ExitTool.
// Context management across iterations is delegated to the ContextManager
// configured on the Runner (via blades.WithContextManager).
func (a *loopAgent) Run(ctx context.Context, input *blades.Invocation) blades.Generator[*blades.Message, error] {
	return func(yield func(*blades.Message, error) bool) {
		state := LoopState{}
		for state.Iteration = 0; state.Iteration < a.config.MaxIterations; state.Iteration++ {
			state.ExitRequested = false
			state.Escalated = false
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
						if exitInput, ok := loopExitInput(exit); ok {
							state.ExitRequested = true
							state.Escalated = exitInput.Escalate
						}
					}
				}
				state.Input = invocation.Message
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
			if state.ExitRequested {
				if state.Escalated {
					yield(nil, blades.ErrLoopEscalated)
					return
				}
				return
			}
		}
	}
}
