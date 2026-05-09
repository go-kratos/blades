package compact

import (
	"context"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
)

// Summarizer is a function that summarizes messages into a text summary.
type Summarizer func(ctx context.Context, msgs []*model.Message) (string, error)

// SummarizeOption configures the summarize compactor.
type SummarizeOption func(*summarizeCompactor)

// WithKeepRecent sets how many recent messages to preserve unsummarized.
func WithKeepRecent(n int) SummarizeOption {
	return func(s *summarizeCompactor) {
		s.keepRecent = n
	}
}

// NewSummarize creates a compactor that uses an LLM to summarize older messages.
func NewSummarize(summarizer Summarizer, opts ...SummarizeOption) Compactor {
	s := &summarizeCompactor{
		summarizer: summarizer,
		keepRecent: 4,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

type summarizeCompactor struct {
	summarizer Summarizer
	keepRecent int
}

func (s *summarizeCompactor) Compact(ctx context.Context, msgs []*model.Message) ([]*model.Message, error) {
	if len(msgs) <= s.keepRecent {
		return msgs, nil
	}
	toSummarize := msgs[:len(msgs)-s.keepRecent]
	recent := msgs[len(msgs)-s.keepRecent:]

	summary, err := s.summarizer(ctx, toSummarize)
	if err != nil {
		return msgs, nil
	}

	summaryMsg := &model.Message{
		Role:  model.RoleUser,
		Parts: []content.Part{content.Text{Text: "[Summary of previous conversation]\n" + summary}},
	}
	result := make([]*model.Message, 0, 1+len(recent))
	result = append(result, summaryMsg)
	result = append(result, recent...)
	return result, nil
}
