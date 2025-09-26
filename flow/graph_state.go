package flow

import (
	"context"

	"github.com/go-kratos/blades"
)

// ctxStateKey is an unexported type for keys defined in this package.
type ctxStateKey struct{}

// GraphState holds the current state of the graph execution.
type GraphState struct {
	Current  string
	Prompt   *blades.Prompt
	History  []*blades.Message
	Metadata map[string]any
}

// NewGraphState returns a new GraphState with the given prompt and empty history and metadata.
func NewGraphState(prompt *blades.Prompt) *GraphState {
	return &GraphState{
		Prompt:   prompt,
		Metadata: make(map[string]any),
	}
}

// NewStateContext returns a new Context that carries value.
func NewStateContext(ctx context.Context, state *GraphState) context.Context {
	return context.WithValue(ctx, ctxStateKey{}, state)
}

// FromStateContext retrieves the StateContext from the context.
func FromStateContext(ctx context.Context) (*GraphState, bool) {
	state, ok := ctx.Value(ctxStateKey{}).(*GraphState)
	return state, ok
}
