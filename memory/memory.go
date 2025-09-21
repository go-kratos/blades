package memory

import (
	"context"
	"sync"

	"github.com/go-kratos/blades"
)

// InMemory is a simple thread-safe memory implementation that stores
// messages per conversation ID in memory. It enforces a maximum number of
// retained messages per conversation (individually per conversation ID).
type InMemory struct {
	mu          sync.RWMutex
	maxMessages int
	store       map[string][]*blades.Message
}

// NewInMemory creates a new in-memory storage. If maxMessages > 0,
// only the last maxMessages messages are retained for each conversation ID.
func NewInMemory(maxMessages int) *InMemory {
	return &InMemory{
		store:       make(map[string][]*blades.Message),
		maxMessages: maxMessages,
	}
}

// AddMessages appends messages to the given conversation and applies the per-conversation limit.
func (m *InMemory) AddMessages(ctx context.Context, id string, msgs []*blades.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[id] = append(m.store[id], msgs...)
	// Enforce maxMessages limit if set.
	if m.maxMessages > 0 {
		if n := len(m.store[id]); n > m.maxMessages {
			m.store[id] = m.store[id][n-m.maxMessages:]
		}
	}
	return nil
}

// ListMessages returns a shallow copy of the stored messages for the conversation.
func (m *InMemory) ListMessages(ctx context.Context, id string) ([]*blades.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	src := m.store[id]
	dst := make([]*blades.Message, len(src))
	copy(dst, src)
	return dst, nil
}

// Clear removes all messages for the conversation.
func (m *InMemory) Clear(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.store, id)
	return nil
}
