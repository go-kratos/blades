package blades

import (
	"context"

	"github.com/go-kratos/kit/container/maps"
)

// AgentContext provides metadata about an agent.
type AgentContext interface {
	Name() string
	Description() string
}

// ToolContext provides metadata about a tool used by an agent.
type ToolContext interface {
	ID() string
	Name() string
	// Actions returns a copy of the tool's action map.
	Actions() map[string]any
	// SetAction sets or updates an action in the tool's action map.
	// This method is safe for concurrent use.
	SetAction(key string, value any)
}

// ctxAgentKey is the context key for AgentContext.
type ctxAgentKey struct{}

// NewAgentContext returns a new context with the given AgentContext.
func NewAgentContext(ctx context.Context, agent Agent) context.Context {
	return context.WithValue(ctx, ctxAgentKey{}, agent)
}

// FromAgentContext retrieves the AgentContext from the context, if present.
func FromAgentContext(ctx context.Context) (AgentContext, bool) {
	agent, ok := ctx.Value(ctxAgentKey{}).(AgentContext)
	return agent, ok
}

// ctxToolKey is the context key for ToolContext.
type ctxToolKey struct{}

// NewToolContext returns a new context with the given ToolContext.
func NewToolContext(ctx context.Context, tool ToolContext) context.Context {
	return context.WithValue(ctx, ctxToolKey{}, tool)
}

// FromToolContext retrieves the ToolContext from the context, if present.
func FromToolContext(ctx context.Context) (ToolContext, bool) {
	tool, ok := ctx.Value(ctxToolKey{}).(ToolContext)
	return tool, ok
}

type toolContext struct {
	id      string
	name    string
	actions *maps.Map[string, any]
}

func (t *toolContext) ID() string {
	return t.id
}
func (t *toolContext) Name() string {
	return t.name
}
func (t *toolContext) Actions() map[string]any {
	return t.actions.ToMap()
}
func (t *toolContext) SetAction(key string, value any) {
	t.actions.Store(key, value)
}
