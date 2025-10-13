package blades

import "context"

// RunHandler represents the core synchronous execution function.
type RunHandler func(context.Context, *Prompt, ...ModelOption) (*Message, error)

// StreamHandler represents the core streaming execution function.
type StreamHandler func(context.Context, *Prompt, ...ModelOption) (Streamable[*Message], error)

// Handler bundles both Run and Stream handlers.
type Handler struct {
	Run    RunHandler
	Stream StreamHandler
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

// Unary builds a Middleware that only wraps the Run path.
func Unary(wrap func(RunHandler) RunHandler) Middleware {
	return func(next Handler) Handler {
		return Handler{
			Run:    wrap(next.Run),
			Stream: next.Stream,
		}
	}
}

// Streaming builds a Middleware that only wraps the Stream path.
func Streaming(wrap func(StreamHandler) StreamHandler) Middleware {
	return func(next Handler) Handler {
		return Handler{
			Run:    next.Run,
			Stream: wrap(next.Stream),
		}
	}
}
