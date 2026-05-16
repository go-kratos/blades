package event

import "github.com/go-kratos/blades/content"

// StopReason indicates why a turn ended.
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

// TurnEnd signals the completion of a full turn (one or more steps).
type TurnEnd struct {
	Parts      []content.Part
	StopReason StopReason
	Usage      Usage
	Err        error
	Action     Action
}

// Text returns the concatenated text parts from the turn.
func (e TurnEnd) Text() string {
	return content.TextFromParts(e.Parts)
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
