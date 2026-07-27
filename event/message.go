package event

import "github.com/go-kratos/blades/content"

// AssistantMessageEnd reports one completed assistant model response.
// The default Agent emits it after the response's tool wave has finished and
// before the corresponding TurnEnd.
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
