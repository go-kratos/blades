package memory

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/go-kratos/blades/content"
)

// InMemoryStore is a simple in-memory implementation of Store.
type InMemoryStore struct {
	mu      sync.RWMutex
	entries []*Entry
}

// NewInMemoryStore creates a new in-memory store.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{}
}

func (s *InMemoryStore) Put(_ context.Context, entry *Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	entry.UpdatedAt = time.Now()
	s.entries = append(s.entries, entry)
	return nil
}

func (s *InMemoryStore) Search(_ context.Context, query Query, _ ...SearchOption) ([]*Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var results []*Entry
	limit := query.Limit
	if limit <= 0 {
		limit = 10
	}
	for _, e := range s.entries {
		if query.Text != "" {
			text := partsToText(e.Parts)
			if !strings.Contains(strings.ToLower(text), strings.ToLower(query.Text)) {
				continue
			}
		}
		results = append(results, e)
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

func (s *InMemoryStore) Delete(_ context.Context, opts ...DeleteOption) error {
	o := &deleteOptions{}
	for _, opt := range opts {
		opt(o)
	}
	if len(o.ids) == 0 && len(o.filter) == 0 {
		return errors.New("memory: Forget requires at least IDs or Filter")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idSet := make(map[string]struct{}, len(o.ids))
	for _, id := range o.ids {
		idSet[id] = struct{}{}
	}
	filtered := s.entries[:0]
	for _, e := range s.entries {
		if _, ok := idSet[e.ID]; ok {
			continue
		}
		filtered = append(filtered, e)
	}
	s.entries = filtered
	return nil
}

func partsToText(parts []content.Part) string {
	var buf strings.Builder
	for _, p := range parts {
		if t, ok := p.(content.Text); ok {
			buf.WriteString(t.Text)
			buf.WriteByte(' ')
		}
	}
	return buf.String()
}
