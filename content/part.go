package content

// Part is the sealed interface for all multimodal content types.
// It is shared across event, model, and tools packages.
type Part interface {
	part()
}
