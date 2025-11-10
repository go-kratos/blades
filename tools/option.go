package tools

import "github.com/google/jsonschema-go/jsonschema"

type Option func(*baseTool)

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
