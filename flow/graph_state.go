package flow

import (
	"context"
	"errors"
	"sync"
)

var (
	// ErrNoFlowState is returned when there is no flow state in the context.
	ErrNoFlowState = errors.New("no flow state in context")
	// ErrNoGraphState is returned when there is no graph state in the context.
	ErrNoGraphState = errors.New("no graph state in context")
)

// GraphStateHandler is a function that handles the graph state.
type GraphStateHandler[I, O any] func(ctx context.Context, output O) (I, error)

// ctxGraphKey is an unexported type for keys defined in this package.
type ctxGraphKey struct{}

// GraphState is the state of a graph execution.
type GraphState struct {
	Inputs   sync.Map // node -> input
	Outputs  sync.Map // node -> output
	Metadata sync.Map // key -> value
}

// NewGraphState returns a new GraphState with the given prompt and empty history and metadata.
func NewGraphState() *GraphState {
	return &GraphState{}
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
