package blades

import (
	"context"

	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)

// AgentContext holds information about the agent handling the request.
type AgentContext interface {
	Name() string
	Model() string
	Description() string
	Instructions() string
	Tools() []tools.Tool
	InputSchema() *jsonschema.Schema
	OutputSchema() *jsonschema.Schema
}

type ctxAgentKey struct{}

// NewAgentContext returns a new context with the given AgentContext.
func NewAgentContext(ctx context.Context, agent AgentContext) context.Context {
	return context.WithValue(ctx, ctxAgentKey{}, agent)
}

// FromContext retrieves the AgentContext from the context, if present.
func FromAgentContext(ctx context.Context) (AgentContext, bool) {
	agent, ok := ctx.Value(ctxAgentKey{}).(AgentContext)
	return agent, ok
}
