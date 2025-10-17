package store

import (
	"context"
	"testing"

	"github.com/go-kratos/blades/rag"
)

func TestMemoryStoreRetrieve(t *testing.T) {
	store := NewMemoryStore()

	docs := []rag.Document{
		{ID: "doc-1", Content: "Golang concurrency patterns", Metadata: map[string]any{"lang": "go"}},
		{ID: "doc-2", Content: "Golang best practices", Metadata: map[string]any{"lang": "go"}},
		{ID: "doc-3", Content: "Python concurrency tips", Metadata: map[string]any{"lang": "python"}},
	}

	if err := store.Add(context.Background(), docs); err != nil {
		t.Fatalf("unexpected add error: %v", err)
	}

	res, err := store.Retrieve(context.Background(), "golang concurrency", rag.WithTopK(2))
	if err != nil {
		t.Fatalf("retrieve failed: %v", err)
	}

	if len(res) != 2 {
		t.Fatalf("expected 2 results, got %d", len(res))
	}

	if res[0].ID != "doc-1" {
		t.Fatalf("expected doc-1 first, got %s", res[0].ID)
	}

	// BM25 分数不再是简单的 1.0，只需验证非零即可
	if res[0].Score == 0 {
		t.Fatalf("expected doc-1 to have non-zero score, got %f", res[0].Score)
	}

	if res[1].ID != "doc-2" {
		t.Fatalf("expected doc-2 second, got %s", res[1].ID)
	}

	filtered, err := store.Retrieve(context.Background(), "concurrency", rag.WithFilter("lang", "go"))
	if err != nil {
		t.Fatalf("filtered retrieve failed: %v", err)
	}

	if len(filtered) != 1 || filtered[0].ID != "doc-1" {
		t.Fatalf("expected only doc-1 after filtering, got %+v", filtered)
	}

	if err := store.Delete(context.Background(), []string{"doc-1"}); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	remaining, err := store.Retrieve(context.Background(), "golang")
	if err != nil {
		t.Fatalf("retrieve after delete failed: %v", err)
	}

	if len(remaining) != 1 || remaining[0].ID != "doc-2" {
		t.Fatalf("expected only doc-2 remaining, got %+v", remaining)
	}
}

func TestMemoryStoreAutoID(t *testing.T) {
	store := NewMemoryStore()

	if err := store.Add(context.Background(), []rag.Document{{Content: "Hello RAG"}}); err != nil {
		t.Fatalf("unexpected add error: %v", err)
	}

	docs, err := store.Retrieve(context.Background(), "hello")
	if err != nil {
		t.Fatalf("retrieve failed: %v", err)
	}

	if len(docs) != 1 {
		t.Fatalf("expected 1 document, got %d", len(docs))
	}

	if docs[0].ID == "" {
		t.Fatal("expected auto-generated document ID")
	}
}
