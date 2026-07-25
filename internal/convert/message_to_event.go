package convert

import (
	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/event"
	"github.com/go-kratos/blades/model"
)

// ChunkToOutputs converts a model.Chunk into output events.
func ChunkToOutputs(chunk *model.Chunk) []event.Output {
	var outputs []event.Output
	for _, p := range chunk.Parts {
		switch v := p.(type) {
		case content.Text:
			outputs = append(outputs, event.TextDelta{Text: v.Text})
		case content.Thinking:
			outputs = append(outputs, event.ThinkingDelta{Text: v.Text, Signature: v.Signature})
		}
	}
	return outputs
}

// ResponseToAssistantMessageEnd converts a model.Response into an
// AssistantMessageEnd event.
func ResponseToAssistantMessageEnd(resp *model.Response) event.AssistantMessageEnd {
	return event.AssistantMessageEnd{
		Parts:      resp.Message.Parts,
		StopReason: event.StopReason(resp.StopReason),
		Usage: event.Usage{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
		},
	}
}
