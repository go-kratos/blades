package tools

import "context"

// Handler consumes tool arguments returned by the LLM (serialized as JSON string).
// Implementations should decode the payload as needed and return the tool result as JSON.
type Handler interface {
	Handle(context.Context, string) (string, error)
}

// HandleFunc adapts a plain function to a ToolHandler, similar to http.HandleFunc.
type HandleFunc func(context.Context, string) (string, error)

// Handle is the Handle method of the Handler interface.
func (f HandleFunc) Handle(ctx context.Context, input string) (string, error) {
	return f(ctx, input)
}

// ToolAdapter is a function that takes an input of type I and returns an output of type O or an error.
type ToolAdapter[I, O any] func(context.Context, I) (O, error)
