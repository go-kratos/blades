package compact

import (
	"context"

	"github.com/go-kratos/blades/model"
)

// NewWindow creates a sliding window compactor.
func NewWindow(opts ...Option) Compactor {
	cfg := options{
		maxMessages: 100,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	w := &windowCompactor{
		maxMessages: cfg.maxMessages,
		maxTokens:   cfg.maxTokens,
		counter:     cfg.counter,
	}
	return w
}

type windowCompactor struct {
	maxMessages int
	maxTokens   int64
	counter     MessageTokenCounter
}

func (w *windowCompactor) Compact(ctx context.Context, msgs []*model.Message) ([]*model.Message, error) {
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
	if w.maxTokens > 0 && w.counter != nil {
		for {
			tokens, err := w.counter.CountMessages(ctx, result...)
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
