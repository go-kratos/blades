package compact

import (
	"context"
	"errors"

	"github.com/go-kratos/blades/model"
)

// Compactor transforms a message slice to fit within context budget.
type Compactor interface {
	Compact(ctx context.Context, msgs []*model.Message) ([]*model.Message, error)
}

// CompactorFunc is a function adapter for Compactor.
type CompactorFunc func(ctx context.Context, msgs []*model.Message) ([]*model.Message, error)

func (f CompactorFunc) Compact(ctx context.Context, msgs []*model.Message) ([]*model.Message, error) {
	return f(ctx, msgs)
}

// Chain composes multiple compactors in sequence.
func Chain(cs ...Compactor) Compactor {
	return CompactorFunc(func(ctx context.Context, msgs []*model.Message) ([]*model.Message, error) {
		var err error
		for _, c := range cs {
			if c == nil {
				continue
			}
			msgs, err = c.Compact(ctx, msgs)
			if err != nil {
				return nil, err
			}
		}
		return msgs, nil
	})
}

// MessageTokenCounter counts tokens for a message view.
type MessageTokenCounter interface {
	CountMessages(ctx context.Context, msgs ...*model.Message) (int64, error)
}

// MessageTokenCounterFunc adapts a function into a MessageTokenCounter.
type MessageTokenCounterFunc func(ctx context.Context, msgs ...*model.Message) (int64, error)

// CountMessages implements MessageTokenCounter.
func (f MessageTokenCounterFunc) CountMessages(ctx context.Context, msgs ...*model.Message) (int64, error) {
	return f(ctx, msgs...)
}

// SummaryRequest is the input to a Summarizer.
type SummaryRequest struct {
	Messages  []*model.Message
	MaxTokens int64
}

// Summarizer summarizes model messages into compact text.
type Summarizer interface {
	Summarize(ctx context.Context, req SummaryRequest) (string, error)
}

// SummarizerFunc adapts a function into a Summarizer.
type SummarizerFunc func(ctx context.Context, req SummaryRequest) (string, error)

// Summarize implements Summarizer.
func (f SummarizerFunc) Summarize(ctx context.Context, req SummaryRequest) (string, error) {
	return f(ctx, req)
}

type options struct {
	maxMessages          int
	maxTokens            int64
	messagesBudget       int64
	keepRecentMessages   int
	keepRecentTokens     int64
	summaryBlockTokens   int64
	maxSummaryBlocks     int
	summaryBatchMessages int
	maxFoldIterations    int
	counter              MessageTokenCounter
	summarizer           Summarizer
}

// Option configures compactors.
type Option func(*options)

// WithMaxMessages sets the maximum number of messages for window compaction.
func WithMaxMessages(n int) Option {
	return func(o *options) {
		o.maxMessages = n
	}
}

// WithMaxTokens sets the maximum token budget for window compaction.
func WithMaxTokens(n int64) Option {
	return func(o *options) {
		o.maxTokens = n
	}
}

// WithMessageTokenCounter sets the token counter used by budgeted compactors.
func WithMessageTokenCounter(tc MessageTokenCounter) Option {
	return func(o *options) {
		o.counter = tc
	}
}

// WithMessagesBudget sets the target message-view budget for block summarization.
func WithMessagesBudget(n int64) Option {
	return func(o *options) {
		o.messagesBudget = n
	}
}

// WithKeepRecentMessages sets the minimum number of recent raw messages to keep.
func WithKeepRecentMessages(n int) Option {
	return func(o *options) {
		o.keepRecentMessages = n
	}
}

// WithKeepRecentTokens sets the target token budget for recent raw messages.
func WithKeepRecentTokens(n int64) Option {
	return func(o *options) {
		o.keepRecentTokens = n
	}
}

// WithSummaryBlockTokens sets the target token budget for each summary block.
func WithSummaryBlockTokens(n int64) Option {
	return func(o *options) {
		o.summaryBlockTokens = n
	}
}

// WithMaxSummaryBlocks sets the maximum number of summary blocks to keep before merging.
func WithMaxSummaryBlocks(n int) Option {
	return func(o *options) {
		o.maxSummaryBlocks = n
	}
}

// WithSummaryBatchMessages sets the maximum number of raw messages folded into one summary block.
func WithSummaryBatchMessages(n int) Option {
	return func(o *options) {
		o.summaryBatchMessages = n
	}
}

// WithMaxFoldIterations sets the maximum number of folds per Compact call.
func WithMaxFoldIterations(n int) Option {
	return func(o *options) {
		o.maxFoldIterations = n
	}
}

// WithSummarizer sets the summarizer used by block summarization.
func WithSummarizer(s Summarizer) Option {
	return func(o *options) {
		o.summarizer = s
	}
}

// NewModelMessageTokenCounter adapts a full request token counter for
// message-only compaction decisions.
func NewModelMessageTokenCounter(counter model.TokenCounter) MessageTokenCounter {
	return modelMessageTokenCounter{counter: counter}
}

type modelMessageTokenCounter struct {
	counter model.TokenCounter
}

func (c modelMessageTokenCounter) CountMessages(ctx context.Context, msgs ...*model.Message) (int64, error) {
	if c.counter == nil {
		return 0, errors.New("compact: model token counter is required")
	}
	count, err := c.counter.CountTokens(ctx, &model.Request{Messages: msgs})
	if err != nil {
		return 0, err
	}
	if count.HasBreakdown || count.MessagesTokens > 0 {
		return count.MessagesTokens, nil
	}
	return count.InputTokens, nil
}
