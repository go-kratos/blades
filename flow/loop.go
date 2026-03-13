package flow

import (
	"context"
	"errors"

	"github.com/go-kratos/blades"
)

// ErrLoopEscalated is returned when a LoopCondition returns PhaseEscalate.
var ErrLoopEscalated = errors.New("loop: escalated by condition")

// LoopPhase is the decision outcome returned by a LoopCondition.
type LoopPhase int

const (
	// PhaseContinue runs another iteration.
	PhaseContinue LoopPhase = iota
	// PhaseComplete stops the loop successfully.
	PhaseComplete
	// PhaseEscalate stops the loop and returns ErrLoopEscalated.
	PhaseEscalate
)

// LoopState captures the accumulated context available to a LoopCondition.
type LoopState struct {
	// Iteration is the 0-based index of the iteration that just completed.
	Iteration int
	// Output is the last message produced in the current iteration.
	Output *blades.Message
	// History holds every message emitted across all iterations, in order.
	History []*blades.Message
}

// LoopCondition is called once after every complete iteration.
// It inspects the accumulated LoopState and returns the next LoopPhase.
type LoopCondition func(ctx context.Context, state LoopState) (LoopPhase, error)

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
		config.MaxIterations = 10
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

// Run runs the sub-agents in a loop, evaluating the condition once per
// complete iteration (after all sub-agents have run).
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
					state.History = append(state.History, message)
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
					yield(nil, ErrLoopEscalated)
					return
				}
			}
		}
	}
}
