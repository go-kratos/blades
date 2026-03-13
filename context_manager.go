package blades

import "context"

// TokenCounter estimates the number of tokens for a set of messages.
// Implementations may use model-specific tokenizers for precise counting,
// or rely on heuristic approximations.
type TokenCounter interface {
	Count(messages ...*Message) int64
}

// ContextManager manages the message context window before each model call.
// It is called by the agent loop before every model invocation, allowing
// strategies such as sliding window truncation or LLM-based summarization.
type ContextManager interface {
	// Prepare filters, truncates, or compresses messages to fit within the
	// context window. System/instruction content is handled separately via
	// ModelRequest.Instruction and is never passed to Prepare.
	Prepare(ctx context.Context, messages []*Message) ([]*Message, error)
}
