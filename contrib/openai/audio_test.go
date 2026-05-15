package openai

import (
	"context"
	"errors"
	"testing"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
)

func TestNewAudioSetsModelName(t *testing.T) {
	t.Parallel()

	provider := NewAudio("gpt-4o-mini-tts", AudioConfig{Voice: "alloy"})
	if got, want := provider.Name(), "gpt-4o-mini-tts"; got != want {
		t.Fatalf("model name = %q, want %q", got, want)
	}
}

func TestAudioGenerateValidateRequest(t *testing.T) {
	t.Parallel()

	provider := &audioModel{
		model: "gpt-4o-mini-tts",
		config: AudioConfig{
			Voice: "alloy",
		},
	}
	_, err := provider.Generate(context.Background(), nil)
	if !errors.Is(err, ErrAudioRequestNil) {
		t.Fatalf("expected ErrAudioRequestNil, got %v", err)
	}
}

func TestAudioGenerateValidateModel(t *testing.T) {
	t.Parallel()

	provider := &audioModel{
		config: AudioConfig{
			Voice: "alloy",
		},
	}
	_, err := provider.Generate(context.Background(), &model.Request{
		Messages: []*model.Message{{Role: model.RoleUser, Parts: []content.Part{content.Text{Text: "hello"}}}},
	})
	if !errors.Is(err, ErrAudioModelRequired) {
		t.Fatalf("expected ErrAudioModelRequired, got %v", err)
	}
}

func TestAudioGenerateValidateVoice(t *testing.T) {
	t.Parallel()

	provider := &audioModel{
		model: "gpt-4o-mini-tts",
	}
	_, err := provider.Generate(context.Background(), &model.Request{
		Messages: []*model.Message{{Role: model.RoleUser, Parts: []content.Part{content.Text{Text: "hello"}}}},
	})
	if !errors.Is(err, ErrAudioVoiceRequired) {
		t.Fatalf("expected ErrAudioVoiceRequired, got %v", err)
	}
}
