package event

import "github.com/go-kratos/blades/content"

// AssistantMessageEnd signals completion of one assistant model response.
type AssistantMessageEnd struct {
	Parts      []content.Part
	StopReason StopReason
	Usage      Usage
}

// Text returns the concatenated text parts from the assistant message.
func (e AssistantMessageEnd) Text() string {
	return content.TextFromParts(e.Parts)
}

func (AssistantMessageEnd) output() {}
