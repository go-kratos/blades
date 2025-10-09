package flow

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades"
)

// GraphStateHandler is a function that handles the graph state.
type GraphStateHandler[I, O any] func(ctx context.Context, output O) (I, error)

// graphEdge represents a directed edge between two nodes in the graph.
type graphEdge[I, O any] struct {
	name         string
	stateHandler GraphStateHandler[I, O]
}

// Graph is a lightweight directed acyclic execution graph that runs nodes in BFS order
// starting from declared start nodes and stopping at terminal nodes. Edges optionally
// transform a node's output into the next node's input.
//
// All nodes share the same input/output/option types to keep the API simple and predictable.
type Graph[I, O, Option any] struct {
	name   string
	nodes  map[string]blades.Runner[I, O, Option]
	edges  map[string][]*graphEdge[I, O]
	starts []string
	ends   []string
}

// NewGraph creates an empty graph.
func NewGraph[I, O, Option any](name string) *Graph[I, O, Option] {
	return &Graph[I, O, Option]{
		name:  name,
		nodes: make(map[string]blades.Runner[I, O, Option]),
		edges: make(map[string][]*graphEdge[I, O]),
	}
}

// AddNode registers a named runner node.
func (g *Graph[I, O, Option]) AddNode(runner blades.Runner[I, O, Option]) {
	g.nodes[runner.Name()] = runner
}

// AddEdge connects two named nodes. Optionally supply a transformer that maps
// the upstream node's output (O) into the downstream node's input (I).
func (g *Graph[I, O, Option]) AddEdge(from, to blades.Runner[I, O, Option], stateHandler GraphStateHandler[I, O]) {
	g.edges[from.Name()] = append(g.edges[from.Name()], &graphEdge[I, O]{
		name:         to.Name(),
		stateHandler: stateHandler,
	})
}

// AddStart marks a node as a start entry.
func (g *Graph[I, O, Option]) AddStart(start blades.Runner[I, O, Option]) {
	g.starts = append(g.starts, start.Name())
}

// AddEnd marks a node as a terminal.
func (g *Graph[I, O, Option]) AddEnd(end blades.Runner[I, O, Option]) {
	g.ends = append(g.ends, end.Name())
}

// Compile returns a blades.Runner that executes the graph.
func (g *Graph[I, O, Option]) Compile() (blades.Runner[I, O, Option], error) {
	// Basic validation for missing nodes referenced by edges.
	for from, to := range g.edges {
		if _, ok := g.nodes[from]; !ok {
			return nil, fmt.Errorf("graph: edge references unknown node %q", from)
		}
		for _, e := range to {
			if _, ok := g.nodes[e.name]; !ok {
				return nil, fmt.Errorf("graph: edge %q -> %q references unknown node", from, e)
			}
		}
	}
	return &graphRunner[I, O, Option]{graph: g}, nil
}

// graphRunner executes a compiled Graph.
type graphRunner[I, O, Option any] struct {
	graph *Graph[I, O, Option]
}

func (gr *graphRunner[I, O, Option]) Name() string {
	return gr.graph.name
}

// Run executes the graph to completion and returns the final node's generation.
func (gr *graphRunner[I, O, Option]) Run(ctx context.Context, input I, opts ...Option) (O, error) {
	var (
		err    error
		output O
	)
	state := NewGraphState()
	ctx = NewGraphContext(ctx, state)
	for _, start := range gr.graph.starts {
		node := gr.graph.nodes[start]
		output, err = node.Run(ctx, input, opts...)
		if err != nil {
			return output, err
		}
		for _, to := range gr.graph.edges[start] {
			node := gr.graph.nodes[to.name]
			input, err := to.stateHandler(ctx, output)
			if err != nil {
				return output, err
			}
			output, err = node.Run(ctx, input, opts...)
			if err != nil {
				return output, err
			}
			state.Inputs.Store(to.name, input)
			state.Outputs.Store(to.name, output)
		}
	}
	return output, nil
}

// RunStream executes the graph and streams each node's output sequentially.
func (gr *graphRunner[I, O, Option]) RunStream(ctx context.Context, input I, opts ...Option) (blades.Streamer[O], error) {
	state := NewGraphState()
	ctx = NewGraphContext(ctx, state)
	pipe := blades.NewStreamPipe[O]()
	pipe.Go(func() error {
		output, err := gr.Run(ctx, input, opts...)
		if err != nil {
			return err
		}
		pipe.Send(output)
		return nil
	})
	return pipe, nil
}
