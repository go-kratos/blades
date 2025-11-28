package graph

import "context"

// Handler mutates the shared State in place and returns an error on failure.
type Handler func(ctx context.Context, state State) error

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
