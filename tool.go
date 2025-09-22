package blades

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
)

// Tool represents a tool with a name, description, input schema, and a callable function.
type Tool struct {
	Name        string                                        `json:"name"`
	Description string                                        `json:"description"`
	InputSchema *jsonschema.Schema                            `json:"inputSchema"`
	Handle      func(context.Context, string) (string, error) `json:"-"`
}
