// Package dummyprovider contains a minimal predefined-response model provider.
package dummyprovider

import (
	"context"
	"encoding/json"
	"errors"
	"iter"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
)

// ErrNoResponses is returned when the provider is called with no response left.
var ErrNoResponses = errors.New("dummyprovider: no predefined responses left")

var _ model.Provider = (*Provider)(nil)

// Provider is a minimal model.Provider backed by predefined responses.
type Provider struct {
	responses []*model.Response
}

// New creates a dummy provider with predefined responses.
func New(responses ...*model.Response) *Provider {
	return NewProvider(responses...)
}

// NewProvider creates a dummy provider with predefined responses.
func NewProvider(responses ...*model.Response) *Provider {
	p := &Provider{}
	p.SetResponses(responses...)
	return p
}

// Name implements model.Provider.
func (p *Provider) Name() string {
	return "dummy"
}

// Generate implements model.Provider.
func (p *Provider) Generate(context.Context, *model.Request) (*model.Response, error) {
	if len(p.responses) == 0 {
		return nil, ErrNoResponses
	}
	resp := p.responses[0]
	p.responses = p.responses[1:]
	return resp, nil
}

// Stream implements model.Provider by streaming chunks derived from Generate.
func (p *Provider) Stream(ctx context.Context, req *model.Request) iter.Seq2[*model.Chunk, error] {
	return func(yield func(*model.Chunk, error) bool) {
		resp, err := p.Generate(ctx, req)
		if err != nil {
			yield(nil, err)
			return
		}
		for _, chunk := range ChunksFromResponse(resp) {
			if !yield(chunk, nil) {
				return
			}
		}
	}
}

// SetResponses replaces the predefined response queue.
func (p *Provider) SetResponses(responses ...*model.Response) {
	p.responses = responses
}

type responseConfig struct {
	stopReason model.StopReason
	usage      *model.Usage
}

// ResponseOption configures helper responses.
type ResponseOption func(*responseConfig)

// WithStopReason sets the response stop reason.
func WithStopReason(reason model.StopReason) ResponseOption {
	return func(c *responseConfig) {
		c.stopReason = reason
	}
}

// WithResponseUsage sets the response usage.
func WithResponseUsage(usage model.Usage) ResponseOption {
	return func(c *responseConfig) {
		c.usage = &usage
	}
}

// Text creates a text content part.
func Text(text string) content.Text {
	return content.Text{Text: text}
}

// Thinking creates a thinking content part.
func Thinking(text string, signature ...[]byte) content.Thinking {
	part := content.Thinking{Text: text}
	if len(signature) > 0 {
		part.Signature = signature[0]
	}
	return part
}

// ToolUse creates a tool-use content part.
func ToolUse(id, name string, input json.RawMessage) content.ToolUse {
	return content.ToolUse{ID: id, Name: name, Input: input}
}

// TextResponse creates an assistant response with a single text part.
func TextResponse(text string, opts ...ResponseOption) *model.Response {
	return AssistantResponse([]content.Part{Text(text)}, opts...)
}

// ToolUseResponse creates an assistant response with a single tool-use part.
func ToolUseResponse(id, name string, input json.RawMessage, opts ...ResponseOption) *model.Response {
	return assistantResponse(model.StopToolUse, []content.Part{ToolUse(id, name, input)}, opts...)
}

// AssistantResponse creates an assistant response with arbitrary content parts.
func AssistantResponse(parts []content.Part, opts ...ResponseOption) *model.Response {
	return assistantResponse(model.StopEnd, parts, opts...)
}

func assistantResponse(defaultStopReason model.StopReason, parts []content.Part, opts ...ResponseOption) *model.Response {
	cfg := responseConfig{stopReason: defaultStopReason}
	for _, opt := range opts {
		opt(&cfg)
	}

	resp := &model.Response{
		Message:    &model.Message{Role: model.RoleAssistant, Parts: parts},
		StopReason: cfg.stopReason,
	}
	if cfg.usage != nil {
		resp.Usage = *cfg.usage
	}
	return resp
}

// Chunk creates a streaming model chunk.
func Chunk(parts []content.Part, opts ...ResponseOption) *model.Chunk {
	cfg := responseConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	chunk := &model.Chunk{
		Parts:      parts,
		StopReason: cfg.stopReason,
	}
	if cfg.usage != nil {
		usage := *cfg.usage
		chunk.Usage = &usage
	}
	return chunk
}

// ChunksFromResponse converts a response into streaming chunks.
// Text and thinking parts are split into deterministic one-rune deltas.
func ChunksFromResponse(resp *model.Response) []*model.Chunk {
	if resp == nil {
		return []*model.Chunk{{StopReason: model.StopEnd}}
	}

	var parts []content.Part
	if resp.Message != nil {
		parts = resp.Message.Parts
	}
	if len(parts) == 0 {
		return []*model.Chunk{{StopReason: resp.StopReason, Usage: &resp.Usage}}
	}

	var chunks []*model.Chunk
	for _, part := range parts {
		for _, delta := range splitPart(part) {
			chunks = append(chunks, &model.Chunk{Parts: []content.Part{delta}})
		}
	}
	if len(chunks) == 0 {
		chunks = append(chunks, &model.Chunk{})
	}
	last := chunks[len(chunks)-1]
	last.StopReason = resp.StopReason
	last.Usage = &resp.Usage
	return chunks
}

func splitPart(part content.Part) []content.Part {
	switch p := part.(type) {
	case content.Text:
		chunks := splitString(p.Text)
		parts := make([]content.Part, 0, len(chunks))
		for _, chunk := range chunks {
			parts = append(parts, content.Text{Text: chunk})
		}
		return parts
	case content.Thinking:
		chunks := splitString(p.Text)
		parts := make([]content.Part, 0, len(chunks))
		for _, chunk := range chunks {
			parts = append(parts, content.Thinking{Text: chunk, Signature: p.Signature})
		}
		return parts
	default:
		return []content.Part{part}
	}
}

func splitString(s string) []string {
	if s == "" {
		return []string{""}
	}
	chunks := make([]string, 0, len(s))
	for _, r := range s {
		chunks = append(chunks, string(r))
	}
	return chunks
}
