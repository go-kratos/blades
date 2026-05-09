package event

import "github.com/go-kratos/blades/content"

// TextDelta is a hot-path streaming text fragment.
type TextDelta struct {
	Text string
}

func (TextDelta) output() {}

// ThinkingDelta is a hot-path streaming thinking/reasoning fragment.
type ThinkingDelta struct {
	Text      string
	Signature []byte
}

func (ThinkingDelta) output() {}

// PartStart signals the beginning of a multimodal part in the stream.
type PartStart struct {
	Index int
	Part  content.Part
}

func (PartStart) output() {}

// PartDelta carries incremental data for a streaming multimodal part.
type PartDelta struct {
	Index int
	Data  []byte
}

func (PartDelta) output() {}

// PartEnd signals the completion of a multimodal part.
type PartEnd struct {
	Index int
	Part  content.Part
}

func (PartEnd) output() {}
