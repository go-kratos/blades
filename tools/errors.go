package tools

import "errors"

// ErrLoopExit is returned by a tool to signal loop termination.
type ErrLoopExit struct {
	Escalate bool
}

func (e *ErrLoopExit) Error() string {
	if e.Escalate {
		return "tools: loop exit (escalate)"
	}
	return "tools: loop exit"
}

// ErrHandoff is returned by a tool to signal delegation to another agent.
type ErrHandoff struct {
	Agent string
}

func (e *ErrHandoff) Error() string {
	return "tools: handoff to " + e.Agent
}

// IsLoopExit checks if an error is a loop exit signal.
func IsLoopExit(err error) (*ErrLoopExit, bool) {
	var e *ErrLoopExit
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// IsHandoff checks if an error is a handoff signal.
func IsHandoff(err error) (*ErrHandoff, bool) {
	var e *ErrHandoff
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}
