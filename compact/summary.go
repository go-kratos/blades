package compact

import (
	"context"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
)

// SummarizeOption configures a summarize compactor.
type SummarizeOption func(*summarizeCompactor)

// WithKeepRecentMessages sets the minimum number of recent raw messages to keep.
func WithKeepRecentMessages(n int) SummarizeOption {
	return func(s *summarizeCompactor) {
		s.keepRecentMessages = n
	}
}

// WithKeepRecentTokens sets the target token budget for recent raw messages.
func WithKeepRecentTokens(n int64) SummarizeOption {
	return func(s *summarizeCompactor) {
		s.keepRecentTokens = n
	}
}

// WithSummarizer sets the summarizer used for compaction.
func WithSummarizer(sum Summarizer) SummarizeOption {
	return func(s *summarizeCompactor) {
		s.summarizer = sum
	}
}

// WithTokenCounter sets the token counter used by the summarize compactor.
func WithTokenCounter(tc model.TokenCounter) SummarizeOption {
	return func(s *summarizeCompactor) {
		s.counter = tc
	}
}

// WithMaxSummaryTokens sets the maximum token budget for the generated summary.
func WithMaxSummaryTokens(n int64) SummarizeOption {
	return func(s *summarizeCompactor) {
		s.maxSummaryTokens = n
	}
}

// NewSummarize creates a compactor that summarizes older messages and keeps
// recent ones verbatim. On each Compact call it summarizes everything before
// the recent window and returns [summaryMsg, ...recentMsgs].
func NewSummarize(opts ...SummarizeOption) Compactor {
	s := &summarizeCompactor{
		keepRecentMessages: 20,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

type summarizeCompactor struct {
	keepRecentMessages int
	keepRecentTokens   int64
	maxSummaryTokens   int64
	counter            model.TokenCounter
	summarizer         Summarizer
}

func (s *summarizeCompactor) Compact(ctx context.Context, req Request) ([]*model.Message, error) {
	msgs := req.Messages
	if len(msgs) == 0 {
		return msgs, nil
	}
	groups, err := messageGroups(msgs)
	if err != nil {
		return nil, err
	}

	recentStart := s.recentStart(ctx, req.TokenCounter, msgs, groups)

	// Nothing to summarize — all messages are in the recent window.
	if recentStart == 0 {
		return msgs, nil
	}

	toSummarize := msgs[:recentStart]
	recentMsgs := msgs[recentStart:]

	if s.summarizer == nil {
		// No summarizer: just drop old messages.
		return recentMsgs, nil
	}

	summary, err := s.summarizer.Summarize(ctx, SummaryRequest{
		Messages:  toSummarize,
		MaxTokens: s.maxSummaryTokens,
	})
	if err != nil {
		return nil, err
	}

	result := make([]*model.Message, 0, 1+len(recentMsgs))
	result = append(result, summaryMessage(summary))
	result = append(result, recentMsgs...)
	return result, nil
}

func (s *summarizeCompactor) recentStart(ctx context.Context, reqCounter model.TokenCounter, msgs []*model.Message, groups []messageGroup) int {
	start := len(msgs)
	if s.keepRecentMessages > 0 {
		start = retainLastMessages(groups, s.keepRecentMessages)
	}
	if s.keepRecentTokens > 0 {
		counter := s.counter
		if counter == nil {
			counter = reqCounter
		}
		for start < len(msgs) {
			tokens, err := countMessagesTokens(ctx, counter, msgs[start:])
			if err != nil || tokens <= s.keepRecentTokens {
				break
			}
			next := nextGroupStart(groups, start)
			if next <= start || next >= len(msgs) {
				break
			}
			start = next
		}
	}
	return start
}

func summaryMessage(text string) *model.Message {
	return &model.Message{
		Role:  model.RoleUser,
		Parts: []content.Part{content.Text{Text: "[Conversation summary]\n" + text}},
	}
}

func nextGroupStart(groups []messageGroup, start int) int {
	for i, group := range groups {
		if group.start == start && i+1 < len(groups) {
			return groups[i+1].start
		}
	}
	return lenFromGroups(groups)
}

func lenFromGroups(groups []messageGroup) int {
	if len(groups) == 0 {
		return 0
	}
	return groups[len(groups)-1].end
}
