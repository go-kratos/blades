package handoff

import "context"

// Handoff represents the data needed to transfer control to another agent.
type Handoff struct {
	TargetAgent string
}

// ctxHandoffKey is an unexported type for context keys defined in this package.
type ctxHandoffKey struct{}

// NewContext returns a new context with the given Handoff attached.
func NewContext(ctx context.Context, handoff *Handoff) context.Context {
	return context.WithValue(ctx, ctxHandoffKey{}, handoff)
}

// FromContext retrieves the Handoff from the context, if it exists.
func FromContext(ctx context.Context) (*Handoff, bool) {
	handoff, ok := ctx.Value(ctxHandoffKey{}).(*Handoff)
	return handoff, ok
}
