package blades

import (
	"context"

	"github.com/go-kratos/generics"
)

// ctxStateKey is an unexported type for keys defined in this package.
type ctxStateKey struct{}

// State holds the state of a flow.
type State struct {
	History  generics.Slice[*Message]
	Inputs   generics.Map[string, *Prompt]
	Outputs  generics.Map[string, *Message]
	Metadata generics.Map[string, any]
}

// NewState creates a new State instance.
func NewState() *State {
	return &State{}
}

// NewStateContext returns a new Context that carries value.
func NewStateContext(ctx context.Context, state *State) context.Context {
	return context.WithValue(ctx, ctxStateKey{}, state)
}

// FromStateContext retrieves the StateContext from the context.
func FromStateContext(ctx context.Context) (*State, bool) {
	state, ok := ctx.Value(ctxStateKey{}).(*State)
	return state, ok
}

// EnsureState retrieves the StateContext from the context, or creates a new one if it doesn't exist.
func EnsureState(ctx context.Context) (*State, context.Context) {
	state, ok := FromStateContext(ctx)
	if !ok {
		state = NewState()
		ctx = NewStateContext(ctx, state)
	}
	return state, ctx
}
