package flow

import (
	"context"
	"strings"

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

func loopVisibleMessage(message *blades.Message) bool {
	return message != nil &&
		message.Role == blades.RoleAssistant &&
		message.Status == blades.StatusCompleted &&
		strings.TrimSpace(message.Text()) != ""
}

func loopFinalMessage(lastVisible, lastOutput *blades.Message) *blades.Message {
	if lastVisible != nil {
		return lastVisible
	}
	return lastOutput
}

// Run runs the sub-agents in a loop. Internal sub-agent messages are buffered,
// and only the last visible assistant message from the final iteration is
// yielded outward. ExitTool signals are still honored based on each
// sub-agent's emitted actions. Context management across iterations is
// delegated to the ContextManager configured on the Runner
// (via blades.WithContextManager).
func (a *LoopAgent) Run(ctx context.Context, input *blades.Invocation) blades.Generator[*blades.Message, error] {
	return func(yield func(*blades.Message, error) bool) {
		state := LoopState{}
		var lastVisible *blades.Message
		for state.Iteration = 0; state.Iteration < a.config.MaxIterations; state.Iteration++ {
			exitRequested := false
			escalated := false
			var iterationOutput *blades.Message
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
					if loopVisibleMessage(message) {
						lastVisible = message
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
				iterationOutput = message
			}
			if a.config.Condition != nil {
				shouldContinue, err := a.config.Condition(ctx, state)
				if err != nil {
					if final := loopFinalMessage(lastVisible, iterationOutput); final != nil {
						if !yield(final, nil) {
							return
						}
					}
					yield(nil, err)
					return
				}
				if !shouldContinue {
					if final := loopFinalMessage(lastVisible, iterationOutput); final != nil {
						yield(final, nil)
					}
					return
				}
				continue
			}
			if exitRequested {
				if final := loopFinalMessage(lastVisible, iterationOutput); final != nil {
					if !yield(final, nil) {
						return
					}
				}
				if escalated {
					yield(nil, blades.ErrLoopEscalated)
					return
				}
				return
			}
		}
		if final := loopFinalMessage(lastVisible, state.Output); final != nil {
			yield(final, nil)
		}
	}
}
