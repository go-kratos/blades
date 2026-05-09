package event

// Action is the sealed interface for tool-triggered control actions.
// Actions are carried in TurnEnd.Action and consumed by flow-level agents.
type Action interface {
	action()
}

// LoopExit signals that a tool requested loop termination.
type LoopExit struct {
	Escalate bool
}

func (LoopExit) action() {}

// Handoff signals that a tool requested delegation to another agent.
type Handoff struct {
	Agent string
}

func (Handoff) action() {}
