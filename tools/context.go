package tools

import "context"

// ToolContext provides per-invocation metadata to a tool handler.
type ToolContext interface {
	ID() string
	Spec() ToolSpec
}

type contextKey struct{}

// NewContext injects a ToolContext into the context.
func NewContext(ctx context.Context, tc ToolContext) context.Context {
	return context.WithValue(ctx, contextKey{}, tc)
}

// FromContext retrieves the ToolContext from the context.
func FromContext(ctx context.Context) (ToolContext, bool) {
	tc, ok := ctx.Value(contextKey{}).(ToolContext)
	return tc, ok
}
