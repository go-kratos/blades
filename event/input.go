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

// NewPromptText creates a Prompt with a single text part.
func NewPromptText(s string) Prompt {
	return Prompt{Parts: []content.Part{content.Text{Text: s}}}
}

// NewSteerText creates a Steer with a single text part.
func NewSteerText(s string) Steer {
	return Steer{Parts: []content.Part{content.Text{Text: s}}}
}
