package flow

import (
	"context"

	"github.com/go-kratos/blades"
)

// Flowable is an interface that marks a node as part of a flow.
type Flowable interface {
	blades.Runner
	isFlowable()
}

// Flow represents a directed acyclic graph (DAG) of nodes for processing prompts.
type Flow struct {
	head Flowable
}

// New creates a new Flow with the given head node.
func New(head Flowable) *Flow {
	return &Flow{head: head}
}

// Run processes the given prompt through the graph and returns the final generation result.
func (g *Flow) Run(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (*blades.Generation, error) {
	state := NewState(prompt)
	ctx = NewContext(ctx, state)
	return g.head.Run(ctx, prompt, opts...)
}

// RunStream processes the given prompt through the graph and returns a streamer for the generation result.
func (g *Flow) RunStream(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (blades.Streamer[*blades.Generation], error) {
	state := NewState(prompt)
	ctx = NewContext(ctx, state)
	return g.head.RunStream(ctx, prompt, opts...)
}
