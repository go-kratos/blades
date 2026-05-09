package event

import "github.com/go-kratos/blades/content"

// StopReason indicates why a model step or turn ended.
type StopReason string

const (
	StopEnd       StopReason = "end"
	StopToolUse   StopReason = "tool_use"
	StopMaxTokens StopReason = "max_tokens"
	StopSafety    StopReason = "safety"
	StopAbort     StopReason = "abort"
)

// Usage tracks token consumption.
type Usage struct {
	InputTokens  int64
	OutputTokens int64
}

// StepEnd signals the completion of a single model step.
type StepEnd struct {
	Index      int
	StopReason StopReason
	Usage      Usage
}

func (StepEnd) output() {}

// TurnEnd signals the completion of a full turn (one or more steps).
type TurnEnd struct {
	Parts      []content.Part
	StopReason StopReason
	Usage      Usage
	Err        error
}

func (TurnEnd) output() {}

// Error carries a runtime error in the output stream.
type Error struct {
	Err error
}

func (Error) output() {}

// Done is the terminal sentinel emitted before the output channel closes.
type Done struct{}

func (Done) output() {}
