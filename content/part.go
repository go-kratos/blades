package content

import "bytes"

// Part is the sealed interface for all multimodal content types.
// It is shared across event, model, and tools packages.
type Part interface {
	part()
}

// Coalesce merges adjacent same-kind parts produced by streaming into single
// parts, returning a new slice. Streaming providers emit one Text/Thinking part
// per delta, which otherwise persists into history and re-serializes as many
// small provider content blocks. Coalescing here keeps the assembled message
// compact so downstream requests carry one block per contiguous run.
//
// Adjacent Text parts are concatenated. Adjacent Thinking parts are
// concatenated when their Signatures are equal. An unsigned Thinking run is
// also finalized by a following signed fragment because some providers stream
// reasoning text before its verification signature. Once a run is signed, a
// following unsigned fragment starts a new block, so verified reasoning blocks
// are never fused across signature boundaries. All other parts, and any
// non-adjacent runs, are preserved in order.
func Coalesce(parts []Part) []Part {
	if len(parts) == 0 {
		return nil
	}
	out := make([]Part, 0, len(parts))
	for _, part := range parts {
		switch p := part.(type) {
		case Text:
			if len(out) > 0 {
				if prev, ok := out[len(out)-1].(Text); ok {
					out[len(out)-1] = Text{Text: prev.Text + p.Text}
					continue
				}
			}
		case Thinking:
			if len(out) > 0 {
				if prev, ok := out[len(out)-1].(Thinking); ok {
					switch {
					case bytes.Equal(prev.Signature, p.Signature):
						out[len(out)-1] = Thinking{Text: prev.Text + p.Text, Signature: prev.Signature}
						continue
					case len(prev.Signature) == 0 && len(p.Signature) > 0:
						out[len(out)-1] = Thinking{Text: prev.Text + p.Text, Signature: p.Signature}
						continue
					}
				}
			}
		}
		out = append(out, part)
	}
	return out
}

// NewParts converts a heterogeneous list of inputs into Parts.
// Accepted types: string (wrapped as Text) and any content.Part implementation.
// Unrecognized types are silently skipped.
func NewParts(inputs ...any) []Part {
	parts := make([]Part, 0, len(inputs))
	for _, input := range inputs {
		switch v := input.(type) {
		case string:
			parts = append(parts, Text{Text: v})
		case Part:
			parts = append(parts, v)
		}
	}
	return parts
}
