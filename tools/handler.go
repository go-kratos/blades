package tools

import (
	"context"
	"encoding/json"
)

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

// JSONAdapter adapts a Handler that consumes and produces JSON-serializable types
// to a Handler that consumes and produces strings.
func JSONAdapter[I, O any](handle func(context.Context, I) (O, error)) HandleFunc {
	return func(ctx context.Context, input string) (string, error) {
		var req I
		if err := json.Unmarshal([]byte(input), &req); err != nil {
			return "", err
		}
		res, err := handle(ctx, req)
		if err != nil {
			return "", err
		}
		b, err := json.Marshal(res)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
}
