package model

import "github.com/go-kratos/blades/content"

// Response is the complete result of a non-streaming model call.
type Response struct {
	Message    *Message
	StopReason StopReason
	Usage      Usage
}

// Chunk is an incremental frame from a streaming model call.
type Chunk struct {
	Parts      []content.Part
	StopReason StopReason
	Usage      *Usage
}
