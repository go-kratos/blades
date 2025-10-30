package graph

import "context"

// Handler is a function that processes the graph state.
// Handlers must not mutate the incoming state; instead, they should return a new state instance.
// This is especially important for reference types (e.g., pointers, slices, maps) to avoid unintended side effects.
type Handler func(ctx context.Context, state State) (State, error)

// Middleware is a function that wraps a Handler with additional functionality.
type Middleware func(Handler) Handler

// ChainMiddlewares composes middlewares into one, applying them in order.
// The first middleware becomes the outermost wrapper.
func ChainMiddlewares(mws ...Middleware) Middleware {
	return func(next Handler) Handler {
		h := next
		for i := len(mws) - 1; i >= 0; i-- { // apply in reverse to make mws[0] outermost
			h = mws[i](h)
		}
		return h
	}
}

// contextKey is a private type for context keys to avoid collisions.
type contextKey string

// NodeNameKey is the context key for retrieving the current node name.
const NodeNameKey contextKey = "node_name"

// GetNodeName retrieves the node name from the context.
// Returns the node name and true if found, or empty string and false if not found.
func GetNodeName(ctx context.Context) (string, bool) {
	name, ok := ctx.Value(NodeNameKey).(string)
	return name, ok
}
