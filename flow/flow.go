package flow

import (
	"context"
	"errors"

	"github.com/go-kratos/blades"
)

var (
	// ErrNoFlowResult is returned when a flow does not produce a result.
	ErrNoFlowResult = errors.New("no flow result")
)

// Node is an interface that represents a processing unit in the flow graph.
type Node interface {
	blades.Runner
	isNode()
}

// Flowable is an interface for nodes that can connect to other nodes.
type Flowable interface {
	To(Node)
}

// Flow represents a directed acyclic graph (DAG) of nodes for processing prompts.
type Flow struct {
	head Node
}

// New creates a new Flow with the given head node.
func New(head Node) *Flow {
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
