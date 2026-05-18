package compact

import (
	"context"

	"github.com/go-kratos/blades/model"
)

// WindowOption configures a window compactor.
type WindowOption func(*windowCompactor)

// WithMaxMessages sets the maximum number of messages for window compaction.
func WithMaxMessages(n int) WindowOption {
	return func(w *windowCompactor) {
		w.maxMessages = n
	}
}

// WithMaxTokens sets the maximum token budget for window compaction.
func WithMaxTokens(n int64) WindowOption {
	return func(w *windowCompactor) {
		w.maxTokens = n
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
	counter     model.TokenCounter
}

func (w *windowCompactor) Compact(ctx context.Context, req Request) ([]*model.Message, error) {
	msgs := req.Messages
	if len(msgs) == 0 {
		return msgs, nil
	}
	groups, err := messageGroups(msgs)
	if err != nil {
		return nil, err
	}
	result := msgs
	if w.maxMessages > 0 && len(result) > w.maxMessages {
		start := retainLastMessages(groups, w.maxMessages)
		result = msgs[start:]
	}
	if w.maxTokens > 0 {
		counter := w.counter
		if counter == nil {
			counter = req.TokenCounter
		}
		for {
			tokens, err := countMessagesTokens(ctx, counter, result)
			if err != nil {
				return nil, err
			}
			if tokens <= w.maxTokens {
				break
			}
			resultGroups, err := messageGroups(result)
			if err != nil {
				return nil, err
			}
			if len(resultGroups) <= 1 {
				break
			}
			result = result[resultGroups[1].start:]
		}
	}
	return result, nil
}
