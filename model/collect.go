package model

import (
	"iter"

	"github.com/go-kratos/blades/content"
)

// Collect accumulates a streaming response into a complete Response. It adopts
// the last reported usage snapshot and the last non-empty stop reason.
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
			usage = *chunk.Usage
		}
	}
	return &Response{
		Message:    &Message{Role: RoleAssistant, Parts: content.Coalesce(parts)},
		StopReason: stopReason,
		Usage:      usage,
	}, nil
}
