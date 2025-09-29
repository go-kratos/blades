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

// ctxGraphKey is an unexported type for keys defined in this package.
type ctxGraphKey struct{}

// GraphState is the state of a graph execution.
type GraphState struct {
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

// NewGraphContext returns a new Context that carries value.
func NewGraphContext(ctx context.Context, state *GraphState) context.Context {
	return context.WithValue(ctx, ctxGraphKey{}, state)
}

// FromGraphContext retrieves the StateContext from the context.
func FromGraphContext(ctx context.Context) (*GraphState, bool) {
	state, ok := ctx.Value(ctxGraphKey{}).(*GraphState)
	return state, ok
}
