package tools

// Middleware wraps a Handler and returns a new Handler with additional behavior.
// It is applied in a chain (outermost first) using ChainMiddlewares.
type Middleware[I, O any] func(Handler[I, O]) Handler[I, O]

// ApplyMiddlewares applies the given middlewares to the handler.
// If no middlewares are provided, it returns the original handler.
func ApplyMiddlewares[I, O any](h Handler[I, O], mws ...Middleware[I, O]) Handler[I, O] {
	if len(mws) == 0 {
		return h
	}
	return ChainMiddlewares(mws...)(h)
}

// ChainMiddlewares composes middlewares into one, applying them in order.
// The first middleware becomes the outermost wrapper.
func ChainMiddlewares[I, O any](mws ...Middleware[I, O]) Middleware[I, O] {
	return func(next Handler[I, O]) Handler[I, O] {
		h := next
		for i := len(mws) - 1; i >= 0; i-- { // apply in reverse to make mws[0] outermost
			h = mws[i](h)
		}
		return h
	}
}
