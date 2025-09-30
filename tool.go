package blades

import (
	"context"
	"encoding/json"

	"github.com/google/jsonschema-go/jsonschema"
)

type ToolHandler interface {
	Handle(context.Context, string) (string, error)
}

// Tool represents a tool with a name, description, input schema, and a tool handler.
type Tool struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	InputSchema *jsonschema.Schema `json:"inputSchema"`
	Handler     ToolHandler        `json:"-"`
}

// FuncAdapter is a function that adapts a function to a ToolHandler.
type FuncAdapter func(context.Context, string) (string, error)

func (f FuncAdapter) Handle(ctx context.Context, input string) (string, error) {
	return f(ctx, input)
}

// TypedHandler is a handler that handles a typed input and output.
type TypedHandler[I, O any] struct {
	handler func(context.Context, I) (O, error)
}

// NewTypedHandler creates a new TypedHandler.
func NewTypedHandler[I, O any](handler func(context.Context, I) (O, error)) *TypedHandler[I, O] {
	return &TypedHandler[I, O]{handler: handler}
}

func (f *TypedHandler[I, O]) Handle(ctx context.Context, input string) (string, error) {
	var iuput I
	if err := json.Unmarshal([]byte(input), &iuput); err != nil {
		return "", err
	}

	output, err := f.handler(ctx, iuput)
	if err != nil {
		return "", err
	}

	outputBytes, err := json.Marshal(output)
	if err != nil {
		return "", err
	}

	return string(outputBytes), nil
}

// NewTool creates a new Tool.
func NewTool(name, description string, inputSchema *jsonschema.Schema, handler ToolHandler) *Tool {
	return &Tool{
		Name:        name,
		Description: description,
		InputSchema: inputSchema,
		Handler:     handler,
	}
}

// NewTypedTool creates a new Tool with a typed input and output.
func NewTypedTool[I, O any](name, description string, inputSchema *jsonschema.Schema, handler func(context.Context, I) (O, error)) *Tool {
	return &Tool{
		Name:        name,
		Description: description,
		InputSchema: inputSchema,
		Handler:     NewTypedHandler(handler),
	}
}

// NewFuncTool creates a new Tool with a function handler.
func NewFuncTool(name, description string, inputSchema *jsonschema.Schema, handler func(context.Context, string) (string, error)) *Tool {
	return &Tool{
		Name:        name,
		Description: description,
		InputSchema: inputSchema,
		Handler:     FuncAdapter(handler),
	}
}
