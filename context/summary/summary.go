package summary

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/internal/counter"
)

// Session state keys used to persist compression state across runs.
// Values stored are primitive types (string and int) only.
const (
	// stateSummaryOffsetKey holds the number of messages from session.History()
	// that have been folded into the rolling summary (int).
	stateSummaryOffsetKey = "__summary_offset__"
	// stateSummaryContentKey holds the rolling summary text (string).
	stateSummaryContentKey = "__summary_content__"
)

const (
	defaultKeepRecent = 10
	defaultBatchSize  = 20

	defaultInstruction = "Please provide a concise summary of the following conversation transcript. " +
		"Preserve key facts, decisions, and outcomes. Output only the summary."
)

// buildWorkingView replaces messages[0:offset] with a single summary message.
func buildWorkingView(summaryContent string, offset int, messages []*blades.Message) []*blades.Message {
	if offset <= 0 || summaryContent == "" {
		return messages
	}
	result := make([]*blades.Message, 0, 1+len(messages)-offset)
	result = append(result, blades.AssistantMessage(summaryContent))
	result = append(result, messages[offset:]...)
	return result
}

// Option configures a summary ContextCompressor.
type Option func(*contextCompressor)

// WithMaxTokens sets the token budget that triggers compression.
// A value of 0 disables compression (no-op).
func WithMaxTokens(tokens int64) Option {
	return func(c *contextCompressor) {
		c.maxTokens = tokens
	}
}

// WithTokenCounter sets the TokenCounter used to estimate token usage.
// Defaults to a character-based counter (1 token ≈ 4 chars).
func WithTokenCounter(counter blades.TokenCounter) Option {
	return func(c *contextCompressor) {
		c.counter = counter
	}
}

// WithKeepRecent sets the number of most-recent messages always kept verbatim.
// Defaults to 10.
func WithKeepRecent(n int) Option {
	return func(c *contextCompressor) {
		c.keepRecent = n
	}
}

// WithBatchSize sets the number of messages to summarize per compression pass.
// Defaults to 20.
func WithBatchSize(n int) Option {
	return func(c *contextCompressor) {
		c.batchSize = n
	}
}

// contextCompressor implements ContextCompressor by compressing old messages into a rolling summary when the token count exceeds a configured limit.
// Compression state is persisted in the session when available to avoid redundant summarization work across runs.
type contextCompressor struct {
	maxTokens   int64
	counter     blades.TokenCounter
	summarizer  blades.ModelProvider
	keepRecent  int
	batchSize   int
	instruction string
}

// NewContextCompressor returns a ContextCompressor that compresses old messages using the provided
// ModelProvider when the token count exceeds the configured limit.
// Recent messages are always kept verbatim.
//
// When a Session is present in the context, compression state (rolling summary
// text and compressed message offset) is persisted across runs via session.State().
// This avoids re-processing already-summarised messages on every invocation.
//
// Compression proceeds as follows:
//  1. Read offset and rolling summary from session.State() (if a session exists).
//  2. Build a working view: [summaryMsg (if any)] + messages[offset:].
//  3. If total tokens exceed MaxTokens, take the next BatchSize messages after
//     the current offset, extend the rolling summary with them, and advance the
//     offset. Repeat until under budget or no more messages can be compressed.
//  4. Persist the updated offset and summary content back to session.State().
func NewContextCompressor(model blades.ModelProvider, opts ...Option) blades.ContextCompressor {
	c := &contextCompressor{
		instruction: defaultInstruction,
		keepRecent:  defaultKeepRecent,
		batchSize:   defaultBatchSize,
		summarizer:  model,
		counter:     counter.NewCharBasedCounter(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ensureSession returns the Session from ctx. If none is present a temporary
// in-memory session is returned so the rest of Compress never has to branch.
func (s *contextCompressor) ensureSession(ctx context.Context) blades.Session {
	if session, ok := blades.FromSessionContext(ctx); ok {
		return session
	}
	return blades.NewSession()
}

// Compress compresses old messages if the total token count exceeds MaxTokens.
// When a session is present in ctx it reads and writes two primitive-typed state
// keys to persist incremental compression state across runs.
func (s *contextCompressor) Compress(ctx context.Context, messages []*blades.Message) ([]*blades.Message, error) {
	if len(messages) == 0 || s.maxTokens == 0 {
		return messages, nil
	}

	// Always get a session: if one exists in the context its state persists across
	// runs; otherwise a temporary session is used and state is discarded at the end
	// of this call (effectively stateless behaviour).
	session := s.ensureSession(ctx)

	// Read persisted compression state from session (primitive types only).
	offset := 0
	summaryContent := ""
	if v, ok := session.State()[stateSummaryOffsetKey]; ok {
		if n, ok := v.(int); ok {
			offset = n
		}
	}
	if v, ok := session.State()[stateSummaryContentKey]; ok {
		if c, ok := v.(string); ok {
			summaryContent = c
		}
	}
	// Guard against a stale offset if the session history was reset externally.
	if offset > len(messages) {
		offset = 0
		summaryContent = ""
	}

	// Build the initial working view using the persisted summary and offset.
	workingView := buildWorkingView(summaryContent, offset, messages)

	// Only compress further when the working view exceeds the token budget.
	for s.counter.Count(workingView...) > s.maxTokens {
		boundary := len(messages) - s.keepRecent
		if offset >= boundary {
			break // all compressible messages already folded into the summary
		}

		end := min(offset+s.batchSize, boundary)
		batch := messages[offset:end]

		newSummary, err := s.extendSummary(ctx, summaryContent, batch)
		if err != nil {
			return nil, fmt.Errorf("context manager: summarization failed: %w", err)
		}

		offset = end
		summaryContent = newSummary
		workingView = buildWorkingView(summaryContent, offset, messages)
	}

	// Persist updated state (primitive types: int and string).
	session.SetState(stateSummaryOffsetKey, offset)
	session.SetState(stateSummaryContentKey, summaryContent)

	return workingView, nil
}

// extendSummary calls the summarizer LLM to produce a new summary that covers
// both the existing summary text and the provided message batch.
func (s *contextCompressor) extendSummary(ctx context.Context, existing string, batch []*blades.Message) (string, error) {
	instruction := s.instruction
	if existing != "" {
		instruction += "\n\nExisting summary:\n" + existing
	}
	req := &blades.ModelRequest{
		Messages:    batch,
		Instruction: blades.SystemMessage(instruction),
	}
	resp, err := s.summarizer.Generate(ctx, req)
	if err != nil {
		return "", err
	}
	return resp.Message.Text(), nil
}
