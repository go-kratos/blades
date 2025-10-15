package tools

import (
	"github.com/google/jsonschema-go/jsonschema"
)

// Tool represents a tool with a name, description, input schema, and a tool handler.
type Tool struct {
	Name         string                  `json:"name"`
	Description  string                  `json:"description"`
	InputSchema  *jsonschema.Schema      `json:"inputSchema"`
	OutputSchema *jsonschema.Schema      `json:"outputSchema"`
	Handler      Handler[string, string] `json:"-"`
}

// NewTool creates a new Tool with the given name, description, input and output types, and handler.
func NewTool[I, O any](name string, description string, handler Handler[I, O]) (*Tool, error) {
	inputSchema, err := jsonschema.For[I](nil)
	if err != nil {
		return nil, err
	}
	outputSchema, err := jsonschema.For[O](nil)
	if err != nil {
		return nil, err
	}
	return &Tool{
		Name:         name,
		Description:  description,
		InputSchema:  inputSchema,
		OutputSchema: outputSchema,
		Handler:      JSONAdapter(handler),
	}, nil
}
