package memory

import (
	"context"
	"testing"

	"github.com/go-kratos/blades"
)

func TestNewInMemoryStore(t *testing.T) {
	store := NewInMemoryStore()
	if store == nil {
		t.Errorf("NewInMemoryStore() returned nil")
	}
	if store.memories.Len() != 0 {
		t.Errorf("InMemoryStore.memories should be empty initially")
	}
}

func TestInMemoryStoreAddMemory(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	message := &blades.Message{
		ID:    "msg-1",
		Role:  blades.RoleUser,
		Parts: []blades.Part{blades.TextPart{Text: "Hello, world!"}},
	}

	memory := &Memory{
		Content: message,
		Metadata: map[string]any{
			"source": "test",
		},
	}

	err := store.AddMemory(ctx, memory)
	if err != nil {
		t.Errorf("AddMemory() returned error: %v", err)
	}

	// Check that memory was added
	if store.memories.Len() != 1 {
		t.Errorf("Expected 1 memory, got %d", store.memories.Len())
	}
}

func TestInMemoryStoreSearchMemory(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	// Add some memories
	memories := []*Memory{
		{
			Content: &blades.Message{
				ID:    "msg-1",
				Role:  blades.RoleUser,
				Parts: []blades.Part{blades.TextPart{Text: "Hello, world!"}},
			},
		},
		{
			Content: &blades.Message{
				ID:    "msg-2",
				Role:  blades.RoleUser,
				Parts: []blades.Part{blades.TextPart{Text: "Goodbye, world!"}},
			},
		},
		{
			Content: &blades.Message{
				ID:    "msg-3",
				Role:  blades.RoleUser,
				Parts: []blades.Part{blades.TextPart{Text: "Hello, universe!"}},
			},
		},
	}

	for _, memory := range memories {
		err := store.AddMemory(ctx, memory)
		if err != nil {
			t.Errorf("AddMemory() returned error: %v", err)
		}
	}

	// Test search for "hello"
	results, err := store.SearchMemory(ctx, "hello")
	if err != nil {
		t.Errorf("SearchMemory() returned error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Expected 2 results for 'hello', got %d", len(results))
	}

	// Test search for "world"
	results, err = store.SearchMemory(ctx, "world")
	if err != nil {
		t.Errorf("SearchMemory() returned error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Expected 2 results for 'world', got %d", len(results))
	}

	// Test search for "goodbye"
	results, err = store.SearchMemory(ctx, "goodbye")
	if err != nil {
		t.Errorf("SearchMemory() returned error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Expected 1 result for 'goodbye', got %d", len(results))
	}

	// Test search for "nonexistent"
	results, err = store.SearchMemory(ctx, "nonexistent")
	if err != nil {
		t.Errorf("SearchMemory() returned error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Expected 0 results for 'nonexistent', got %d", len(results))
	}
}

func TestInMemoryStoreSearchMemoryCaseInsensitive(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	memory := &Memory{
		Content: &blades.Message{
			ID:    "msg-1",
			Role:  blades.RoleUser,
			Parts: []blades.Part{blades.TextPart{Text: "Hello, World!"}},
		},
	}

	err := store.AddMemory(ctx, memory)
	if err != nil {
		t.Errorf("AddMemory() returned error: %v", err)
	}

	// Test case-insensitive search
	results, err := store.SearchMemory(ctx, "HELLO")
	if err != nil {
		t.Errorf("SearchMemory() returned error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Expected 1 result for 'HELLO', got %d", len(results))
	}

	results, err = store.SearchMemory(ctx, "world")
	if err != nil {
		t.Errorf("SearchMemory() returned error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Expected 1 result for 'world', got %d", len(results))
	}
}

func TestInMemoryStoreSearchMemoryMultipleWords(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	memories := []*Memory{
		{
			Content: &blades.Message{
				ID:    "msg-1",
				Role:  blades.RoleUser,
				Parts: []blades.Part{blades.TextPart{Text: "Hello world"}},
			},
		},
		{
			Content: &blades.Message{
				ID:    "msg-2",
				Role:  blades.RoleUser,
				Parts: []blades.Part{blades.TextPart{Text: "Hello universe"}},
			},
		},
		{
			Content: &blades.Message{
				ID:    "msg-3",
				Role:  blades.RoleUser,
				Parts: []blades.Part{blades.TextPart{Text: "Goodbye world"}},
			},
		},
	}

	for _, memory := range memories {
		err := store.AddMemory(ctx, memory)
		if err != nil {
			t.Errorf("AddMemory() returned error: %v", err)
		}
	}

	// Test search with multiple words
	results, err := store.SearchMemory(ctx, "hello world")
	if err != nil {
		t.Errorf("SearchMemory() returned error: %v", err)
	}
	// The search should find memories that contain any of the words
	// So "hello world" should match memories containing "hello" OR "world"
	if len(results) < 1 {
		t.Errorf("Expected at least 1 result for 'hello world', got %d", len(results))
	}

	// Test search with single word that matches multiple
	results, err = store.SearchMemory(ctx, "world")
	if err != nil {
		t.Errorf("SearchMemory() returned error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Expected 2 results for 'world', got %d", len(results))
	}
}

func TestInMemoryStoreEmptySearch(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	// Test search with empty query
	results, err := store.SearchMemory(ctx, "")
	if err != nil {
		t.Errorf("SearchMemory() returned error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Expected 0 results for empty query, got %d", len(results))
	}
}

func TestInMemoryStoreInterface(t *testing.T) {
	// Test that InMemoryStore implements MemoryStore interface
	var store MemoryStore = NewInMemoryStore()
	if store == nil {
		t.Errorf("InMemoryStore should implement MemoryStore interface")
	}
}
