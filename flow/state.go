package flow

import (
	"context"
	"sync"
)

// StateHandler is a function that handles the state of a flow.
type StateHandler[I, O any] func(ctx context.Context, current string, output O) (I, error)

// ctxStqateKey is an unexported type for keys defined in this package.
type ctxStateKey struct{}

// State holds the state of a flow.
type State[I, O any] struct {
	Inputs   sync.Map
	Outputs  sync.Map
	Metadata sync.Map
}

// NewState creates a new State instance.
func NewState[I, O any]() *State[I, O] {
	return &State[I, O]{}
}

// NewStateContext returns a new Context that carries value.
func NewStateContext[I, O any](ctx context.Context, state *State[I, O]) context.Context {
	return context.WithValue(ctx, ctxStateKey{}, state)
}

// FromStateContext retrieves the StateContext from the context.
func FromStateContext[I, O any](ctx context.Context) (*State[I, O], bool) {
	state, ok := ctx.Value(ctxStateKey{}).(*State[I, O])
	return state, ok
}

// EnsureState retrieves the StateContext from the context, or creates a new one if it doesn't exist.
func EnsureState[I, O any](ctx context.Context) (*State[I, O], context.Context) {
	state, ok := FromStateContext[I, O](ctx)
	if !ok {
		state = NewState[I, O]()
		ctx = NewStateContext(ctx, state)
	}
	return state, ctx
}
