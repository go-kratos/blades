package flow

import (
	"context"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/tools"
)

// LoopPhase is the decision outcome returned by a LoopCondition.
type LoopPhase int

const (
	// PhaseContinue runs another iteration.
	PhaseContinue LoopPhase = iota
	// PhaseComplete stops the loop successfully.
	PhaseComplete
	// PhaseEscalate stops the loop and escalates to an outer handler.
	// The loop yields blades.ErrLoopEscalated to signal escalation.
	PhaseEscalate
)

// LoopState captures the observable state available to a LoopCondition.
type LoopState struct {
	// Iteration is the 0-based index of the iteration that just completed.
	Iteration int
	// Input is the original input message to the LoopAgent.
	Input *blades.Message
	// Output is the last message produced in the current iteration.
	Output *blades.Message
	// Phase is the implicit phase signal set by ExitTool. It is overridden by
	// the explicit phase returned from the LoopCondition.
	Phase LoopPhase
}

// LoopCondition is called once after every complete iteration.
// It inspects the accumulated LoopState and returns the next LoopPhase.
type LoopCondition func(ctx context.Context, state LoopState) (LoopPhase, error)

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

// Run runs the sub-agents in a loop. After each message yielded by a sub-agent
// the loop checks message.Actions for an ActionLoopExit signal set by ExitTool.
// Context management across iterations is delegated to the ContextManager
// configured on the Runner (via blades.WithContextManager).
func (a *loopAgent) Run(ctx context.Context, input *blades.Invocation) blades.Generator[*blades.Message, error] {
	return func(yield func(*blades.Message, error) bool) {
		state := LoopState{}
		for state.Iteration = 0; state.Iteration < a.config.MaxIterations; state.Iteration++ {
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
						if ei, ok := exit.(tools.ExitInput); ok {
							if ei.Escalate {
								state.Phase = PhaseEscalate
							} else {
								state.Phase = PhaseComplete
							}
						}
					}
				}
				state.Input = invocation.Message
				state.Output = message
			}
			if a.config.Condition != nil {
				phase, err := a.config.Condition(ctx, state)
				if err != nil {
					yield(nil, err)
					return
				}
				switch phase {
				case PhaseComplete:
					return
				case PhaseEscalate:
					yield(nil, blades.ErrLoopEscalated)
					return
				}
				// Condition returned PhaseContinue: ignore any ExitTool signal.
			} else {
				switch state.Phase {
				case PhaseComplete:
					return
				case PhaseEscalate:
					yield(nil, blades.ErrLoopEscalated)
					return
				}
			}
		}
	}
}
