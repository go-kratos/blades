package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-kratos/blades"
)

func TestManagerGetOrNewUsesRequestedSessionID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mgr := NewManager(dir)
	sess := mgr.GetOrNew("chat")

	if got, want := sess.ID(), "chat"; got != want {
		t.Fatalf("session id = %q, want %q", got, want)
	}
	if err := mgr.Save(sess); err != nil {
		t.Fatalf("save session: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "chat.json")); err != nil {
		t.Fatalf("stat session file: %v", err)
	}
}

func TestManagerSaveAndReloadPreservesHistory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mgr := NewManager(dir)
	sess := mgr.GetOrNew("chat")
	sess.SetState("topic", "history")

	if err := sess.Append(context.Background(), blades.UserMessage("hello")); err != nil {
		t.Fatalf("append user message: %v", err)
	}
	assistant := blades.NewAssistantMessage(blades.StatusCompleted)
	assistant.Parts = append(assistant.Parts,
		blades.TextPart{Text: "world"},
		blades.ToolPart{
			ID:        "call_1",
			Name:      "echo",
			Request:   `{"value":"hello"}`,
			Response:  `{"value":"world"}`,
			Completed: true,
		},
	)
	if err := sess.Append(context.Background(), assistant); err != nil {
		t.Fatalf("append assistant message: %v", err)
	}

	if err := mgr.Save(sess); err != nil {
		t.Fatalf("save session: %v", err)
	}

	reloaded, err := NewManager(dir).Get("chat")
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	if got, want := reloaded.ID(), "chat"; got != want {
		t.Fatalf("reloaded session id = %q, want %q", got, want)
	}
	if got, want := reloaded.State()["topic"], "history"; got != want {
		t.Fatalf("reloaded state = %v, want %v", got, want)
	}

	history := reloaded.History()
	if got, want := len(history), 2; got != want {
		t.Fatalf("history len = %d, want %d", got, want)
	}
	if got, want := history[0].Text(), "hello"; got != want {
		t.Fatalf("first history text = %q, want %q", got, want)
	}
	if got, want := history[1].Text(), "world"; got != want {
		t.Fatalf("second history text = %q, want %q", got, want)
	}
	if got, want := len(history[1].Parts), 2; got != want {
		t.Fatalf("second history parts = %d, want %d", got, want)
	}
	toolPart, ok := history[1].Parts[1].(blades.ToolPart)
	if !ok {
		t.Fatalf("part type = %T, want blades.ToolPart", history[1].Parts[1])
	}
	if got, want := toolPart.Name, "echo"; got != want {
		t.Fatalf("tool part name = %q, want %q", got, want)
	}
	if got, want := toolPart.Completed, true; got != want {
		t.Fatalf("tool part completed = %t, want %t", got, want)
	}
}
