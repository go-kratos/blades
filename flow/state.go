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

// ctxGraphKey is an unexported type for keys defined in this package.
type ctxGraphKey struct{}

// NodeState is the state of a node execution.
type NodeState[I, O any] struct {
	Input  I
	Output O
}

// NewNodeState returns a new NodeState with the given input and output.
func NewNodeState[I, O any](input I, output O) *NodeState[I, O] {
	return &NodeState[I, O]{
		Input:  input,
		Output: output,
	}
}

// GraphState is the state of a graph execution.
type GraphState struct {
	States   sync.Map // node -> state
	Metadata sync.Map
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
