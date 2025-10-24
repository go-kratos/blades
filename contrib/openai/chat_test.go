package openai

import (
	"context"
	"testing"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/openai/openai-go/v2"
)

func TestChatOptions(t *testing.T) {
	opts := &ChatOptions{}

	// Test default values
	if opts.ReasoningEffort != "" {
		t.Errorf("ChatOptions.ReasoningEffort = %v, want empty string", opts.ReasoningEffort)
	}
	if opts.RequestOpts != nil {
		t.Errorf("ChatOptions.RequestOpts = %v, want nil", opts.RequestOpts)
	}
}

func TestWithReasoningEffort(t *testing.T) {
	opts := &ChatOptions{}

	// Test WithReasoningEffort
	option := WithReasoningEffort("high")
	option(opts)

	if opts.ReasoningEffort != "high" {
		t.Errorf("ChatOptions.ReasoningEffort = %v, want high", opts.ReasoningEffort)
	}
}

func TestWithChatOptions(t *testing.T) {
	opts := &ChatOptions{}

	// Test WithChatOptions
	option := WithChatOptions()
	option(opts)

	if opts.RequestOpts == nil {
		t.Errorf("ChatOptions.RequestOpts should not be nil")
	}
}

func TestNewChatProvider(t *testing.T) {
	provider := NewChatProvider()
	if provider == nil {
		t.Errorf("NewChatProvider() returned nil")
	}

	// Test that provider implements ModelProvider interface
	var _ blades.ModelProvider = provider
}

func TestNewChatProviderWithOptions(t *testing.T) {
	provider := NewChatProvider(
		WithReasoningEffort("medium"),
		WithChatOptions(),
	)
	if provider == nil {
		t.Errorf("NewChatProvider() returned nil")
	}

	// Test that provider implements ModelProvider interface
	var _ blades.ModelProvider = provider
}

func TestChatProviderToChatCompletionParams(t *testing.T) {
	provider := &ChatProvider{}

	req := &blades.ModelRequest{
		Model: "test-model",
		Messages: []*blades.Message{
			{
				ID:    "msg-1",
				Role:  blades.RoleUser,
				Parts: []blades.Part{blades.TextPart{Text: "Hello"}},
			},
		},
	}

	opt := blades.ModelOptions{
		Seed:             42,
		MaxOutputTokens:  100,
		Temperature:      0.7,
		FrequencyPenalty: 0.1,
		PresencePenalty:  0.1,
		TopP:             0.9,
		StopSequences:    []string{"stop"},
	}

	params, err := provider.toChatCompletionParams(req, opt)
	if err != nil {
		t.Errorf("toChatCompletionParams returned error: %v", err)
	}

	if params.Model != "test-model" {
		t.Errorf("Params.Model = %v, want test-model", params.Model)
	}
	if len(params.Messages) != 1 {
		t.Errorf("Params.Messages length = %v, want 1", len(params.Messages))
	}
}

func TestChatProviderToChatCompletionParamsWithTools(t *testing.T) {
	provider := &ChatProvider{}

	tool := &tools.Tool{
		Name:        "test-tool",
		Description: "A test tool",
		InputSchema: &jsonschema.Schema{Type: "object"},
	}

	req := &blades.ModelRequest{
		Model: "test-model",
		Tools: []*tools.Tool{tool},
		Messages: []*blades.Message{
			{
				ID:    "msg-1",
				Role:  blades.RoleUser,
				Parts: []blades.Part{blades.TextPart{Text: "Hello"}},
			},
		},
	}

	opt := blades.ModelOptions{}

	params, err := provider.toChatCompletionParams(req, opt)
	if err != nil {
		t.Errorf("toChatCompletionParams returned error: %v", err)
	}

	if len(params.Tools) != 1 {
		t.Errorf("Params.Tools length = %v, want 1", len(params.Tools))
	}
}

func TestChatProviderToChatCompletionParamsWithOutputSchema(t *testing.T) {
	provider := &ChatProvider{}

	schema := &jsonschema.Schema{
		Type:        "object",
		Title:       "TestSchema",
		Description: "A test schema",
	}

	req := &blades.ModelRequest{
		Model:        "test-model",
		OutputSchema: schema,
		Messages: []*blades.Message{
			{
				ID:    "msg-1",
				Role:  blades.RoleUser,
				Parts: []blades.Part{blades.TextPart{Text: "Hello"}},
			},
		},
	}

	opt := blades.ModelOptions{}

	params, err := provider.toChatCompletionParams(req, opt)
	if err != nil {
		t.Errorf("toChatCompletionParams returned error: %v", err)
	}

	if params.ResponseFormat.OfJSONSchema == nil {
		t.Errorf("Params.ResponseFormat.OfJSONSchema should not be nil")
	}
}

func TestToTools(t *testing.T) {
	tool := &tools.Tool{
		Name:        "test-tool",
		Description: "A test tool",
		InputSchema: &jsonschema.Schema{Type: "object"},
	}

	tools := []*tools.Tool{tool}

	params, err := toTools(tools)
	if err != nil {
		t.Errorf("toTools returned error: %v", err)
	}

	if len(params) != 1 {
		t.Errorf("Expected 1 tool param, got %d", len(params))
	}
}

func TestToToolsEmpty(t *testing.T) {
	params, err := toTools(nil)
	if err != nil {
		t.Errorf("toTools returned error: %v", err)
	}
	if params != nil {
		t.Errorf("Expected nil params for empty tools, got %v", params)
	}
}

func TestToTextParts(t *testing.T) {
	message := &blades.Message{
		Parts: []blades.Part{
			blades.TextPart{Text: "Hello"},
			blades.TextPart{Text: "World"},
		},
	}

	parts := toTextParts(message)
	if len(parts) != 2 {
		t.Errorf("Expected 2 text parts, got %d", len(parts))
	}
	if parts[0].Text != "Hello" {
		t.Errorf("First part text = %v, want Hello", parts[0].Text)
	}
	if parts[1].Text != "World" {
		t.Errorf("Second part text = %v, want World", parts[1].Text)
	}
}

func TestToContentParts(t *testing.T) {
	message := &blades.Message{
		Parts: []blades.Part{
			blades.TextPart{Text: "Hello"},
			blades.FilePart{
				Name:     "test.jpg",
				URI:      "file://test.jpg",
				MIMEType: blades.MIMEImageJPEG,
			},
			blades.DataPart{
				Name:     "test.png",
				Bytes:    []byte("fake image data"),
				MIMEType: blades.MIMEImagePNG,
			},
		},
	}

	parts := toContentParts(message)
	if len(parts) != 3 {
		t.Errorf("Expected 3 content parts, got %d", len(parts))
	}
}

func TestToToolCallMessage(t *testing.T) {
	message := &blades.Message{
		Role: blades.RoleTool,
		Parts: []blades.Part{
			blades.ToolPart{
				ID:      "tool-1",
				Name:    "test-tool",
				Request: `{"arg": "value"}`,
			},
		},
	}

	param := toToolCallMessage(message)
	if param.OfAssistant == nil {
		t.Errorf("Expected assistant message param")
	}
	if len(param.OfAssistant.ToolCalls) != 1 {
		t.Errorf("Expected 1 tool call, got %d", len(param.OfAssistant.ToolCalls))
	}
}

func TestChoiceToResponse(t *testing.T) {
	// This is a simplified test since we can't easily mock the OpenAI response
	// In a real test, we would need to create proper mock responses
	ctx := context.Background()
	params := openai.ChatCompletionNewParams{}
	choices := []openai.ChatCompletionChoice{}

	_, err := choiceToResponse(ctx, params, choices)
	if err != nil {
		t.Errorf("choiceToResponse returned error: %v", err)
	}
}

func TestChunkChoiceToResponse(t *testing.T) {
	// This is a simplified test since we can't easily mock the OpenAI response
	ctx := context.Background()
	choices := []openai.ChatCompletionChunkChoice{}

	_, err := chunkChoiceToResponse(ctx, choices)
	if err != nil {
		t.Errorf("chunkChoiceToResponse returned error: %v", err)
	}
}
