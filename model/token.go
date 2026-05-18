package model

import (
	"context"
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"github.com/go-kratos/blades/content"
)

// TokenCounter estimates token usage for a full provider request without
// invoking generation.
type TokenCounter interface {
	CountTokens(ctx context.Context, req *Request) (TokenCount, error)
}

// TokenCounterFunc adapts a function into a TokenCounter.
type TokenCounterFunc func(ctx context.Context, req *Request) (TokenCount, error)

// CountTokens implements TokenCounter.
func (f TokenCounterFunc) CountTokens(ctx context.Context, req *Request) (TokenCount, error) {
	return f(ctx, req)
}

// TokenCount describes estimated request token usage.
type TokenCount struct {
	Input    int64
	System   int64
	Messages int64
	Tools    int64
}

// Total returns Input if set, otherwise the sum of sub-segments.
func (c TokenCount) Total() int64 {
	if c.Input > 0 {
		return c.Input
	}
	return c.System + c.Messages + c.Tools
}

// HasSegments reports whether per-segment breakdown is available.
func (c TokenCount) HasSegments() bool {
	return c.System > 0 || c.Messages > 0 || c.Tools > 0
}

// ApproxTokenCounter is a provider-agnostic, conservative token estimator.
//
// This fallback exists so context stats and coarse budgets work even before a
// model-specific tokenizer is wired in.
type ApproxTokenCounter struct{}

// CountTokens implements TokenCounter.
func (ApproxTokenCounter) CountTokens(ctx context.Context, req *Request) (TokenCount, error) {
	if err := ctx.Err(); err != nil {
		return TokenCount{}, err
	}
	if req == nil {
		return TokenCount{}, nil
	}
	system := estimateTextTokens(req.System)
	messages := estimateMessagesTokens(req.Messages)
	tools := estimateJSONTokens(req.Tools)
	return TokenCount{
		Input:    system + messages + tools,
		System:   system,
		Messages: messages,
		Tools:    tools,
	}, nil
}

func estimateMessagesTokens(msgs []*Message) int64 {
	var total int64
	for _, msg := range msgs {
		total += estimateMessageTokens(msg)
	}
	return total
}

func estimateMessageTokens(msg *Message) int64 {
	if msg == nil {
		return 0
	}
	total := estimateTextTokens(string(msg.Role))
	for _, part := range msg.Parts {
		total += estimatePartTokens(part)
	}
	if total > 0 {
		total += 3
	}
	return total
}

func estimatePartTokens(part content.Part) int64 {
	switch p := part.(type) {
	case content.Text:
		return estimateTextTokens(p.Text)
	case content.Thinking:
		return estimateTextTokens(p.Text) + estimateByteTokens(len(p.Signature))
	case content.ToolUse:
		return estimateTextTokens(p.ID) + estimateTextTokens(p.Name) + estimateTextTokens(string(p.Input))
	case content.ToolResult:
		return estimateTextTokens(p.ID) + estimateTextTokens(p.Name) + estimateMessagesTokens([]*Message{{Parts: p.Parts}}) + 1
	case content.FilePart:
		return estimateTextTokens(p.URI) + estimateTextTokens(p.MIME) + estimateTextTokens(p.Filename)
	case content.FileRefPart:
		return estimateTextTokens(p.ID) + estimateTextTokens(p.MIME)
	case content.DataPart:
		return estimateByteTokens(len(p.Bytes)) + estimateTextTokens(p.MIME) + estimateTextTokens(p.Filename)
	default:
		return estimateJSONTokens(p)
	}
}

func estimateJSONTokens(v any) int64 {
	data, err := json.Marshal(v)
	if err != nil {
		return estimateTextTokens(fmt.Sprint(v))
	}
	return estimateTextTokens(string(data))
}

func estimateTextTokens(s string) int64 {
	var ascii, nonASCII int64
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		if r == utf8.RuneError && size == 0 {
			break
		}
		if r < utf8.RuneSelf {
			ascii++
		} else {
			nonASCII++
		}
		s = s[size:]
	}
	return ceilDiv(ascii, 4) + nonASCII
}

func estimateByteTokens(n int) int64 {
	if n <= 0 {
		return 0
	}
	encoded := int64((n + 2) / 3 * 4)
	return ceilDiv(encoded, 4)
}

func ceilDiv(n, d int64) int64 {
	if n <= 0 {
		return 0
	}
	return (n + d - 1) / d
}
