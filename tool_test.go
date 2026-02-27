package blades

import (
	"context"
	"errors"
	"testing"
)

type scriptedAgent struct {
	name     string
	messages []*Message
	err      error
}

func (a *scriptedAgent) Name() string {
	if a.name != "" {
		return a.name
	}
	return "scripted"
}

func (a *scriptedAgent) Description() string { return "scripted agent" }

func (a *scriptedAgent) Run(ctx context.Context, inv *Invocation) Generator[*Message, error] {
	return func(yield func(*Message, error) bool) {
		for _, msg := range a.messages {
			if !yield(msg, nil) {
				return
			}
		}
		if a.err != nil {
			yield(nil, a.err)
		}
	}
}

func TestAgentToolHandleReturnsFinalCompletedAssistant(t *testing.T) {
	t.Parallel()

	first := NewAssistantMessage(StatusInProgress)
	first.Parts = append(first.Parts, TextPart{Text: "first"})
	second := NewAssistantMessage(StatusCompleted)
	second.Parts = append(second.Parts, TextPart{Text: "second"})

	tool := NewAgentTool(&scriptedAgent{
		messages: []*Message{first, second},
	})
	got, err := tool.Handle(context.Background(), "input")
	if err != nil {
		t.Fatalf("handle returned error: %v", err)
	}
	if got != "second" {
		t.Fatalf("expected final completed message, got %q", got)
	}
}

func TestAgentToolHandleFallsBackToLastMessage(t *testing.T) {
	t.Parallel()

	tool := NewAgentTool(&scriptedAgent{
		messages: []*Message{AssistantMessage("only")},
	})
	got, err := tool.Handle(context.Background(), "input")
	if err != nil {
		t.Fatalf("handle returned error: %v", err)
	}
	if got != "only" {
		t.Fatalf("expected last message text, got %q", got)
	}
}

func TestAgentToolHandleReturnsNoFinalResponse(t *testing.T) {
	t.Parallel()

	tool := NewAgentTool(&scriptedAgent{})
	_, err := tool.Handle(context.Background(), "input")
	if !errors.Is(err, ErrNoFinalResponse) {
		t.Fatalf("expected ErrNoFinalResponse, got %v", err)
	}
}

func TestAgentToolHandleReturnsStreamError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("stream failed")
	tool := NewAgentTool(&scriptedAgent{err: wantErr})
	_, err := tool.Handle(context.Background(), "input")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected stream error, got %v", err)
	}
}
