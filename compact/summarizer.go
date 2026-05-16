package compact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
)

var (
	// ErrSummarizerProviderRequired is returned when a model summarizer has no provider.
	ErrSummarizerProviderRequired = errors.New("compact: summarizer provider is required")
	// ErrSummarizerEmptyResponse is returned when the summary model returns no text.
	ErrSummarizerEmptyResponse = errors.New("compact: summarizer returned empty response")
)

const defaultSummarySystem = "Summarize conversation history for future model context. Preserve user goals, decisions, constraints, tool results, unresolved tasks, and important facts. Do not invent facts. Keep the summary concise and self-contained."

// SummarizerOption configures a model-backed summarizer.
type SummarizerOption func(*summarizerOptions)

type summarizerOptions struct {
	system  string
	options []model.Option
}

// WithSummarySystem sets the system prompt for model-backed summaries.
func WithSummarySystem(system string) SummarizerOption {
	return func(o *summarizerOptions) {
		o.system = system
	}
}

// WithSummaryOptions appends provider-neutral request options for summaries.
func WithSummaryOptions(opts ...model.Option) SummarizerOption {
	return func(o *summarizerOptions) {
		o.options = append(o.options, opts...)
	}
}

// NewModelSummarizer creates a Summarizer backed by direct provider calls.
func NewModelSummarizer(provider model.Provider, opts ...SummarizerOption) Summarizer {
	cfg := summarizerOptions{system: defaultSummarySystem}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &modelSummarizer{
		provider: provider,
		system:   cfg.system,
		options:  append([]model.Option(nil), cfg.options...),
	}
}

type modelSummarizer struct {
	provider model.Provider
	system   string
	options  []model.Option
}

func (s *modelSummarizer) Summarize(ctx context.Context, req SummaryRequest) (string, error) {
	if s.provider == nil {
		return "", ErrSummarizerProviderRequired
	}
	resp, err := s.provider.Generate(ctx, &model.Request{
		Model:    s.provider.Name(),
		System:   s.system,
		Messages: summaryMessages(req),
		Options:  summaryOptions(req.MaxTokens, s.options),
	})
	if err != nil {
		return "", err
	}
	if resp == nil || resp.Message == nil {
		return "", ErrSummarizerEmptyResponse
	}
	text := strings.TrimSpace(content.TextFromParts(resp.Message.Parts))
	if text == "" {
		return "", ErrSummarizerEmptyResponse
	}
	return text, nil
}

func summaryMessages(req SummaryRequest) []*model.Message {
	return []*model.Message{
		{
			Role:  model.RoleUser,
			Parts: []content.Part{content.Text{Text: formatSummaryPrompt(req)}},
		},
	}
}

func summaryOptions(maxTokens int64, opts []model.Option) []model.Option {
	if maxTokens <= 0 {
		return append([]model.Option(nil), opts...)
	}
	n := int(maxTokens)
	defaults := []model.Option{model.Sampling{MaxTokens: &n}}
	return model.MergeOptions(defaults, opts)
}

func formatSummaryPrompt(req SummaryRequest) string {
	var b strings.Builder
	if req.MaxTokens > 0 {
		fmt.Fprintf(&b, "Target maximum summary tokens: %d.\n\n", req.MaxTokens)
	}
	b.WriteString("Transcript:\n")
	for i, msg := range req.Messages {
		fmt.Fprintf(&b, "\n<message index=%d role=%q>\n", i, messageRole(msg))
		b.WriteString(formatMessageParts(msg))
		b.WriteString("\n</message>\n")
	}
	return b.String()
}

func messageRole(msg *model.Message) model.Role {
	if msg == nil {
		return ""
	}
	return msg.Role
}

func formatMessageParts(msg *model.Message) string {
	if msg == nil {
		return ""
	}
	var b strings.Builder
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case content.Text:
			b.WriteString(p.Text)
		case content.Thinking:
			b.WriteString("[thinking] ")
			b.WriteString(p.Text)
		case content.ToolUse:
			fmt.Fprintf(&b, "[tool_use id=%q name=%q input=%s]", p.ID, p.Name, string(p.Input))
		case content.ToolResult:
			fmt.Fprintf(&b, "[tool_result id=%q name=%q error=%t]\n", p.ID, p.Name, p.IsError)
			b.WriteString(formatParts(p.Parts))
		case content.FilePart:
			fmt.Fprintf(&b, "[file uri=%q mime=%q name=%q]", p.URI, p.MIME, p.Filename)
		case content.FileRefPart:
			fmt.Fprintf(&b, "[file_ref id=%q mime=%q]", p.ID, p.MIME)
		case content.DataPart:
			fmt.Fprintf(&b, "[data mime=%q name=%q bytes=%d]", p.MIME, p.Filename, len(p.Bytes))
		default:
			data, _ := json.Marshal(p)
			b.Write(data)
		}
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

func formatParts(parts []content.Part) string {
	return formatMessageParts(&model.Message{Parts: parts})
}
