package memory

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/go-kratos/blades/content"
	"github.com/google/uuid"
)

const defaultRecallLimit = 10

// NewInMemory creates an in-memory Memory implementation.
func NewInMemory() Memory {
	return &inMemory{}
}

type inMemory struct {
	mu      sync.RWMutex
	entries []Entry
}

func (m *inMemory) Recall(_ context.Context, query Query) ([]Entry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	limit := query.Limit
	if limit <= 0 {
		limit = defaultRecallLimit
	}

	text := strings.TrimSpace(query.Text)
	results := make([]Entry, 0, min(limit, len(m.entries)))
	for _, entry := range m.entries {
		if text != "" && !strings.Contains(strings.ToLower(partsToText(entry.Parts)), strings.ToLower(text)) {
			continue
		}
		if !matchesFilter(entry.Metadata, query.Filter) {
			continue
		}
		results = append(results, cloneEntry(entry))
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

func (m *inMemory) Remember(_ context.Context, entry Entry) error {
	if len(entry.Parts) == 0 {
		return errors.New("memory: entry parts required")
	}

	cp := cloneEntry(entry)
	if cp.ID == "" {
		cp.ID = uuid.NewString()
	}

	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, existing := range m.entries {
		if existing.ID != cp.ID {
			continue
		}
		if cp.CreatedAt.IsZero() {
			cp.CreatedAt = existing.CreatedAt
		}
		cp.UpdatedAt = now
		m.entries[i] = cp
		return nil
	}

	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = now
	}
	cp.UpdatedAt = now
	m.entries = append(m.entries, cp)
	return nil
}

func (m *inMemory) Forget(_ context.Context, entry Entry) error {
	if strings.TrimSpace(entry.ID) == "" {
		return errors.New("memory: entry ID required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	filtered := m.entries[:0]
	for _, existing := range m.entries {
		if existing.ID == entry.ID {
			continue
		}
		filtered = append(filtered, existing)
	}
	m.entries = filtered
	return nil
}

func matchesFilter(metadata, filter map[string]any) bool {
	if len(filter) == 0 {
		return true
	}
	for key, want := range filter {
		got, ok := metadata[key]
		if !ok || !reflect.DeepEqual(got, want) {
			return false
		}
	}
	return true
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

func cloneEntry(entry Entry) Entry {
	cp := entry
	cp.Parts = cloneParts(entry.Parts)
	cp.Metadata = cloneMap(entry.Metadata)
	return cp
}

func cloneMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func cloneParts(parts []content.Part) []content.Part {
	if parts == nil {
		return nil
	}
	cp := make([]content.Part, len(parts))
	for i, part := range parts {
		cp[i] = clonePart(part)
	}
	return cp
}

func clonePart(part content.Part) content.Part {
	switch p := part.(type) {
	case content.DataPart:
		p.Bytes = append([]byte(nil), p.Bytes...)
		return p
	case content.Thinking:
		p.Signature = append([]byte(nil), p.Signature...)
		return p
	case content.ToolUse:
		p.Input = append([]byte(nil), p.Input...)
		return p
	case content.ToolResult:
		p.Parts = cloneParts(p.Parts)
		return p
	default:
		return part
	}
}
