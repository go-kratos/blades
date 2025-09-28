package flow

import (
	"context"
	"errors"

	"github.com/go-kratos/blades"
)

var (
	// ErrNoFlowState is returned when there is no flow state in the context.
	ErrNoFlowState = errors.New("no flow state in context")
)

// ctxStateKey is an unexported type for keys defined in this package.
type ctxStateKey struct{}

// State is the state of a flow execution.
type State struct {
	Prompt   *blades.Prompt
	History  []*blades.Message
	Metadata map[string]any
}

// NewState returns a new GraphState with the given prompt and empty history and metadata.
func NewState(prompt *blades.Prompt) *State {
	return &State{
		Prompt:   prompt,
		Metadata: make(map[string]any),
	}
}

// NewContext returns a new Context that carries value.
func NewContext(ctx context.Context, state *State) context.Context {
	return context.WithValue(ctx, ctxStateKey{}, state)
}

// FromContext retrieves the StateContext from the context.
func FromContext(ctx context.Context) (*State, bool) {
	state, ok := ctx.Value(ctxStateKey{}).(*State)
	return state, ok
}
