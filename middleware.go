package blades

import "context"

// Middleware wraps a Handler and returns a new Handler with additional behavior.
// It is applied in a chain (outermost first) using ChainMiddlewares.
type Middleware func(Runnable) Runnable

// ChainMiddlewares composes middlewares into one, applying them in order.
// The first middleware becomes the outermost wrapper.
func ChainMiddlewares(mws ...Middleware) Middleware {
	return func(next Runnable) Runnable {
		h := next
		for i := len(mws) - 1; i >= 0; i-- { // apply in reverse to make mws[0] outermost
			h = mws[i](h)
		}
		return h
	}
}

// HandleFunc is a helper to easily create Runner instances from functions.
// It is especially useful for testing, lightweight adapters, or wrapping logic with middleware.
type HandleFunc struct {
	Handle       func(context.Context, *Prompt, ...ModelOption) (*Message, error)
	HandleStream func(context.Context, *Prompt, ...ModelOption) (Streamable[*Message], error)
}

// Name returns the name of the runner.
func (f *HandleFunc) Name() string {
	return "middleware"
}

// Run executes the runner with the given context, prompt, and options.
func (f *HandleFunc) Run(ctx context.Context, p *Prompt, opts ...ModelOption) (*Message, error) {
	return f.Handle(ctx, p, opts...)
}

// RunStream executes the runner in streaming mode with the given context, prompt, and options.
func (f *HandleFunc) RunStream(ctx context.Context, p *Prompt, opts ...ModelOption) (Streamable[*Message], error) {
	return f.HandleStream(ctx, p, opts...)
}
