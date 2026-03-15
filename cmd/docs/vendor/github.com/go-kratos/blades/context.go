package blades

import (
	"context"
)

type AgentContext interface {
	Name() string
	Model() string
	Description() string
	Instructions() string
}

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

type agentContext struct {
	name         string
	model        string
	description  string
	instructions string
}

func (a *agentContext) Name() string {
	return a.name
}
func (a *agentContext) Model() string {
	return a.model
}
func (a *agentContext) Description() string {
	return a.description
}
func (a *agentContext) Instructions() string {
	return a.instructions
}
