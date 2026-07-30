package event

import (
	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
)

// StopReason indicates why a model call stopped or its turn was aborted.
type StopReason string

const (
	StopEnd       StopReason = "end"
	StopToolUse   StopReason = "tool_use"
	StopMaxTokens StopReason = "max_tokens"
	StopSafety    StopReason = "safety"
	StopAbort     StopReason = "abort"
)

// TurnEnd signals completion of one primary model call and its tool wave.
type TurnEnd struct {
	Parts      []content.Part
	StopReason StopReason
	Usage      model.Usage
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
