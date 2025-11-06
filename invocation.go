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
func NewInvocationContext(ctx context.Context, invocation *InvocationContext) context.Context {
	return context.WithValue(ctx, ctxInvocationKey{}, invocation)
}

// FromInvocationContext retrieves the InvocationContext from the context.
func FromInvocationContext(ctx context.Context) (*InvocationContext, bool) {
	invocation, ok := ctx.Value(ctxInvocationKey{}).(*InvocationContext)
	return invocation, ok
}

// EnsureInvocationContext retrieves the InvocationContext from the context, or creates a new one if it doesn't exist.
func EnsureInvocationContext(ctx context.Context) (*InvocationContext, context.Context) {
	invocation, ok := FromInvocationContext(ctx)
	if !ok {
		invocation = &InvocationContext{Session: NewSession(), InvocationID: uuid.NewString()}
		ctx = NewInvocationContext(ctx, invocation)
	}
	return invocation, ctx
}
