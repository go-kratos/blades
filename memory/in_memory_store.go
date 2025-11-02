package memory

import (
	"context"
	"strings"
	"sync"

	"github.com/go-kratos/blades"
)

// InMemoryStore is an in-memory implementation of MemoryStore.
type InMemoryStore struct {
	m        sync.RWMutex
	memories []*Memory
}

// NewInMemoryStore creates a new instance of InMemoryStore.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{}
}

// AddMemory adds a new memory to the in-memory store.
func (s *InMemoryStore) AddMemory(ctx context.Context, m *Memory) error {
	s.m.Lock()
	s.memories = append(s.memories, m)
	s.m.Unlock()
	return nil
}

// SaveSession saves the session's history as memories in the store.
func (s *InMemoryStore) SaveSession(ctx context.Context, session blades.Session) error {
	s.m.Lock()
	defer s.m.Unlock()
	for _, m := range session.History() {
		s.AddMemory(ctx, &Memory{Content: m})
	}
	return nil
}

// SearchMemory searches for memories containing the given query string.
func (s *InMemoryStore) SearchMemory(ctx context.Context, query string) ([]*Memory, error) {
	s.m.RLock()
	defer s.m.RUnlock()
	// Simple case-insensitive substring match
	words := strings.Fields(strings.ToLower(query))
	var result []*Memory
	for _, m := range s.memories {
		for _, word := range words {
			if strings.Contains(strings.ToLower(m.Content.Text()), word) {
				result = append(result, m)
				break
			}
		}
	}
	return result, nil
}
