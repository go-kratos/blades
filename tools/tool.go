package tools

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
)

type Tool interface {
	Name() string
	Description() string
	InputSchema() *jsonschema.Schema
	OutputSchema() *jsonschema.Schema
	Handle(context.Context, string) (string, error)
}

func NewTool(name string, description string, handler Handler[string, string], opts ...Option) Tool {
	t := &tool{
		name:         name,
		description:  description,
		inputSchema:  nil,
		outputSchema: nil,
		handler:      handler,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// NewToolFunc creates a new Tool with the given name, description, input and output types, and handler.
func NewToolFunc[I, O any](name string, description string, handler Handler[I, O]) (Tool, error) {
	inputSchema, err := jsonschema.For[I](nil)
	if err != nil {
		return nil, err
	}
	outputSchema, err := jsonschema.For[O](nil)
	if err != nil {
		return nil, err
	}
	return &tool{
		name:         name,
		description:  description,
		inputSchema:  inputSchema,
		outputSchema: outputSchema,
		handler:      JSONAdapter(handler),
	}, nil
}

type Option func(*tool)

// WithInputSchema sets the input schema for the tool.
func WithInputSchema(schema *jsonschema.Schema) Option {
	return func(t *tool) {
		t.inputSchema = schema
	}
}

// WithOutputSchema sets the output schema for the tool.
func WithOutputSchema(schema *jsonschema.Schema) Option {
	return func(t *tool) {
		t.outputSchema = schema
	}
}

// tool represents a tool with a name, description, input schema, and a tool handler.
type tool struct {
	name         string
	description  string
	inputSchema  *jsonschema.Schema
	outputSchema *jsonschema.Schema
	handler      Handler[string, string]
}

func (t *tool) Name() string {
	return t.name
}

func (t *tool) Description() string {
	return t.description
}

func (t *tool) InputSchema() *jsonschema.Schema {
	return t.inputSchema
}

func (t *tool) OutputSchema() *jsonschema.Schema {
	return t.outputSchema
}

func (t *tool) Handle(ctx context.Context, input string) (string, error) {
	return t.handler.Handle(ctx, input)
}
