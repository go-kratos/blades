package memory

import (
	"context"
	"testing"
	"time"

	"github.com/go-kratos/blades"
)

func TestInMemoryStoreSaveSession(t *testing.T) {
	t.Parallel()

	session := blades.NewSession()
	if err := session.Append(context.Background(), blades.UserMessage("hi")); err != nil {
		t.Fatalf("append user message: %v", err)
	}
	if err := session.Append(context.Background(), blades.AssistantMessage("hello")); err != nil {
		t.Fatalf("append assistant message: %v", err)
	}

	store := NewInMemoryStore()

	done := make(chan error, 1)
	go func() {
		done <- store.SaveSession(context.Background(), session)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SaveSession returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SaveSession timed out")
	}

	memories, err := store.SearchMemory(context.Background(), "hi")
	if err != nil {
		t.Fatalf("SearchMemory returned error: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 matched memory, got %d", len(memories))
	}
}
