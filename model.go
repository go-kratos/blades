package blades

import (
	"context"

	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)

// ModelOption configures a single request. Providers may ignore options
// they do not support but should prefer best-effort behavior.
type ModelOption func(*ModelOptions)

// ModelOptions holds common request-time controls.
type ModelOptions struct {
	Seed             int64
	MaxOutputTokens  int64
	FrequencyPenalty float64
	PresencePenalty  float64
	Temperature      float64
	TopP             float64
	StopSequences    []string
	Image            ImageOptions
	Audio            AudioOptions
}

// ImageOptions holds configuration for image generation requests.
type ImageOptions struct {
	Background        string
	Size              string
	Quality           string
	ResponseFormat    string
	OutputFormat      string
	Moderation        string
	Style             string
	User              string
	Count             int
	PartialImages     int
	OutputCompression int
}

// AudioOptions holds configuration for text-to-speech style requests.
type AudioOptions struct {
	Voice          string
	ResponseFormat string
	StreamFormat   string
	Instructions   string
	Speed          float64
}

// ModelRequest is a multimodal chat-style request to the provider.
type ModelRequest struct {
	Model        string             `json:"model"`
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
	// Generate executes the request and returns a single assistant response.
	Generate(context.Context, *ModelRequest, ...ModelOption) (*ModelResponse, error)
	// NewStreaming executes the request and returns a stream of assistant responses.
	NewStreaming(context.Context, *ModelRequest, ...ModelOption) Generator[*ModelResponse, error]
}
