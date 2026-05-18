package event

import "github.com/go-kratos/blades/content"

// Prompt starts a new turn with the given content parts.
type Prompt struct {
	Parts []content.Part
}

func (Prompt) input() {}

// Steer injects additional context or correction into the current run.
type Steer struct {
	Parts []content.Part
}

func (Steer) input() {}

// NewPrompt creates a Prompt from variadic inputs.
// Each input may be a string (auto-wrapped to content.Text) or any content.Part.
func NewPrompt(parts ...any) Prompt {
	return Prompt{Parts: content.NewParts(parts...)}
}

// NewSteer creates a Steer from variadic inputs.
// Each input may be a string (auto-wrapped to content.Text) or any content.Part.
func NewSteer(parts ...any) Steer {
	return Steer{Parts: content.NewParts(parts...)}
}
