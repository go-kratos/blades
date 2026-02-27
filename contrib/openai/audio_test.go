package openai

import (
	"context"
	"errors"
	"testing"

	"github.com/go-kratos/blades"
)

func TestNewAudioSetsModelName(t *testing.T) {
	t.Parallel()

	model := NewAudio("gpt-4o-mini-tts", AudioConfig{Voice: "alloy"})
	if got, want := model.Name(), "gpt-4o-mini-tts"; got != want {
		t.Fatalf("model name = %q, want %q", got, want)
	}
}

func TestAudioGenerateValidateRequest(t *testing.T) {
	t.Parallel()

	model := &audioModel{
		model: "gpt-4o-mini-tts",
		config: AudioConfig{
			Voice: "alloy",
		},
	}
	_, err := model.Generate(context.Background(), nil)
	if !errors.Is(err, ErrAudioRequestNil) {
		t.Fatalf("expected ErrAudioRequestNil, got %v", err)
	}
}

func TestAudioGenerateValidateModel(t *testing.T) {
	t.Parallel()

	model := &audioModel{
		config: AudioConfig{
			Voice: "alloy",
		},
	}
	_, err := model.Generate(context.Background(), &blades.ModelRequest{
		Messages: []*blades.Message{blades.UserMessage("hello")},
	})
	if !errors.Is(err, ErrAudioModelRequired) {
		t.Fatalf("expected ErrAudioModelRequired, got %v", err)
	}
}

func TestAudioGenerateValidateVoice(t *testing.T) {
	t.Parallel()

	model := &audioModel{
		model: "gpt-4o-mini-tts",
	}
	_, err := model.Generate(context.Background(), &blades.ModelRequest{
		Messages: []*blades.Message{blades.UserMessage("hello")},
	})
	if !errors.Is(err, ErrAudioVoiceRequired) {
		t.Fatalf("expected ErrAudioVoiceRequired, got %v", err)
	}
}
