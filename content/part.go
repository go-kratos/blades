package content

// Part is the sealed interface for all multimodal content types.
// It is shared across event, model, and tools packages.
type Part interface {
	part()
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
