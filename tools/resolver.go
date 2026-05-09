package tools

import "context"

// Resolver dynamically provides tools at runtime.
type Resolver interface {
	List(ctx context.Context) ([]Tool, error)
	Resolve(ctx context.Context, name string) (Tool, error)
}
