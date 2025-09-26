package flow

import (
	"context"

	"github.com/go-kratos/blades"
)

// ctxStateKey is an unexported type for keys defined in this package.
type ctxStateKey struct{}

// StateContext holds the state for a particular flow execution.
type StateContext struct {
	Current  string
	Prompt   *blades.Prompt
	History  []*blades.Message
	Metadata map[string]any
}

// NewState creates a new StateContext with the given prompt.
func NewState(prompt *blades.Prompt) *StateContext {
	return &StateContext{
		Prompt:   prompt,
		Metadata: make(map[string]any),
	}
}

// NewStateContext returns a new Context that carries value.
func NewStateContext(ctx context.Context, state *StateContext) context.Context {
	return context.WithValue(ctx, ctxStateKey{}, state)
}

// FromStateContext retrieves the StateContext from the context.
func FromStateContext(ctx context.Context) (*StateContext, bool) {
	state, ok := ctx.Value(ctxStateKey{}).(*StateContext)
	return state, ok
}
