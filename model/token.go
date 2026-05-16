package model

import "context"

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
	InputTokens    int64
	SystemTokens   int64
	MessagesTokens int64
	ToolTokens     int64
	HasBreakdown   bool
}
