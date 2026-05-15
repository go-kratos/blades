package memory

import (
	"context"
	"testing"
	"time"

	"github.com/go-kratos/blades/content"
)

func TestInMemoryRememberRecallForget(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mem := NewInMemory()
	entry := Entry{
		ID:       "project",
		Parts:    []content.Part{content.Text{Text: "blades uses prompt memory"}},
		Metadata: map[string]any{"scope": "repo"},
	}

	if err := mem.Remember(ctx, entry); err != nil {
		t.Fatalf("Remember() error = %v", err)
	}

	got, err := mem.Recall(ctx, Query{Text: "prompt", Filter: map[string]any{"scope": "repo"}})
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(Recall()) = %d, want 1", len(got))
	}
	if got[0].ID != entry.ID {
		t.Fatalf("Recall()[0].ID = %q, want %q", got[0].ID, entry.ID)
	}

	if err := mem.Forget(ctx, got[0]); err != nil {
		t.Fatalf("Forget() error = %v", err)
	}
	got, err = mem.Recall(ctx, Query{Text: "prompt"})
	if err != nil {
		t.Fatalf("Recall() after Forget error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len(Recall()) after Forget = %d, want 0", len(got))
	}
}

func TestInMemoryRememberRejectsEmptyParts(t *testing.T) {
	t.Parallel()

	err := NewInMemory().Remember(context.Background(), Entry{ID: "empty"})
	if err == nil {
		t.Fatal("Remember() error = nil, want error")
	}
}

func TestInMemoryForgetRequiresIDAndIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mem := NewInMemory()

	if err := mem.Forget(ctx, Entry{}); err == nil {
		t.Fatal("Forget(empty) error = nil, want error")
	}
	if err := mem.Forget(ctx, Entry{ID: "missing"}); err != nil {
		t.Fatalf("Forget(missing) error = %v, want nil", err)
	}
}

func TestInMemoryRememberUpsertsSameID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mem := NewInMemory()
	createdAt := time.Now().Add(-time.Hour).UTC()

	if err := mem.Remember(ctx, Entry{
		ID:        "same",
		Parts:     []content.Part{content.Text{Text: "old"}},
		CreatedAt: createdAt,
	}); err != nil {
		t.Fatalf("first Remember() error = %v", err)
	}
	if err := mem.Remember(ctx, Entry{
		ID:    "same",
		Parts: []content.Part{content.Text{Text: "new"}},
	}); err != nil {
		t.Fatalf("second Remember() error = %v", err)
	}

	got, err := mem.Recall(ctx, Query{Text: "new"})
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(Recall()) = %d, want 1", len(got))
	}
	if got[0].CreatedAt != createdAt {
		t.Fatalf("CreatedAt = %v, want %v", got[0].CreatedAt, createdAt)
	}
	if !got[0].UpdatedAt.After(createdAt) {
		t.Fatalf("UpdatedAt = %v, want after CreatedAt", got[0].UpdatedAt)
	}
}

func TestInMemoryCopiesEntries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mem := NewInMemory()
	parts := []content.Part{content.Text{Text: "original"}}
	metadata := map[string]any{"scope": "old"}

	if err := mem.Remember(ctx, Entry{ID: "copy", Parts: parts, Metadata: metadata}); err != nil {
		t.Fatalf("Remember() error = %v", err)
	}
	parts[0] = content.Text{Text: "changed"}
	metadata["scope"] = "new"

	got, err := mem.Recall(ctx, Query{Text: "original"})
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(Recall()) = %d, want 1", len(got))
	}

	got[0].Parts[0] = content.Text{Text: "mutated"}
	got[0].Metadata["scope"] = "mutated"

	again, err := mem.Recall(ctx, Query{Text: "original"})
	if err != nil {
		t.Fatalf("Recall() again error = %v", err)
	}
	if len(again) != 1 {
		t.Fatalf("len(Recall()) again = %d, want 1", len(again))
	}
	text := again[0].Parts[0].(content.Text).Text
	if text != "original" {
		t.Fatalf("stored text = %q, want original", text)
	}
	if again[0].Metadata["scope"] != "old" {
		t.Fatalf("stored metadata = %v, want old", again[0].Metadata["scope"])
	}
}
