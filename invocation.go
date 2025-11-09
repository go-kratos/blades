package blades

import (
	"context"

	"github.com/google/uuid"
)

// InvocationContext holds information about the current invocation.
type InvocationContext interface {
	Session() Session
	Resumable() bool
	InvocationID() string
}

// ctxInvocationKey is an unexported type for keys defined in this package.
type ctxInvocationKey struct{}

// NewInvocationID generates a new unique invocation ID.
func NewInvocationID() string {
	return uuid.NewString()
}

// NewInvocationContext returns a new Context that carries value.
func NewInvocationContext(ctx context.Context, invocation InvocationContext) context.Context {
	return context.WithValue(ctx, ctxInvocationKey{}, invocation)
}

// FromInvocationContext retrieves the InvocationContext from the context.
func FromInvocationContext(ctx context.Context) (InvocationContext, bool) {
	invocation, ok := ctx.Value(ctxInvocationKey{}).(InvocationContext)
	return invocation, ok
}
