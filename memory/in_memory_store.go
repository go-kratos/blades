package memory

import (
	"context"
	"strings"
	"sync"
)

// InMemoryStore is an in-memory implementation of MemoryStore.
type InMemoryStore struct {
	mu       sync.RWMutex
	memories []*Memory
}

// NewInMemoryStore creates a new instance of InMemoryStore.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		memories: make([]*Memory, 0),
	}
}

// AddMemory adds a new memory to the in-memory store.
func (s *InMemoryStore) AddMemory(ctx context.Context, m *Memory) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.memories = append(s.memories, m)
	return nil
}

// SearchMemory searches for memories containing the given query string.
func (s *InMemoryStore) SearchMemory(ctx context.Context, query string) ([]*Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var results []*Memory
	// Simple case-insensitive substring match
	words := strings.Fields(strings.ToLower(query))
	for _, word := range words {
		for _, m := range s.memories {
			if strings.Contains(strings.ToLower(m.Content.Text()), word) {
				results = append(results, m)
			}
		}
	}
	return results, nil
}
