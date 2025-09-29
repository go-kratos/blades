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

// ctxFlowKey is an unexported type for keys defined in this package.
type ctxFlowKey struct{}

// FlowState is the state of a flow execution.
type FlowState[I any] struct {
	Input  I
	Prompt *blades.Prompt
}

// NewFlowContext returns a new Context that carries value.
func NewFlowState[I any](input I, prompt *blades.Prompt) *FlowState[I] {
	return &FlowState[I]{Input: input, Prompt: prompt}
}

// NewFlowContext returns a new Context that carries value.
func NewFlowContext[I any](ctx context.Context, state *FlowState[I]) context.Context {
	return context.WithValue(ctx, ctxFlowKey{}, state)
}

// FromFlowContext retrieves the FlowState from the context.
func FromFlowContext[I any](ctx context.Context) (*FlowState[I], bool) {
	state, ok := ctx.Value(ctxFlowKey{}).(*FlowState[I])
	return state, ok
}
