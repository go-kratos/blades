package openai

import (
	"testing"

	"github.com/go-kratos/blades"
	"github.com/openai/openai-go/v2"
)

func TestAudioOptions(t *testing.T) {
	opts := &AudioOptions{}

	// Test default values
	if opts.RequestOpts != nil {
		t.Errorf("AudioOptions.RequestOpts = %v, want nil", opts.RequestOpts)
	}
}

func TestWithAudioOptions(t *testing.T) {
	opts := &AudioOptions{}

	// Test WithAudioOptions
	option := WithAudioOptions()
	option(opts)

	if opts.RequestOpts == nil {
		t.Errorf("AudioOptions.RequestOpts should not be nil")
	}
}

func TestNewAudioProvider(t *testing.T) {
	provider := NewAudioProvider()
	if provider == nil {
		t.Errorf("NewAudioProvider() returned nil")
	}

	// Test that provider implements ModelProvider interface
	var _ blades.ModelProvider = provider
}

func TestNewAudioProviderWithOptions(t *testing.T) {
	provider := NewAudioProvider(
		WithAudioOptions(),
	)
	if provider == nil {
		t.Errorf("NewAudioProvider() returned nil")
	}

	// Test that provider implements ModelProvider interface
	var _ blades.ModelProvider = provider
}

func TestAudioProviderApplyOptions(t *testing.T) {
	provider := &AudioProvider{}
	params := &openai.AudioSpeechNewParams{}

	opts := blades.AudioOptions{
		ResponseFormat: "mp3",
		StreamFormat:   "mp3",
		Instructions:   "Speak clearly",
		Speed:          1.2,
	}

	err := provider.applyOptions(params, opts)
	if err != nil {
		t.Errorf("applyOptions returned error: %v", err)
	}

	// Note: We can't easily test the actual parameter values without
	// accessing the internal structure, but we can test that no error occurred
}

func TestAudioProviderApplyOptionsEmpty(t *testing.T) {
	provider := &AudioProvider{}
	params := &openai.AudioSpeechNewParams{}

	opts := blades.AudioOptions{}

	err := provider.applyOptions(params, opts)
	if err != nil {
		t.Errorf("applyOptions returned error: %v", err)
	}
}

func TestAudioMimeType(t *testing.T) {
	tests := []struct {
		name     string
		format   openai.AudioSpeechNewParamsResponseFormat
		expected blades.MIMEType
	}{
		{"MP3 format", openai.AudioSpeechNewParamsResponseFormat("mp3"), blades.MIMEAudioMP3},
		{"WAV format", openai.AudioSpeechNewParamsResponseFormat("wav"), blades.MIMEAudioWAV},
		{"Opus format", openai.AudioSpeechNewParamsResponseFormat("opus"), blades.MIMEAudioOpus},
		{"AAC format", openai.AudioSpeechNewParamsResponseFormat("aac"), blades.MIMEAudioAAC},
		{"FLAC format", openai.AudioSpeechNewParamsResponseFormat("flac"), blades.MIMEAudioFLAC},
		{"PCM format", openai.AudioSpeechNewParamsResponseFormat("pcm"), blades.MIMEAudioPCM},
		{"Default format", openai.AudioSpeechNewParamsResponseFormat("unknown"), blades.MIMEAudioMP3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := audioMimeType(tt.format)
			if result != tt.expected {
				t.Errorf("audioMimeType(%v) = %v, want %v", tt.format, result, tt.expected)
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
