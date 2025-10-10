package flow

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades"
)

// graphEdge represents a directed edge between two nodes in the graph.
type graphEdge[I, O any] struct {
	name         string
	stateHandler StateHandler[I, O]
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
	starts map[string]struct{}
	ends   map[string]struct{}
}

// NewGraph creates an empty graph.
func NewGraph[I, O, Option any](name string) *Graph[I, O, Option] {
	return &Graph[I, O, Option]{
		name:   name,
		nodes:  make(map[string]blades.Runner[I, O, Option]),
		edges:  make(map[string][]*graphEdge[I, O]),
		starts: make(map[string]struct{}),
		ends:   make(map[string]struct{}),
	}
}

// AddNode registers a named runner node.
func (g *Graph[I, O, Option]) AddNode(runner blades.Runner[I, O, Option]) error {
	if _, ok := g.nodes[runner.Name()]; ok {
		return fmt.Errorf("graph: node %s already exists", runner.Name())
	}
	g.nodes[runner.Name()] = runner
	return nil
}

// AddEdge connects two named nodes. Optionally supply a transformer that maps
// the upstream node's output (O) into the downstream node's input (I).
func (g *Graph[I, O, Option]) AddEdge(from, to blades.Runner[I, O, Option], stateHandler StateHandler[I, O]) error {
	if _, ok := g.edges[from.Name()]; ok {
		return fmt.Errorf("graph: edge from %s already exists", from)
	}
	g.edges[from.Name()] = append(g.edges[from.Name()], &graphEdge[I, O]{
		name:         to.Name(),
		stateHandler: stateHandler,
	})
	return nil
}

// AddStart marks a node as a start entry.
func (g *Graph[I, O, Option]) AddStart(start blades.Runner[I, O, Option]) error {
	if _, ok := g.starts[start.Name()]; ok {
		return fmt.Errorf("graph: start node %s already exists", start)
	}
	g.starts[start.Name()] = struct{}{}
	return nil
}

// AddEnd marks a node as a terminal.
func (g *Graph[I, O, Option]) AddEnd(end blades.Runner[I, O, Option]) error {
	if _, ok := g.ends[end.Name()]; ok {
		return fmt.Errorf("graph: end node %s already exists", end)
	}
	g.ends[end.Name()] = struct{}{}
	return nil
}

// Compile returns a blades.Runner that executes the graph.
func (g *Graph[I, O, Option]) Compile() (blades.Runner[I, O, Option], error) {
	// Validate starts and ends exist
	if len(g.starts) == 0 {
		return nil, fmt.Errorf("graph: no start nodes defined")
	}
	if len(g.ends) == 0 {
		return nil, fmt.Errorf("graph: no end nodes defined")
	}
	for start := range g.starts {
		if _, ok := g.nodes[start]; !ok {
			return nil, fmt.Errorf("graph: edge references unknown node %s", start)
		}
	}
	for end := range g.ends {
		if _, ok := g.nodes[end]; !ok {
			return nil, fmt.Errorf("graph: edge references unknown node %s", end)
		}
		if _, ok := g.edges[end]; ok {
			return nil, fmt.Errorf("graph: end node %s has outgoing edges", end)
		}
	}
	// Basic validation for missing nodes referenced by edges.
	for from, to := range g.edges {
		if _, ok := g.nodes[from]; !ok {
			return nil, fmt.Errorf("graph: edge references unknown node %s", from)
		}
		for _, e := range to {
			if _, ok := g.nodes[e.name]; !ok {
				return nil, fmt.Errorf("graph: edge %s -> %s references unknown node", from, e.name)
			}
		}
	}
	// BFS discover reachable nodes from starts
	compiled := make(map[string][]*graphEdge[I, O], len(g.nodes))
	for start := range g.starts {
		visited := make(map[string]int, len(g.nodes))
		queue := make([]*graphEdge[I, O], 0, len(g.nodes))
		queue = append(queue, &graphEdge[I, O]{name: start})
		var next *graphEdge[I, O]
		for len(queue) > 0 {
			next = queue[0]
			queue = queue[1:]
			visited[next.name]++
			for _, to := range g.edges[next.name] {
				queue = append(queue, to)
			}
			if visited[next.name] > 1 {
				return nil, fmt.Errorf("graph: cycle detected at node %s", next.name)
			}
			compiled[start] = append(compiled[start], next)
		}
		if _, ok := g.ends[next.name]; !ok {
			return nil, fmt.Errorf("graph: graph is not fully connected, node %s is unreachable", next.name)
		}
	}
	return &graphRunner[I, O, Option]{graph: g, compiled: compiled}, nil
}

// graphRunner executes a compiled Graph.
type graphRunner[I, O, Option any] struct {
	graph    *Graph[I, O, Option]
	compiled map[string][]*graphEdge[I, O]
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
	for _, queue := range gr.compiled {
		for len(queue) > 0 {
			next := queue[0]
			queue = queue[1:]
			node := gr.graph.nodes[next.name]
			if next.stateHandler != nil {
				if input, err = next.stateHandler(ctx, output); err != nil {
					return output, err
				}
			}
			output, err = node.Run(ctx, input, opts...)
			if err != nil {
				return output, err
			}
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
