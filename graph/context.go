package graph

import "context"

type ctxNodeKey struct{}
type ctxNodeNameKey struct{}

// NodeContext holds information about the current node in the graph.
type NodeContext struct {
	Name  string
	State State
}

// NewNodeContext returns a new context with the given NodeContext.
func NewNodeContext(ctx context.Context, node *NodeContext) context.Context {
	ctx = context.WithValue(ctx, ctxNodeKey{}, node)
	if node != nil {
		ctx = context.WithValue(ctx, ctxNodeNameKey{}, node.Name)
	}
	return ctx
}

// FromNodeContext retrieves the NodeContext from the context, if present.
func FromNodeContext(ctx context.Context) (*NodeContext, bool) {
	agent, ok := ctx.Value(ctxNodeKey{}).(*NodeContext)
	return agent, ok
}

// NodeNameFromContext returns the current node name if available.
func NodeNameFromContext(ctx context.Context) (string, bool) {
	name, ok := ctx.Value(ctxNodeNameKey{}).(string)
	return name, ok
}
