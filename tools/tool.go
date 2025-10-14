package tools

import (
	"context"
	"encoding/json"

	"github.com/google/jsonschema-go/jsonschema"
)

// Tool represents a tool with a name, description, input schema, and a tool handler.
type Tool struct {
	Name         string             `json:"name"`
	Description  string             `json:"description"`
	InputSchema  *jsonschema.Schema `json:"inputSchema"`
	OutputSchema *jsonschema.Schema `json:"outputSchema"`
	Handler      Handler            `json:"-"`
}

// NewTool creates a new Tool with the given name, description, input and output types, and handler.
func NewTool[I, O any](name string, description string, handler ToolFunc[I, O]) (*Tool, error) {
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
		Handler: HandleFunc(func(ctx context.Context, input string) (string, error) {
			var req I
			if err := json.Unmarshal([]byte(input), &req); err != nil {
				return "", err
			}
			res, err := handler(ctx, req)
			if err != nil {
				return "", err
			}
			b, err := json.Marshal(res)
			if err != nil {
				return "", err
			}
			return string(b), nil
		}),
	}, nil
}
