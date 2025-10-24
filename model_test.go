package blades

import (
	"context"
	"testing"

	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)

func TestModelOptions(t *testing.T) {
	opts := &ModelOptions{}

	// Test default values
	if opts.Seed != 0 {
		t.Errorf("ModelOptions.Seed = %v, want 0", opts.Seed)
	}
	if opts.MaxOutputTokens != 0 {
		t.Errorf("ModelOptions.MaxOutputTokens = %v, want 0", opts.MaxOutputTokens)
	}
	if opts.FrequencyPenalty != 0 {
		t.Errorf("ModelOptions.FrequencyPenalty = %v, want 0", opts.FrequencyPenalty)
	}
	if opts.PresencePenalty != 0 {
		t.Errorf("ModelOptions.PresencePenalty = %v, want 0", opts.PresencePenalty)
	}
	if opts.Temperature != 0 {
		t.Errorf("ModelOptions.Temperature = %v, want 0", opts.Temperature)
	}
	if opts.TopP != 0 {
		t.Errorf("ModelOptions.TopP = %v, want 0", opts.TopP)
	}
	if opts.StopSequences != nil {
		t.Errorf("ModelOptions.StopSequences = %v, want nil", opts.StopSequences)
	}
}

func TestImageOptions(t *testing.T) {
	opts := &ImageOptions{}

	// Test default values
	if opts.Background != "" {
		t.Errorf("ImageOptions.Background = %v, want empty string", opts.Background)
	}
	if opts.Size != "" {
		t.Errorf("ImageOptions.Size = %v, want empty string", opts.Size)
	}
	if opts.Quality != "" {
		t.Errorf("ImageOptions.Quality = %v, want empty string", opts.Quality)
	}
	if opts.ResponseFormat != "" {
		t.Errorf("ImageOptions.ResponseFormat = %v, want empty string", opts.ResponseFormat)
	}
	if opts.OutputFormat != "" {
		t.Errorf("ImageOptions.OutputFormat = %v, want empty string", opts.OutputFormat)
	}
	if opts.Moderation != "" {
		t.Errorf("ImageOptions.Moderation = %v, want empty string", opts.Moderation)
	}
	if opts.Style != "" {
		t.Errorf("ImageOptions.Style = %v, want empty string", opts.Style)
	}
	if opts.User != "" {
		t.Errorf("ImageOptions.User = %v, want empty string", opts.User)
	}
	if opts.Count != 0 {
		t.Errorf("ImageOptions.Count = %v, want 0", opts.Count)
	}
	if opts.PartialImages != 0 {
		t.Errorf("ImageOptions.PartialImages = %v, want 0", opts.PartialImages)
	}
	if opts.OutputCompression != 0 {
		t.Errorf("ImageOptions.OutputCompression = %v, want 0", opts.OutputCompression)
	}
}

func TestAudioOptions(t *testing.T) {
	opts := &AudioOptions{}

	// Test default values
	if opts.Voice != "" {
		t.Errorf("AudioOptions.Voice = %v, want empty string", opts.Voice)
	}
	if opts.ResponseFormat != "" {
		t.Errorf("AudioOptions.ResponseFormat = %v, want empty string", opts.ResponseFormat)
	}
	if opts.StreamFormat != "" {
		t.Errorf("AudioOptions.StreamFormat = %v, want empty string", opts.StreamFormat)
	}
	if opts.Instructions != "" {
		t.Errorf("AudioOptions.Instructions = %v, want empty string", opts.Instructions)
	}
	if opts.Speed != 0 {
		t.Errorf("AudioOptions.Speed = %v, want 0", opts.Speed)
	}
}

func TestModelRequest(t *testing.T) {
	req := &ModelRequest{}

	// Test default values
	if req.Model != "" {
		t.Errorf("ModelRequest.Model = %v, want empty string", req.Model)
	}
	if req.Tools != nil {
		t.Errorf("ModelRequest.Tools = %v, want nil", req.Tools)
	}
	if req.Messages != nil {
		t.Errorf("ModelRequest.Messages = %v, want nil", req.Messages)
	}
	if req.InputSchema != nil {
		t.Errorf("ModelRequest.InputSchema = %v, want nil", req.InputSchema)
	}
	if req.OutputSchema != nil {
		t.Errorf("ModelRequest.OutputSchema = %v, want nil", req.OutputSchema)
	}
}

func TestModelRequestWithData(t *testing.T) {
	tool := &tools.Tool{
		Name:        "test_tool",
		Description: "A test tool",
	}

	message := &Message{
		ID:    "msg-123",
		Role:  RoleUser,
		Parts: []Part{TextPart{Text: "Hello"}},
	}

	inputSchema := &jsonschema.Schema{Type: "object"}
	outputSchema := &jsonschema.Schema{Type: "string"}

	req := &ModelRequest{
		Model:        "test-model",
		Tools:        []*tools.Tool{tool},
		Messages:     []*Message{message},
		InputSchema:  inputSchema,
		OutputSchema: outputSchema,
	}

	if req.Model != "test-model" {
		t.Errorf("ModelRequest.Model = %v, want test-model", req.Model)
	}
	if len(req.Tools) != 1 {
		t.Errorf("ModelRequest.Tools length = %v, want 1", len(req.Tools))
	}
	if req.Tools[0].Name != "test_tool" {
		t.Errorf("ModelRequest.Tools[0].Name = %v, want test_tool", req.Tools[0].Name)
	}
	if len(req.Messages) != 1 {
		t.Errorf("ModelRequest.Messages length = %v, want 1", len(req.Messages))
	}
	if req.Messages[0].ID != "msg-123" {
		t.Errorf("ModelRequest.Messages[0].ID = %v, want msg-123", req.Messages[0].ID)
	}
	if req.InputSchema != inputSchema {
		t.Errorf("ModelRequest.InputSchema = %v, want %v", req.InputSchema, inputSchema)
	}
	if req.OutputSchema != outputSchema {
		t.Errorf("ModelRequest.OutputSchema = %v, want %v", req.OutputSchema, outputSchema)
	}
}

func TestModelResponse(t *testing.T) {
	message := &Message{
		ID:    "msg-123",
		Role:  RoleAssistant,
		Parts: []Part{TextPart{Text: "Hello, world!"}},
	}

	resp := &ModelResponse{
		Message: message,
	}

	if resp.Message != message {
		t.Errorf("ModelResponse.Message = %v, want %v", resp.Message, message)
	}
	if resp.Message.ID != "msg-123" {
		t.Errorf("ModelResponse.Message.ID = %v, want msg-123", resp.Message.ID)
	}
	if resp.Message.Role != RoleAssistant {
		t.Errorf("ModelResponse.Message.Role = %v, want %v", resp.Message.Role, RoleAssistant)
	}
}

// mockModelProvider is a mock implementation of ModelProvider for testing
type mockModelProvider struct {
	generateFunc func(context.Context, *ModelRequest, ...ModelOption) (*ModelResponse, error)
	streamFunc   func(context.Context, *ModelRequest, ...ModelOption) (Streamable[*ModelResponse], error)
}

func (m *mockModelProvider) Generate(ctx context.Context, req *ModelRequest, opts ...ModelOption) (*ModelResponse, error) {
	if m.generateFunc != nil {
		return m.generateFunc(ctx, req, opts...)
	}
	return &ModelResponse{
		Message: &Message{
			ID:    "mock-msg",
			Role:  RoleAssistant,
			Parts: []Part{TextPart{Text: "Mock response"}},
		},
	}, nil
}

func (m *mockModelProvider) NewStream(ctx context.Context, req *ModelRequest, opts ...ModelOption) (Streamable[*ModelResponse], error) {
	if m.streamFunc != nil {
		return m.streamFunc(ctx, req, opts...)
	}
	return &mockStream[*ModelResponse]{
		values: []*ModelResponse{
			{
				Message: &Message{
					ID:    "mock-msg",
					Role:  RoleAssistant,
					Parts: []Part{TextPart{Text: "Mock stream response"}},
				},
			},
		},
	}, nil
}

func TestModelProviderInterface(t *testing.T) {
	// Test that mockModelProvider implements ModelProvider interface
	var provider ModelProvider = &mockModelProvider{}

	req := &ModelRequest{
		Model: "test-model",
		Messages: []*Message{
			{
				ID:    "msg-1",
				Role:  RoleUser,
				Parts: []Part{TextPart{Text: "Hello"}},
			},
		},
	}

	// Test Generate
	resp, err := provider.Generate(context.Background(), req)
	if err != nil {
		t.Errorf("Generate() returned error: %v", err)
	}
	if resp.Message.Role != RoleAssistant {
		t.Errorf("Generate() response role = %v, want %v", resp.Message.Role, RoleAssistant)
	}

	// Test NewStream
	stream, err := provider.NewStream(context.Background(), req)
	if err != nil {
		t.Errorf("NewStream() returned error: %v", err)
	}
	if stream == nil {
		t.Errorf("NewStream() returned nil stream")
	}
}

func TestModelOption(t *testing.T) {
	opts := &ModelOptions{}

	// Test that we can create a ModelOption function
	option := func(o *ModelOptions) {
		o.Seed = 42
		o.Temperature = 0.7
		o.MaxOutputTokens = 100
	}

	// Apply the option
	option(opts)

	if opts.Seed != 42 {
		t.Errorf("ModelOptions.Seed = %v, want 42", opts.Seed)
	}
	if opts.Temperature != 0.7 {
		t.Errorf("ModelOptions.Temperature = %v, want 0.7", opts.Temperature)
	}
	if opts.MaxOutputTokens != 100 {
		t.Errorf("ModelOptions.MaxOutputTokens = %v, want 100", opts.MaxOutputTokens)
	}
}
