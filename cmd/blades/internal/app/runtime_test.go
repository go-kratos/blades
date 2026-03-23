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

type fixedReplyModel struct {
	text string
}

func (m *fixedReplyModel) Name() string { return "fixed-reply-model" }

func (m *fixedReplyModel) Generate(context.Context, *blades.ModelRequest) (*blades.ModelResponse, error) {
	msg := blades.NewAssistantMessage(blades.StatusCompleted)
	msg.Parts = append(msg.Parts, blades.TextPart{Text: m.text})
	return &blades.ModelResponse{
		Message: msg,
	}, nil
}

func (m *fixedReplyModel) NewStreaming(context.Context, *blades.ModelRequest) blades.Generator[*blades.ModelResponse, error] {
	return func(yield func(*blades.ModelResponse, error) bool) {
		msg := blades.NewAssistantMessage(blades.StatusCompleted)
		msg.Parts = append(msg.Parts, blades.TextPart{Text: m.text})
		yield(&blades.ModelResponse{
			Message: msg,
		}, nil)
	}
}

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
	history, err := reloaded.History(context.Background())
	if err != nil {
		t.Fatalf("reloaded history: %v", err)
	}
	if got, want := len(history), 2; got != want {
		t.Fatalf("persisted history len = %d, want %d", got, want)
	}
	if got, want := history[0].Text(), "hello"; got != want {
		t.Fatalf("history[0] text = %q, want %q", got, want)
	}
	if got, want := history[1].Text(), "saved reply"; got != want {
		t.Fatalf("history[1] text = %q, want %q", got, want)
	}
}

func TestTurnExecutorRunDoesNotDuplicateHistoryForManagedBladesAgent(t *testing.T) {
	model := &fixedReplyModel{text: "saved reply"}
	agent, err := blades.NewAgent("assistant", blades.WithModel(model))
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	runner := blades.NewRunner(agent)
	sessMgr := session.NewManager(t.TempDir())

	executor := NewTurnExecutor(runner, sessMgr, TurnOptions{})
	if _, err := executor.Run(context.Background(), "chat", "hello"); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if _, err := executor.Run(context.Background(), "chat", "who are you"); err != nil {
		t.Fatalf("second run: %v", err)
	}

	sess, err := sessMgr.Get("chat")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	history, err := sess.History(context.Background())
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if got, want := len(history), 4; got != want {
		t.Fatalf("history len = %d, want %d", got, want)
	}
	if got, want := history[0].Role, blades.RoleUser; got != want {
		t.Fatalf("history[0] role = %q, want %q", got, want)
	}
	if got, want := history[0].Text(), "hello"; got != want {
		t.Fatalf("history[0] text = %q, want %q", got, want)
	}
	if got, want := history[1].Role, blades.RoleAssistant; got != want {
		t.Fatalf("history[1] role = %q, want %q", got, want)
	}
	if got, want := history[1].Text(), "saved reply"; got != want {
		t.Fatalf("history[1] text = %q, want %q", got, want)
	}
	if got, want := history[2].Role, blades.RoleUser; got != want {
		t.Fatalf("history[2] role = %q, want %q", got, want)
	}
	if got, want := history[2].Text(), "who are you"; got != want {
		t.Fatalf("history[2] text = %q, want %q", got, want)
	}
	if got, want := history[3].Role, blades.RoleAssistant; got != want {
		t.Fatalf("history[3] role = %q, want %q", got, want)
	}
	if got, want := history[3].Text(), "saved reply"; got != want {
		t.Fatalf("history[3] text = %q, want %q", got, want)
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
