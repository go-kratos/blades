package blades

import "context"

// TokenCounter estimates the number of tokens for a set of messages.
// Implementations may use model-specific tokenizers for precise counting,
// or rely on heuristic approximations.
type TokenCounter interface {
	Count(messages ...*Message) int64
}

// Compressor compresses, truncates, or filters the message list before each
// model call to keep it within the context window budget.
type Compressor interface {
	// Compress filters, truncates, or compresses messages to fit within the
	// context window. System/instruction content is handled separately via
	// ModelRequest.Instruction and is never passed to Compress.
	Compress(ctx context.Context, messages []*Message) ([]*Message, error)
}
