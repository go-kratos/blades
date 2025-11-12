package blades

import (
	"context"

	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)

// ModelContext defines the interface for accessing model-related information
type ModelContext interface {
	Model() string
	Tools() []tools.Tool
	Instruction() *Message
	InputSchema() *jsonschema.Schema
	OutputSchema() *jsonschema.Schema
}

type ctxAgentKey struct{}

// NewAgentContext returns a new context with the given AgentContext.
func NewAgentContext(ctx context.Context, agent Agent) context.Context {
	return context.WithValue(ctx, ctxAgentKey{}, agent)
}

// FromAgentContext retrieves the AgentContext from the context, if present.
func FromAgentContext(ctx context.Context) (Agent, bool) {
	agent, ok := ctx.Value(ctxAgentKey{}).(Agent)
	return agent, ok
}

type ctxModelKey struct{}

// NewModelContext returns a new context with the given ModelContext.
func NewModelContext(ctx context.Context, modelCtx ModelContext) context.Context {
	return context.WithValue(ctx, ctxModelKey{}, modelCtx)
}

// FromModelContext retrieves the ModelContext from the context, if present.
func FromModelContext(ctx context.Context) (ModelContext, bool) {
	modelCtx, ok := ctx.Value(ctxModelKey{}).(ModelContext)
	return modelCtx, ok
}

type modelContext struct {
	model        string
	tools        []tools.Tool
	instruction  *Message
	inputSchema  *jsonschema.Schema
	outputSchema *jsonschema.Schema
}

func (m *modelContext) Model() string {
	return m.model
}
func (m *modelContext) Tools() []tools.Tool {
	return m.tools
}
func (m *modelContext) Instruction() *Message {
	return m.instruction
}
func (m *modelContext) InputSchema() *jsonschema.Schema {
	return m.inputSchema
}
func (m *modelContext) OutputSchema() *jsonschema.Schema {
	return m.outputSchema
}
