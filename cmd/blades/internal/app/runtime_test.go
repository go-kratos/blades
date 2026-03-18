package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/cmd/blades/internal/channel"
	"github.com/go-kratos/blades/cmd/blades/internal/session"
)

type fixedReplyAgent struct {
	text string
}

func (a *fixedReplyAgent) Name() string        { return "fixed-reply" }
func (a *fixedReplyAgent) Description() string { return "" }
func (a *fixedReplyAgent) Run(context.Context, *blades.Invocation) blades.Generator[*blades.Message, error] {
	return func(yield func(*blades.Message, error) bool) {
		yield(blades.AssistantMessage(a.text), nil)
	}
}

type toolStreamingAgent struct{}

func (a *toolStreamingAgent) Name() string        { return "tool-streaming" }
func (a *toolStreamingAgent) Description() string { return "" }
func (a *toolStreamingAgent) Run(context.Context, *blades.Invocation) blades.Generator[*blades.Message, error] {
	return func(yield func(*blades.Message, error) bool) {
		request := `{"q":"same"}`
		yield(&blades.Message{
			Role:   blades.RoleAssistant,
			Status: blades.StatusInProgress,
			Parts: []blades.Part{
				blades.NewToolPart("", "search", request),
				blades.NewToolPart("", "search", request),
			},
		}, nil)
		yield(&blades.Message{
			Role:   blades.RoleAssistant,
			Status: blades.StatusCompleted,
			Parts: []blades.Part{
				blades.TextPart{Text: "done"},
				blades.ToolPart{Name: "search", Request: request, Response: "result-1", Completed: true},
				blades.ToolPart{Name: "search", Request: request, Response: "result-2", Completed: true},
			},
		}, nil)
	}
}

type eventCaptureWriter struct {
	events []channel.Event
}

func (w *eventCaptureWriter) WriteText(string)           {}
func (w *eventCaptureWriter) WriteEvent(e channel.Event) { w.events = append(w.events, e) }

func TestToolEventKey(t *testing.T) {
	if got := ToolEventKey(blades.ToolPart{ID: "known"}, 1); got != "known" {
		t.Fatalf("ToolEventKey with ID = %q, want known", got)
	}
	if got := ToolEventKey(blades.ToolPart{Name: "search", Request: `{"q":"x"}`}, 2); got != "search\n{\"q\":\"x\"}\n#2" {
		t.Fatalf("ToolEventKey without ID = %q", got)
	}
}

func TestTurnExecutorRunPersistsSession(t *testing.T) {
	runner := blades.NewRunner(&fixedReplyAgent{text: "saved reply"})
	sessionsDir := t.TempDir()
	sessMgr := session.NewManager(sessionsDir)

	reply, err := NewTurnExecutor(runner, sessMgr, TurnOptions{}).Run(context.Background(), "cron-session", "hello")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if reply != "saved reply" {
		t.Fatalf("reply = %q, want %q", reply, "saved reply")
	}

	reloaded, err := session.NewManager(sessionsDir).Get("cron-session")
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	if got := len(reloaded.History()); got == 0 {
		t.Fatalf("expected persisted session history, got %d messages", got)
	}
}

func TestTurnExecutorStreamHandlerTracksDuplicateToolRequestsSeparately(t *testing.T) {
	runner := blades.NewRunner(&toolStreamingAgent{})
	sessMgr := session.NewManager(t.TempDir())
	writer := &eventCaptureWriter{}

	reply, err := NewTurnExecutor(runner, sessMgr, TurnOptions{}).StreamHandler()(context.Background(), "dup-tools", "hello", writer)
	if err != nil {
		t.Fatalf("StreamHandler: %v", err)
	}
	if reply != "done" {
		t.Fatalf("reply = %q, want %q", reply, "done")
	}
	if got := len(writer.events); got != 4 {
		t.Fatalf("event count = %d, want 4", got)
	}
	if writer.events[0].ID == writer.events[1].ID {
		t.Fatalf("duplicate tool starts share ID %q", writer.events[0].ID)
	}
	if writer.events[2].ID != writer.events[0].ID {
		t.Fatalf("first tool end ID = %q, want %q", writer.events[2].ID, writer.events[0].ID)
	}
	if writer.events[3].ID != writer.events[1].ID {
		t.Fatalf("second tool end ID = %q, want %q", writer.events[3].ID, writer.events[1].ID)
	}
}

func TestTurnExecutorRunReturnsSessionLoadError(t *testing.T) {
	sessionsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sessionsDir, "broken.json"), []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("write corrupt session: %v", err)
	}

	runner := blades.NewRunner(&fixedReplyAgent{text: "unused"})
	_, err := NewTurnExecutor(runner, session.NewManager(sessionsDir), TurnOptions{}).Run(context.Background(), "broken", "hello")
	if err == nil {
		t.Fatal("expected corrupt session load error")
	}
}
