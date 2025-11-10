package tools

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
)

// Tool defines the interface for a tool that can be used in a system.
type Tool interface {
	Name() string
	Description() string
	InputSchema() *jsonschema.Schema
	OutputSchema() *jsonschema.Schema
	Handle(context.Context, string) (string, error)
}

// NewTool creates a new Tool with the given name, description, and handler.
func NewTool(name string, description string, handler Handler[string, string], opts ...Option) Tool {
	t := &baseTool{
		name:        name,
		description: description,
		handler:     handler,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// NewFunc creates a new Tool with the given name, description, input and output types, and handler.
func NewFunc[I, O any](name string, description string, handler Handler[I, O]) (Tool, error) {
	inputSchema, err := jsonschema.For[I](nil)
	if err != nil {
		return nil, err
	}
	outputSchema, err := jsonschema.For[O](nil)
	if err != nil {
		return nil, err
	}
	return &baseTool{
		name:         name,
		description:  description,
		inputSchema:  inputSchema,
		outputSchema: outputSchema,
		handler:      JSONAdapter(handler),
	}, nil
}
