package compact

import (
	"context"

	"github.com/go-kratos/blades/model"
)

// WindowOption configures the window compactor.
type WindowOption func(*windowCompactor)

// WithMaxMessages sets the maximum number of messages to retain.
func WithMaxMessages(n int) WindowOption {
	return func(w *windowCompactor) {
		w.maxMessages = n
	}
}

// WithMaxTokens sets the maximum token budget.
func WithMaxTokens(n int64) WindowOption {
	return func(w *windowCompactor) {
		w.maxTokens = n
	}
}

// WithTokenCounter sets the token counter for budget calculation.
func WithTokenCounter(tc TokenCounter) WindowOption {
	return func(w *windowCompactor) {
		w.counter = tc
	}
}

// NewWindow creates a sliding window compactor.
func NewWindow(opts ...WindowOption) Compactor {
	w := &windowCompactor{
		maxMessages: 100,
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

type windowCompactor struct {
	maxMessages int
	maxTokens   int64
	counter     TokenCounter
}

func (w *windowCompactor) Compact(_ context.Context, msgs []*model.Message) ([]*model.Message, error) {
	if len(msgs) == 0 {
		return msgs, nil
	}
	result := msgs
	if w.maxMessages > 0 && len(result) > w.maxMessages {
		result = result[len(result)-w.maxMessages:]
	}
	if w.maxTokens > 0 && w.counter != nil {
		for len(result) > 1 {
			if w.counter.Count(result...) <= w.maxTokens {
				break
			}
			result = result[1:]
		}
	}
	return result, nil
}
