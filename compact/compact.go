package compact

import (
	"context"

	"github.com/go-kratos/blades/model"
)

// Compactor transforms a message slice to fit within context budget.
type Compactor interface {
	Compact(ctx context.Context, req Request) ([]*model.Message, error)
}

// Request is the runtime input to a compactor.
type Request struct {
	Messages     []*model.Message
	TokenCounter model.TokenCounter
}

// CompactorFunc is a function adapter for Compactor.
type CompactorFunc func(ctx context.Context, req Request) ([]*model.Message, error)

func (f CompactorFunc) Compact(ctx context.Context, req Request) ([]*model.Message, error) {
	return f(ctx, req)
}

// Chain composes multiple compactors in sequence.
func Chain(cs ...Compactor) Compactor {
	return CompactorFunc(func(ctx context.Context, req Request) ([]*model.Message, error) {
		var err error
		msgs := req.Messages
		for _, c := range cs {
			if c == nil {
				continue
			}
			req.Messages = msgs
			msgs, err = c.Compact(ctx, req)
			if err != nil {
				return nil, err
			}
		}
		return msgs, nil
	})
}

// Summarizer summarizes model messages into compact text.
type Summarizer interface {
	Summarize(ctx context.Context, req SummaryRequest) (string, error)
}

// SummaryRequest is the input to a Summarizer.
type SummaryRequest struct {
	Messages  []*model.Message
	MaxTokens int64
}

// SummarizerFunc adapts a function into a Summarizer.
type SummarizerFunc func(ctx context.Context, req SummaryRequest) (string, error)

// Summarize implements Summarizer.
func (f SummarizerFunc) Summarize(ctx context.Context, req SummaryRequest) (string, error) {
	return f(ctx, req)
}

func countMessagesTokens(ctx context.Context, counter model.TokenCounter, msgs []*model.Message) (int64, error) {
	if counter == nil {
		counter = model.ApproxTokenCounter{}
	}
	count, err := counter.CountTokens(ctx, &model.Request{Messages: msgs})
	if err != nil {
		return 0, err
	}
	if count.HasSegments() {
		return count.Messages, nil
	}
	if count.Messages > 0 {
		return count.Messages, nil
	}
	return count.Total(), nil
}
