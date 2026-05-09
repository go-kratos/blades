package memory

import "context"

// Query describes a search request against a memory store.
type Query struct {
	Text     string
	Limit    int
	Metadata map[string]any
}

// Store is the backend abstraction for memory persistence.
type Store interface {
	Put(ctx context.Context, entry *Entry) error
	Search(ctx context.Context, query Query, opts ...SearchOption) ([]*Entry, error)
	Delete(ctx context.Context, opts ...DeleteOption) error
}

// SearchOption configures a Search call.
type SearchOption func(*searchOptions)

type searchOptions struct{}

// DeleteOption configures a Delete call.
type DeleteOption func(*deleteOptions)

type deleteOptions struct {
	ids    []string
	filter map[string]any
}

// WithDeleteIDs restricts deletion to specific entry IDs.
func WithDeleteIDs(ids ...string) DeleteOption {
	return func(o *deleteOptions) {
		o.ids = append(o.ids, ids...)
	}
}

// WithDeleteFilter restricts deletion by metadata filter.
func WithDeleteFilter(filter map[string]any) DeleteOption {
	return func(o *deleteOptions) {
		o.filter = filter
	}
}
