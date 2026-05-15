package memory

import (
	"context"
	"time"

	"github.com/go-kratos/blades/content"
)

// Memory is the interface for long-term memory operations.
type Memory interface {
	Recall(ctx context.Context, query Query) ([]Entry, error)
	Remember(ctx context.Context, entry Entry) error
	Forget(ctx context.Context, entry Entry) error
}

// Entry is a single memory record.
type Entry struct {
	ID        string
	Parts     []content.Part
	Metadata  map[string]any
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Query describes a recall request.
type Query struct {
	Text   string
	Limit  int
	Filter map[string]any
}
