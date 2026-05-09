package memory

import (
	"context"
	"time"

	"github.com/go-kratos/blades/content"
)

// Memory is the interface for long-term memory operations.
type Memory interface {
	Recall(ctx context.Context, query string, opts ...RecallOption) ([]content.Part, error)
	Remember(ctx context.Context, parts []content.Part, opts ...RememberOption) error
	Forget(ctx context.Context, opts ...ForgetOption) error
}

// Entry is a single memory record.
type Entry struct {
	ID        string
	Parts     []content.Part
	Metadata  map[string]any
	CreatedAt time.Time
	UpdatedAt time.Time
}
