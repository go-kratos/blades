package blades

import (
	"context"

	"github.com/google/uuid"
)

// InvocationContext holds information about the current invocation.
type InvocationContext struct {
	Session      Session
	Resumable    bool
	InvocationID string
}

// ctxInvocationKey is an unexported type for keys defined in this package.
type ctxInvocationKey struct{}

// NewInvocationContext returns a new Context that carries value.
func NewInvocationContext(ctx context.Context, value *InvocationContext) context.Context {
	return context.WithValue(ctx, ctxInvocationKey{}, value)
}

// FromInvocationContext retrieves the SessionContext from the context.
func FromInvocationContext(ctx context.Context) (*InvocationContext, bool) {
	value, ok := ctx.Value(ctxInvocationKey{}).(*InvocationContext)
	return value, ok
}

// EnsureInvocation retrieves the InvocationContext from the context, or creates a new one if it doesn't exist.
func EnsureInvocation(ctx context.Context) (*InvocationContext, context.Context) {
	invocation, ok := FromInvocationContext(ctx)
	if !ok {
		ctx = NewInvocationContext(ctx, &InvocationContext{
			Session: NewSession(uuid.NewString()),
		})
	}
	return invocation, ctx
}
