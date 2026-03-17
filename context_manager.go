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

type ctxManagerKey struct{}

// NewContextManagerContext returns a child context carrying m.
// Called by Runner to inject the configured Manager into the execution context.
func NewContextManagerContext(ctx context.Context, m ContextManager) context.Context {
	return context.WithValue(ctx, ctxManagerKey{}, m)
}

// ContextManagerFromContext retrieves the Manager stored in ctx by the Runner.
func ContextManagerFromContext(ctx context.Context) (ContextManager, bool) {
	m, ok := ctx.Value(ctxManagerKey{}).(ContextManager)
	return m, ok
}
