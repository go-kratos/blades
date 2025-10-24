package openai

import (
	"testing"

	"github.com/go-kratos/blades"
	"github.com/openai/openai-go/v2"
)

func TestImageOptions(t *testing.T) {
	opts := &ImageOptions{}

	// Test default values
	if opts.RequestOpts != nil {
		t.Errorf("ImageOptions.RequestOpts = %v, want nil", opts.RequestOpts)
	}
}

func TestWithImageOptions(t *testing.T) {
	opts := &ImageOptions{}

	// Test WithImageOptions
	option := WithImageOptions()
	option(opts)

	if opts.RequestOpts == nil {
		t.Errorf("ImageOptions.RequestOpts should not be nil")
	}
}

func TestNewImageProvider(t *testing.T) {
	provider := NewImageProvider()
	if provider == nil {
		t.Errorf("NewImageProvider() returned nil")
	}

	// Test that provider implements ModelProvider interface
	var _ blades.ModelProvider = provider
}

func TestNewImageProviderWithOptions(t *testing.T) {
	provider := NewImageProvider(
		WithImageOptions(),
	)
	if provider == nil {
		t.Errorf("NewImageProvider() returned nil")
	}

	// Test that provider implements ModelProvider interface
	var _ blades.ModelProvider = provider
}

func TestImageProviderApplyOptions(t *testing.T) {
	provider := &ImageProvider{}
	params := &openai.ImageGenerateParams{}

	opts := blades.ImageOptions{
		Background:        "white",
		Size:              "1024x1024",
		Quality:           "hd",
		ResponseFormat:    "url",
		OutputFormat:      "png",
		Moderation:        "auto",
		Style:             "vivid",
		User:              "test-user",
		Count:             2,
		PartialImages:     1,
		OutputCompression: 50,
	}

	err := provider.applyOptions(params, opts)
	if err != nil {
		t.Errorf("applyOptions returned error: %v", err)
	}

	// Note: We can't easily test the actual parameter values without
	// accessing the internal structure, but we can test that no error occurred
}

func TestImageProviderApplyOptionsEmpty(t *testing.T) {
	provider := &ImageProvider{}
	params := &openai.ImageGenerateParams{}

	opts := blades.ImageOptions{}

	err := provider.applyOptions(params, opts)
	if err != nil {
		t.Errorf("applyOptions returned error: %v", err)
	}
}

func TestToImageResponse(t *testing.T) {
	// This is a simplified test since we can't easily mock the OpenAI response
	// In a real test, we would need to create proper mock responses
	res := &openai.ImagesResponse{}

	_, err := toImageResponse(res)
	if err == nil {
		t.Errorf("toImageResponse should return error for empty response")
	}
	if err != ErrImageGenerationEmpty {
		t.Errorf("Expected ErrImageGenerationEmpty, got %v", err)
	}
}

func TestToImageResponseNil(t *testing.T) {
	_, err := toImageResponse(nil)
	if err == nil {
		t.Errorf("toImageResponse should return error for nil response")
	}
	if err != ErrImageGenerationEmpty {
		t.Errorf("Expected ErrImageGenerationEmpty, got %v", err)
	}
}

func TestImageMimeType(t *testing.T) {
	tests := []struct {
		name     string
		format   openai.ImagesResponseOutputFormat
		expected blades.MIMEType
	}{
		{"JPEG format", openai.ImagesResponseOutputFormatJPEG, blades.MIMEImageJPEG},
		{"WebP format", openai.ImagesResponseOutputFormatWebP, blades.MIMEImageWEBP},
		{"Default format", openai.ImagesResponseOutputFormat("unknown"), blades.MIMEImagePNG},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := imageMimeType(tt.format)
			if result != tt.expected {
				t.Errorf("imageMimeType(%v) = %v, want %v", tt.format, result, tt.expected)
			}
		})
	}
}

func TestPromptFromMessages(t *testing.T) {
	// Test with user message
	messages := []*blades.Message{
		{
			ID:    "msg-1",
			Role:  blades.RoleUser,
			Parts: []blades.Part{blades.TextPart{Text: "Hello"}},
		},
	}

	prompt, err := promptFromMessages(messages)
	if err != nil {
		t.Errorf("promptFromMessages returned error: %v", err)
	}
	if prompt != "Hello" {
		t.Errorf("promptFromMessages = %v, want Hello", prompt)
	}
}

func TestPromptFromMessagesMultiple(t *testing.T) {
	// Test with multiple messages
	messages := []*blades.Message{
		{
			ID:    "msg-1",
			Role:  blades.RoleUser,
			Parts: []blades.Part{blades.TextPart{Text: "Hello"}},
		},
		{
			ID:    "msg-2",
			Role:  blades.RoleUser,
			Parts: []blades.Part{blades.TextPart{Text: "World"}},
		},
	}

	prompt, err := promptFromMessages(messages)
	if err != nil {
		t.Errorf("promptFromMessages returned error: %v", err)
	}
	if prompt != "Hello\nWorld" {
		t.Errorf("promptFromMessages = %v, want Hello\\nWorld", prompt)
	}
}

func TestPromptFromMessagesEmpty(t *testing.T) {
	// Test with empty messages
	messages := []*blades.Message{}

	_, err := promptFromMessages(messages)
	if err == nil {
		t.Errorf("promptFromMessages should return error for empty messages")
	}
	if err != ErrPromptRequired {
		t.Errorf("Expected ErrPromptRequired, got %v", err)
	}
}

func TestPromptFromMessagesNoText(t *testing.T) {
	// Test with messages that have no text parts
	messages := []*blades.Message{
		{
			ID:    "msg-1",
			Role:  blades.RoleUser,
			Parts: []blades.Part{blades.FilePart{Name: "test.jpg"}},
		},
	}

	_, err := promptFromMessages(messages)
	if err == nil {
		t.Errorf("promptFromMessages should return error for messages with no text")
	}
	if err != ErrPromptRequired {
		t.Errorf("Expected ErrPromptRequired, got %v", err)
	}
}
