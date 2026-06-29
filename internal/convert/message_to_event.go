package convert

import (
	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/event"
	"github.com/go-kratos/blades/model"
)

// ChunkToOutputs converts a model.Chunk into output events.
func ChunkToOutputs(chunk *model.Chunk) []event.Output {
	if chunk == nil {
		return nil
	}
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
	var parts []content.Part
	var stopReason model.StopReason
	var usage model.Usage
	if resp != nil && resp.Message != nil {
		parts = resp.Message.Parts
	}
	if resp != nil {
		stopReason = resp.StopReason
		usage = resp.Usage
	}
	return event.AssistantMessageEnd{
		Parts:      parts,
		StopReason: event.StopReason(stopReason),
		Usage:      event.Usage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens},
	}
}

// ResponseToTurnEnd converts a model.Response into a TurnEnd event.
func ResponseToTurnEnd(resp *model.Response) event.TurnEnd {
	var parts []content.Part
	if resp.Message != nil {
		parts = resp.Message.Parts
	}
	return event.TurnEnd{
		Parts:      parts,
		StopReason: event.StopReason(resp.StopReason),
		Usage:      event.Usage{InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens},
	}
}
