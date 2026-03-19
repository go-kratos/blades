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

// captureAgent records the invocation it receives so tests can inspect it.
type captureAgent struct {
	scriptedAgent
	captured *Invocation
}

func (a *captureAgent) Run(ctx context.Context, inv *Invocation) Generator[*Message, error] {
	a.captured = inv
	return a.scriptedAgent.Run(ctx, inv)
}

func TestAgentToolHandleIsolatesSession(t *testing.T) {
	t.Parallel()

	parentSession := NewSession()
	parentCtx := NewSessionContext(context.Background(), parentSession)

	sub := &captureAgent{
		scriptedAgent: scriptedAgent{
			messages: []*Message{AssistantMessage("hello")},
		},
	}
	tool := NewAgentTool(sub)
	if _, err := tool.Handle(parentCtx, "hi"); err != nil {
		t.Fatalf("handle returned error: %v", err)
	}

	// Sub-agent must NOT have written to the parent session.
	history, err := parentSession.History(context.Background())
	if err != nil {
		t.Fatalf("unexpected error reading parent history: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("parent session should be empty, got %d message(s)", len(history))
	}
}

func TestAgentToolHandleSetsCallerAuthor(t *testing.T) {
	t.Parallel()

	sub := &captureAgent{
		scriptedAgent: scriptedAgent{
			messages: []*Message{AssistantMessage("hello")},
		},
	}
	tool := NewAgentTool(sub)

	// When a calling agent is in context, its name should appear as Author.
	callerAgent := &scriptedAgent{name: "caller"}
	ctx := NewAgentContext(context.Background(), callerAgent)
	if _, err := tool.Handle(ctx, "hi"); err != nil {
		t.Fatalf("handle returned error: %v", err)
	}
	if sub.captured == nil || sub.captured.Message == nil {
		t.Fatal("sub-agent did not capture an invocation")
	}
	if got := sub.captured.Message.Author; got != "caller" {
		t.Fatalf("expected Author %q, got %q", "caller", got)
	}
}

func TestAgentToolHandleDefaultsAuthorToUser(t *testing.T) {
	t.Parallel()

	sub := &captureAgent{
		scriptedAgent: scriptedAgent{
			messages: []*Message{AssistantMessage("hello")},
		},
	}
	tool := NewAgentTool(sub)

	// Without an agent context the Author should stay "user".
	if _, err := tool.Handle(context.Background(), "hi"); err != nil {
		t.Fatalf("handle returned error: %v", err)
	}
	if sub.captured == nil || sub.captured.Message == nil {
		t.Fatal("sub-agent did not capture an invocation")
	}
	if got := sub.captured.Message.Author; got != "user" {
		t.Fatalf("expected Author %q, got %q", "user", got)
	}
}
