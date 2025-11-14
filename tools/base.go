package tools

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
)

// Option defines a configuration option for a baseTool.
type Option func(*baseTool)

// WithMiddleware sets the middlewares for the tool.
func WithMiddleware(mw ...Middleware) Option {
	return func(t *baseTool) {
		t.middlewares = mw
	}
}

// WithInputSchema sets the input schema for the tool.
func WithInputSchema(schema *jsonschema.Schema) Option {
	return func(t *baseTool) {
		t.inputSchema = schema
	}
}

// WithOutputSchema sets the output schema for the tool.
func WithOutputSchema(schema *jsonschema.Schema) Option {
	return func(t *baseTool) {
		t.outputSchema = schema
	}
}

// baseTool represents a tool with a name, description, input schema, and a tool handler.
type baseTool struct {
	name         string
	description  string
	inputSchema  *jsonschema.Schema
	outputSchema *jsonschema.Schema
	handler      Handler
	middlewares  []Middleware
}

func (t *baseTool) Name() string {
	return t.name
}

func (t *baseTool) Description() string {
	return t.description
}

func (t *baseTool) InputSchema() *jsonschema.Schema {
	return t.inputSchema
}

func (t *baseTool) OutputSchema() *jsonschema.Schema {
	return t.outputSchema
}

func (t *baseTool) Handle(ctx context.Context, input string) (string, error) {
	handler := t.handler
	if len(t.middlewares) > 0 {
		handler = ChainMiddlewares(t.middlewares...)(t.handler)
	}
	return handler.Handle(ctx, input)
}
