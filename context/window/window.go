package window

import (
	"context"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/internal/counter"
)

const defaultMaxMessages = 100

// Option configures a window ContextManager.
type Option func(*contextManager)

// WithMaxMessages sets the maximum number of messages to retain.
// Oldest messages are dropped first when the limit is exceeded.
// Default is 100. Set to 0 to disable message count limiting.
func WithMaxMessages(n int) Option {
	return func(c *contextManager) {
		c.maxMessages = n
	}
}

// WithMaxTokens sets the maximum total token budget.
// Messages are dropped from the front until the budget is met.
// Default is 0 (no limit).
func WithMaxTokens(tokens int64) Option {
	return func(c *contextManager) {
		c.maxTokens = tokens
	}
}

// WithTokenCounter sets the TokenCounter used to estimate token usage.
// Defaults to a character-based counter (1 token ≈ 4 chars).
func WithTokenCounter(counter blades.TokenCounter) Option {
	return func(c *contextManager) {
		c.counter = counter
	}
}

type contextManager struct {
	maxMessages int
	maxTokens   int64
	counter     blades.TokenCounter
}

// NewContextManager returns a ContextManager that keeps the most recent
// messages within the configured token or message count limits. Messages are
// dropped from the front (oldest first) when limits are exceeded.
func NewContextManager(opts ...Option) blades.ContextManager {
	cm := &contextManager{
		maxMessages: defaultMaxMessages,
		counter:     counter.NewCharBasedCounter(),
	}
	for _, opt := range opts {
		opt(cm)
	}
	if cm.counter == nil {
		cm.counter = counter.NewCharBasedCounter()
	}
	return cm
}

// Prepare retains the most recent messages that fit the configured limits.
func (w *contextManager) Prepare(_ context.Context, messages []*blades.Message) ([]*blades.Message, error) {
	if len(messages) == 0 {
		return messages, nil
	}

	result := messages
	if w.maxMessages > 0 && len(result) > w.maxMessages {
		result = result[len(result)-w.maxMessages:]
	}

	if w.maxTokens > 0 {
		total := w.counter.Count(result...)
		for len(result) > 1 && total > w.maxTokens {
			total -= w.counter.Count(result[0])
			result = result[1:]
		}
	}

	return result, nil
}
