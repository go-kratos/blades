package blades

import (
	"context"
)

// AgentContext provides metadata about an AI agent.
type AgentContext interface {
	Name() string
	Description() string
}

// ToolContext provides metadata about a tool used by an agent.
type ToolContext interface {
	ID() string
	Name() string
	Actions() map[string]any
}

// ctxAgentKey is the context key for AgentContext.
type ctxAgentKey struct{}

// NewAgentContext returns a new context with the given AgentContext.
func NewAgentContext(ctx context.Context, agent AgentContext) context.Context {
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

type agentContext struct {
	name        string
	description string
}

func (a *agentContext) Name() string {
	return a.name
}
func (a *agentContext) Description() string {
	return a.description
}

type toolContext struct {
	id      string
	name    string
	actions map[string]any
}

func (t *toolContext) ID() string {
	return t.id
}
func (t *toolContext) Name() string {
	return t.name
}
func (t *toolContext) Actions() map[string]any {
	return t.actions
}
