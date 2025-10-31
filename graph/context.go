package graph

import "context"

type ctxNodeKey struct{}

// NodeContext holds information about the current node in the graph.
type NodeContext struct {
	Name string
}

// NewNodeContext returns a new context with the given NodeContext.
func NewNodeContext(ctx context.Context, node *NodeContext) context.Context {
	return context.WithValue(ctx, ctxNodeKey{}, node)
}

// FromNodeContext retrieves the NodeContext from the context, if present.
func FromNodeContext(ctx context.Context) (*NodeContext, bool) {
	node, ok := ctx.Value(ctxNodeKey{}).(*NodeContext)
	return node, ok
}
