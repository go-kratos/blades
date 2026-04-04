package blades

import (
	"context"

	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)

// ModelRequest is a multimodal chat-style request to the provider.
type ModelRequest struct {
	Tools        []tools.Tool       `json:"tools,omitempty"`
	Messages     []*Message         `json:"messages"`
	Instruction  *Message           `json:"instruction,omitempty"`
	InputSchema  *jsonschema.Schema `json:"inputSchema,omitempty"`
	OutputSchema *jsonschema.Schema `json:"outputSchema,omitempty"`
}

// ModelResponse is a single assistant message as a result of generation.
type ModelResponse struct {
	Message *Message `json:"message"`
}

// ModelProvider is an interface for multimodal chat-style models.
type ModelProvider interface {
	// Name returns the model name.
	Name() string
	// Generate executes the request and returns a single assistant response.
	Generate(context.Context, *ModelRequest) (*ModelResponse, error)
	// NewStreaming executes the request and returns a stream of assistant responses.
	NewStreaming(context.Context, *ModelRequest) Generator[*ModelResponse, error]
}
