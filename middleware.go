package blades

import (
	"context"
)

// Handler defines a function that processes an Invocation and returns a Sequence of Messages.
type Handler interface {
	Handle(context.Context, *Invocation) Sequence[*Message]
}

// HandleFunc is an adapter to allow the use of ordinary functions as Handlers.
type HandleFunc func(context.Context, *Invocation) Sequence[*Message]

// Handler calls f(ctx, invocation).
func (f HandleFunc) Handle(ctx context.Context, invocation *Invocation) Sequence[*Message] {
	return f(ctx, invocation)
}

// Middleware wraps a Handler and returns a new Handler with additional behavior.
// It is applied in a chain (outermost first) using ChainMiddlewares.
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
