package memory

import (
	"context"
	"strings"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/generics"
)

// InMemoryStore is an in-memory implementation of MemoryStore.
type InMemoryStore struct {
	memories generics.Slice[*Memory]
}

// NewInMemoryStore creates a new instance of InMemoryStore.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{}
}

// AddMemory adds a new memory to the in-memory store.
func (s *InMemoryStore) AddMemory(ctx context.Context, m *Memory) error {
	s.memories.Append(m)
	return nil
}

func (s *InMemoryStore) SaveSession(ctx context.Context, session *blades.Session) error {
	session.History.Range(func(_ int, m *blades.Message) bool {
		s.memories.Append(&Memory{
			Content: m,
		})
		return true
	})
	return nil
}

// SearchMemory searches for memories containing the given query string.
func (s *InMemoryStore) SearchMemory(ctx context.Context, query string) ([]*Memory, error) {
	// Simple case-insensitive substring match
	words := strings.Fields(strings.ToLower(query))
	sets := generics.NewSet[*Memory]()
	s.memories.Range(func(i int, m *Memory) bool {
		for _, word := range words {
			if strings.Contains(strings.ToLower(m.Content.Text()), word) {
				sets.Insert(m)
				break
			}
		}
		return true
	})
	return sets.ToSlice(), nil
}
