package blades

import (
	"context"

	"github.com/go-kratos/blades/tools"
	"github.com/go-kratos/kit/container/maps"
)

// AgentContext provides metadata about an agent.
type AgentContext interface {
	Name() string
	Description() string
}

// ToolContext is an alias for tools.ToolContext so that existing callers of
// blades.ToolContext / blades.FromToolContext continue to work unchanged.
type ToolContext = tools.ToolContext

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
