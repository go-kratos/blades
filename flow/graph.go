package flow

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades"
)

// graphNode represents a node in the graph.
type graphNode struct {
	name  string
	edges []*graphEdge
}

// graphEdge represents a directed edge between two nodes in the graph.
type graphEdge struct {
	name string
}

// Graph is a lightweight directed acyclic execution graph that runs nodes in BFS order
// starting from declared start nodes and stopping at terminal nodes. Edges optionally
// transform a node's output into the next node's input.
//
// All nodes share the same input/output/option types to keep the API simple and predictable.
type Graph[I, O, Option any] struct {
	name       string
	runners    map[string]blades.Runnable[I, O, Option]
	nodes      map[string]*graphNode
	starts     map[string]struct{}
	transition TransitionHandler[I, O]
}

// NewGraph creates an empty graph.
func NewGraph[I, O, Option any](name string, transition TransitionHandler[I, O]) *Graph[I, O, Option] {
	return &Graph[I, O, Option]{
		name:       name,
		transition: transition,
		runners:    make(map[string]blades.Runnable[I, O, Option]),
		nodes:      make(map[string]*graphNode),
		starts:     make(map[string]struct{}),
	}
}

// AddNode registers a named runner node.
func (g *Graph[I, O, Option]) AddNode(runner blades.Runnable[I, O, Option]) error {
	name := runner.Name()
	if _, ok := g.nodes[name]; ok {
		return fmt.Errorf("graph: node %s already exists", runner.Name())
	}
	g.runners[name] = runner
	g.nodes[name] = &graphNode{name: name}
	return nil
}

// AddEdge connects two named nodes. Optionally supply a transformer that maps
// the upstream node's output (O) into the downstream node's input (I).
func (g *Graph[I, O, Option]) AddEdge(from, to blades.Runnable[I, O, Option]) error {
	node := g.nodes[from.Name()]
	node.edges = append(node.edges, &graphEdge{name: to.Name()})
	return nil
}

// AddStart marks a node as a start entry.
func (g *Graph[I, O, Option]) AddStart(start blades.Runnable[I, O, Option]) error {
	if _, ok := g.starts[start.Name()]; ok {
		return fmt.Errorf("graph: start node %s already exists", start.Name())
	}
	g.starts[start.Name()] = struct{}{}
	return nil
}

// Compile returns a blades.Runner that executes the graph.
func (g *Graph[I, O, Option]) Compile() (blades.Runnable[I, O, Option], error) {
	// Validate starts and ends exist
	if len(g.starts) == 0 {
		return nil, fmt.Errorf("graph: no start nodes defined")
	}
	for start := range g.starts {
		if _, ok := g.nodes[start]; !ok {
			return nil, fmt.Errorf("graph: edge references unknown node %s", start)
		}
	}
	// BFS discover reachable nodes from starts
	compiled := make(map[string][]*graphNode, len(g.nodes))
	for start := range g.starts {
		node := g.nodes[start]
		visited := make(map[string]int, len(g.nodes))
		queue := make([]*graphNode, 0, len(g.nodes))
		queue = append(queue, node)
		for len(queue) > 0 {
			next := queue[0]
			queue = queue[1:]
			visited[next.name]++
			for _, to := range next.edges {
				queue = append(queue, g.nodes[to.name])
			}
			if visited[next.name] > 1 {
				return nil, fmt.Errorf("graph: cycle detected at node %s", next.name)
			}
			compiled[start] = append(compiled[start], next)
		}
	}
	return &graphRunner[I, O, Option]{graph: g, compiled: compiled}, nil
}

// graphRunner executes a compiled Graph.
type graphRunner[I, O, Option any] struct {
	graph    *Graph[I, O, Option]
	compiled map[string][]*graphNode
}

func (r *graphRunner[I, O, Option]) Name() string {
	return r.graph.name
}

// Run executes the graph to completion and returns the final node's generation.
func (r *graphRunner[I, O, Option]) Run(ctx context.Context, input I, opts ...Option) (O, error) {
	var (
		err    error
		output O
		last   blades.Runnable[I, O, Option]
	)
	for _, queue := range r.compiled {
		handle := false
		for len(queue) > 0 {
			next := queue[0]
			queue = queue[1:]
			if handle {
				if input, err = r.graph.transition(ctx, Transition{From: last.Name(), To: next.name}, output); err != nil {
					return output, err
				}
			}
			handle = true
			runner := r.graph.runners[next.name]
			if output, err = runner.Run(ctx, input, opts...); err != nil {
				return output, err
			}
			last = runner
		}
	}
	return output, nil
}

// RunStream executes the graph and streams each node's output sequentially.
func (r *graphRunner[I, O, Option]) RunStream(ctx context.Context, input I, opts ...Option) (blades.Streamable[O], error) {
	pipe := blades.NewStreamPipe[O]()
	pipe.Go(func() error {
		output, err := r.Run(ctx, input, opts...)
		if err != nil {
			return err
		}
		pipe.Send(output)
		return nil
	})
	return pipe, nil
}
