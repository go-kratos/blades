package model

import (
	"iter"

	"github.com/go-kratos/blades/content"
)

// Collect accumulates a streaming response into a complete Response.
func Collect(seq iter.Seq2[*Chunk, error]) (*Response, error) {
	var (
		parts      []content.Part
		stopReason StopReason
		usage      Usage
	)
	for chunk, err := range seq {
		if err != nil {
			return nil, err
		}
		if chunk == nil {
			continue
		}
		parts = append(parts, chunk.Parts...)
		if chunk.StopReason != "" {
			stopReason = chunk.StopReason
		}
		if chunk.Usage != nil {
			usage.InputTokens += chunk.Usage.InputTokens
			usage.OutputTokens += chunk.Usage.OutputTokens
		}
	}
	return &Response{
		Message:    &Message{Role: RoleAssistant, Parts: parts},
		StopReason: stopReason,
		Usage:      usage,
	}, nil
}
