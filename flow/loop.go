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

// LoopState captures the accumulated context available to a LoopCondition.
// A pointer to the current LoopState is stored in the context for every
// iteration so that tools (e.g. tools.ExitTool) can signal loop exit.
type LoopState struct {
	// Iteration is the 0-based index of the iteration that just completed.
	Iteration int
	// Input is the original input message to the LoopAgent.
	Input *blades.Message
	// Output is the last message produced in the current iteration.
	Output *blades.Message
	phase  LoopPhase
	reason string
}

// ExitLoop implements tools.LoopExiter. It is called by tools running inside
// an iteration to signal that the loop should stop.
func (s *LoopState) ExitLoop(reason string, escalate bool) {
	s.reason = reason
	if escalate {
		s.phase = PhaseEscalate
	} else {
		s.phase = PhaseComplete
	}
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

// Run runs the sub-agents in a loop. Before each iteration the current
// *LoopState is stored in the context via tools.WithLoopExiter so that
// tools like tools.ExitTool can signal exit without needing a LoopConfig field.
// The LoopCondition (if set) is evaluated after each iteration.
func (a *loopAgent) Run(ctx context.Context, input *blades.Invocation) blades.Generator[*blades.Message, error] {
	return func(yield func(*blades.Message, error) bool) {
		state := LoopState{}
		for state.Iteration = 0; state.Iteration < a.config.MaxIterations; state.Iteration++ {
			state.phase = PhaseContinue
			iterCtx := tools.WithLoopExiter(ctx, &state)
			for _, agent := range a.config.SubAgents {
				var (
					err        error
					message    *blades.Message
					invocation = input.Clone()
				)
				state.Input = invocation.Message
				for message, err = range agent.Run(iterCtx, invocation) {
					if err != nil {
						yield(nil, err)
						return
					}
					if !yield(message, nil) {
						return
					}
				}
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
			}
			switch state.phase {
			case PhaseComplete:
				return
			case PhaseEscalate:
				yield(nil, blades.ErrLoopEscalated)
				return
			}
		}
	}
}
