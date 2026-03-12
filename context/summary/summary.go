package summary

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-kratos/blades"
)

// metaCompressedKey marks summary messages to prevent re-compression.
const metaCompressedKey = "summary_compressed"

const (
	defaultKeepRecent = 10
	defaultBatchSize  = 20
)

// Option configures a summary ContextManager.
type Option func(*contextManager)

// WithMaxTokens sets the token budget that triggers compression.
// A value of 0 disables compression (no-op).
func WithMaxTokens(tokens int64) Option {
	return func(c *contextManager) {
		c.maxTokens = tokens
	}
}

// WithSummarizer sets the ModelProvider used to generate summaries.
func WithSummarizer(model blades.ModelProvider) Option {
	return func(c *contextManager) {
		c.summarizer = model
	}
}

// WithTokenCounter sets the TokenCounter used to estimate token usage.
// Defaults to a character-based counter (1 token ≈ 4 chars).
func WithTokenCounter(counter blades.TokenCounter) Option {
	return func(c *contextManager) {
		c.counter = counter
	}
}

// WithKeepRecent sets the number of most-recent messages always kept verbatim.
// Defaults to 10.
func WithKeepRecent(n int) Option {
	return func(c *contextManager) {
		c.keepRecent = n
	}
}

// WithBatchSize sets the number of messages to summarize per compression pass.
// Defaults to 20.
func WithBatchSize(n int) Option {
	return func(c *contextManager) {
		c.batchSize = n
	}
}

type contextManager struct {
	maxTokens  int64
	counter    blades.TokenCounter
	summarizer blades.ModelProvider
	keepRecent int
	batchSize  int
}

// NewContextManager returns a ContextManager that compresses old messages using
// the provided ModelProvider when the token count exceeds the configured limit.
// Recent messages are always kept verbatim.
//
// Compression proceeds as follows:
//  1. Always keep the KeepRecent most recent messages unchanged.
//  2. If total tokens exceed MaxTokens, take the oldest BatchSize eligible
//     messages and call Summarizer to produce a summary.
//  3. Replace those messages with a single summary Message (marked internally
//     so it is not re-compressed later).
//  4. Repeat until under MaxTokens or no more messages can be compressed.
func NewContextManager(opts ...Option) blades.ContextManager {
	cm := &contextManager{
		keepRecent: defaultKeepRecent,
		batchSize:  defaultBatchSize,
	}
	for _, opt := range opts {
		opt(cm)
	}
	return cm
}

// Prepare compresses old messages if the total token count exceeds MaxTokens.
func (s *contextManager) Prepare(ctx context.Context, messages []*blades.Message) ([]*blades.Message, error) {
	if len(messages) == 0 || s.maxTokens == 0 {
		return messages, nil
	}

	result := make([]*blades.Message, len(messages))
	copy(result, messages)

	for s.counter.Count(result...) > s.maxTokens {
		boundary := len(result) - s.keepRecent
		if boundary <= 0 {
			break
		}

		start, end := -1, 0
		count := 0
		for i := 0; i < boundary; i++ {
			m := result[i]
			if m.Metadata[metaCompressedKey] == true {
				continue
			}
			if start == -1 {
				start = i
			}
			end = i + 1
			count++
			if count >= s.batchSize {
				break
			}
		}
		if start == -1 {
			break
		}

		batch := result[start:end]
		summaryMsg, err := s.summarize(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("context manager: summarization failed: %w", err)
		}

		newResult := make([]*blades.Message, 0, len(result)-len(batch)+1)
		newResult = append(newResult, result[:start]...)
		newResult = append(newResult, summaryMsg)
		newResult = append(newResult, result[end:]...)
		result = newResult
	}

	return result, nil
}

func (s *contextManager) summarize(ctx context.Context, messages []*blades.Message) (*blades.Message, error) {
	prompt := buildSummaryPrompt(messages)
	req := &blades.ModelRequest{
		Messages: []*blades.Message{blades.UserMessage(prompt)},
	}
	resp, err := s.summarizer.Generate(ctx, req)
	if err != nil {
		return nil, err
	}
	msg := resp.Message
	if msg.Metadata == nil {
		msg.Metadata = make(map[string]any)
	}
	msg.Metadata[metaCompressedKey] = true
	msg.Role = blades.RoleUser
	return msg, nil
}

func buildSummaryPrompt(messages []*blades.Message) string {
	var sb strings.Builder
	sb.WriteString("Please provide a concise summary of the following conversation transcript. " +
		"Preserve key facts, decisions, and outcomes. Output only the summary.\n\n")
	for _, m := range messages {
		sb.WriteString(string(m.Role))
		sb.WriteString(": ")
		sb.WriteString(m.Text())
		sb.WriteByte('\n')
	}
	return sb.String()
}
