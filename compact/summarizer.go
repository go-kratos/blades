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

const defaultSummaryInstruction = `Your task is to create a detailed summary of the conversation so far, paying close attention to the user's explicit requests and your previous actions.

Before providing your final summary, wrap your analysis in <analysis> tags to organize your thoughts. In your analysis process:
1. Identify the user's primary goals and any explicit requests
2. Note key technical concepts, decisions, and patterns discussed
3. List specific files examined, modified, or created
4. Document any errors encountered and how they were resolved
5. Highlight important context needed to continue the work

Your summary should include the following sections:

1. Primary Request and Intent: Capture all of the user's explicit requests and intents in detail
2. Key Technical Concepts: List all important technical concepts, technologies, and frameworks discussed
3. Files and Code Sections: Enumerate specific files and code sections examined, modified, or created. Include important code snippets and explain why each file is relevant
4. Errors and Fixes: List all errors encountered and how they were fixed. Pay special attention to user feedback and corrections
5. Current State: Summarize the current state of the work and what remains to be done

CRITICAL: Respond with TEXT ONLY. Do NOT call any tools. Provide your response as an <analysis> block followed by a <summary> block.`

// SummarizerOption configures a model-backed summarizer.
type SummarizerOption func(*summarizerOptions)

type summarizerOptions struct {
	instruction string
	options     []model.Option
}

// WithSummaryInstruction sets the instruction for model-backed summaries.
func WithSummaryInstruction(instruction string) SummarizerOption {
	return func(o *summarizerOptions) {
		o.instruction = instruction
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
	cfg := summarizerOptions{instruction: defaultSummaryInstruction}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &modelSummarizer{
		provider:    provider,
		instruction: cfg.instruction,
		options:     append([]model.Option(nil), cfg.options...),
	}
}

type modelSummarizer struct {
	provider    model.Provider
	instruction string
	options     []model.Option
}

func (s *modelSummarizer) Summarize(ctx context.Context, req SummaryRequest) (string, error) {
	if s.provider == nil {
		return "", ErrSummarizerProviderRequired
	}
	resp, err := s.provider.Generate(ctx, &model.Request{
		Model:    s.provider.Name(),
		System:   s.instruction,
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
