package flow

import (
	"context"

	"github.com/go-kratos/blades"
)

// NodeRunner is an interface that extends blades.Runner and marks a node in the graph.
type NodeRunner interface {
	blades.Runner
	isNode()
}

// Graph represents a directed acyclic graph (DAG) of nodes for processing prompts.
type Graph struct {
	head NodeRunner
}

// NewGraph creates a new Graph with the given head node.
func NewGraph(head NodeRunner) *Graph {
	return &Graph{head: head}
}

// Run processes the given prompt through the graph and returns the final generation result.
func (g *Graph) Run(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (*blades.Generation, error) {
	state := NewGraphState(prompt)
	ctx = NewGraphContext(ctx, state)
	return g.head.Run(ctx, prompt, opts...)
}

// RunStream processes the given prompt through the graph and returns a streamer for the generation result.
func (g *Graph) RunStream(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (blades.Streamer[*blades.Generation], error) {
	state := NewGraphState(prompt)
	ctx = NewGraphContext(ctx, state)
	return g.head.RunStream(ctx, prompt, opts...)
}
