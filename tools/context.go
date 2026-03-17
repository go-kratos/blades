package tools

import "context"

// ToolContext provides metadata about a tool invocation and allows the tool
// to communicate control-flow signals back to the caller through SetAction.
type ToolContext interface {
	ID() string
	Name() string
	// Actions returns a copy of the tool's accumulated action map.
	Actions() map[string]any
	// SetAction records a control-flow action that callers (e.g. LoopAgent,
	// RoutingAgent) can inspect on the yielded message after tool execution.
	// Safe for concurrent use.
	SetAction(key string, value any)
}

type ctxToolKey struct{}

// NewContext returns a child context that carries the given ToolContext.
func NewContext(ctx context.Context, tool ToolContext) context.Context {
	return context.WithValue(ctx, ctxToolKey{}, tool)
}

// FromContext retrieves the ToolContext stored by NewContext.
func FromContext(ctx context.Context) (ToolContext, bool) {
	tool, ok := ctx.Value(ctxToolKey{}).(ToolContext)
	return tool, ok
}
