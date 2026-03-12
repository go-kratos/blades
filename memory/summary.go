package memory

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades"
)

// metaCompressedKey is the metadata key used to mark summary messages so they
// are not re-compressed in subsequent iterations.
const metaCompressedKey = "_compressed"

// SummaryConfig holds configuration for the summary-based context manager.
type SummaryConfig struct {
	// MaxTokens is the token budget that triggers compression. 0 disables compression.
	MaxTokens int64
	// Counter estimates token usage. Defaults to charBasedCounter.
	Counter blades.TokenCounter
	// Summarizer is the ModelProvider used to generate summaries.
	Summarizer blades.ModelProvider
	// KeepRecent is the number of most-recent messages always kept verbatim.
	// Defaults to 10.
	KeepRecent int
	// BatchSize is the number of messages to summarize per compression pass.
	// Defaults to 20.
	BatchSize int
}

// summaryContextManager compresses old messages into LLM-generated summaries
// while keeping the most recent messages verbatim.
//
// Compression proceeds as follows:
//  1. Always keep the KeepRecent most recent messages unchanged.
//  2. If total tokens exceed MaxTokens, take the oldest BatchSize eligible
//     messages and call Summarizer to produce a summary.
//  3. Replace those messages with a single summary Message (marked with
//     metaCompressedKey metadata so it is not re-compressed later).
//  4. Repeat until under MaxTokens or no more messages can be compressed.
type summaryContextManager struct {
	cfg SummaryConfig
}

// NewSummaryContextManager returns a ContextManager that compresses old
// messages using the provided ModelProvider when the token count exceeds
// cfg.MaxTokens. Recent messages are always kept verbatim.
func NewSummaryContextManager(cfg SummaryConfig) blades.ContextManager {
	return &summaryContextManager{cfg: cfg}
}

// Prepare compresses old messages if the total token count exceeds MaxTokens.
func (s *summaryContextManager) Prepare(ctx context.Context, messages []*blades.Message) ([]*blades.Message, error) {
	if len(messages) == 0 || s.cfg.MaxTokens == 0 {
		return messages, nil
	}
	counter := s.counter()
	keepRecent := s.keepRecent()
	batchSize := s.batchSize()

	result := make([]*blades.Message, len(messages))
	copy(result, messages)

	for counter.Count(result...) > s.cfg.MaxTokens {
		boundary := len(result) - keepRecent
		if boundary <= 0 {
			break
		}

		// Find eligible (non-summary) messages before the boundary.
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
			if count >= batchSize {
				break
			}
		}
		if start == -1 {
			break
		}

		batch := result[start:end]
		summary, err := s.summarize(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("context manager: summarization failed: %w", err)
		}

		newResult := make([]*blades.Message, 0, len(result)-len(batch)+1)
		newResult = append(newResult, result[:start]...)
		newResult = append(newResult, summary)
		newResult = append(newResult, result[end:]...)
		result = newResult
	}

	return result, nil
}

func (s *summaryContextManager) counter() blades.TokenCounter {
	if s.cfg.Counter != nil {
		return s.cfg.Counter
	}
	return &charBasedCounter{}
}

func (s *summaryContextManager) keepRecent() int {
	if s.cfg.KeepRecent > 0 {
		return s.cfg.KeepRecent
	}
	return 10
}

func (s *summaryContextManager) batchSize() int {
	if s.cfg.BatchSize > 0 {
		return s.cfg.BatchSize
	}
	return 20
}

// summarize calls the configured Summarizer with a formatted transcript.
func (s *summaryContextManager) summarize(ctx context.Context, messages []*blades.Message) (*blades.Message, error) {
	prompt := buildSummaryPrompt(messages)
	req := &blades.ModelRequest{
		Messages: []*blades.Message{blades.UserMessage(prompt)},
	}
	resp, err := s.cfg.Summarizer.Generate(ctx, req)
	if err != nil {
		return nil, err
	}
	summaryMsg := resp.Message
	if summaryMsg.Metadata == nil {
		summaryMsg.Metadata = make(map[string]any)
	}
	summaryMsg.Metadata[metaCompressedKey] = true
	summaryMsg.Role = blades.RoleUser
	return summaryMsg, nil
}

// buildSummaryPrompt formats a slice of messages into a text transcript.
func buildSummaryPrompt(messages []*blades.Message) string {
	var buf []byte
	buf = append(buf, "Please provide a concise summary of the following conversation transcript. "+
		"Preserve key facts, decisions, and outcomes. Output only the summary.\n\n"...)
	for _, m := range messages {
		buf = append(buf, string(m.Role)+": "+m.Text()+"\n"...)
	}
	return string(buf)
}
