package flow

import (
	"context"
	"fmt"
)

// GraphState represents the shared state passed between graph nodes.
type GraphState map[string]any

// GraphHandler is a function that processes the graph state.
type GraphHandler func(ctx context.Context, state GraphState) (GraphState, error)

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
type Graph struct {
	name     string
	handlers map[string]GraphHandler
	nodes    map[string]*graphNode
	starts   map[string]struct{}
	ends     map[string]struct{}
}

// NewGraph creates an empty graph.
func NewGraph(name string) *Graph {
	return &Graph{
		name:     name,
		handlers: make(map[string]GraphHandler),
		nodes:    make(map[string]*graphNode),
		starts:   make(map[string]struct{}),
		ends:     make(map[string]struct{}),
	}
}

// AddNode registers a named runner node.
func (g *Graph) AddNode(name string, handler GraphHandler) error {
	if _, ok := g.handlers[name]; ok {
		return fmt.Errorf("graph: node %s already exists", name)
	}
	g.handlers[name] = handler
	g.nodes[name] = &graphNode{name: name}
	return nil
}

// AddEdge connects two named nodes. Optionally supply a transformer that maps
// the upstream node's output (O) into the downstream node's input (I).
func (g *Graph) AddEdge(from, to string) error {
	node, ok := g.nodes[from]
	if !ok {
		return fmt.Errorf("graph: edge references unknown node %s", from)
	}
	node.edges = append(node.edges, &graphEdge{name: to})
	return nil
}

// AddStart marks a node as a start entry.
func (g *Graph) AddStart(start string) error {
	if _, ok := g.starts[start]; ok {
		return fmt.Errorf("graph: start node %s already exists", start)
	}
	g.starts[start] = struct{}{}
	return nil
}

// AddEnd marks a node as an end terminal.
func (g *Graph) AddEnd(end string) error {
	if _, ok := g.ends[end]; ok {
		return fmt.Errorf("graph: end node %s already exists", end)
	}
	g.ends[end] = struct{}{}
	return nil
}

// Compile returns a blades.Runner that executes the graph.
func (g *Graph) Compile() (GraphHandler, error) {
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
	return func(ctx context.Context, state GraphState) (GraphState, error) {
		var err error
		for _, queue := range compiled {
			for len(queue) > 0 {
				next := queue[0]
				queue = queue[1:]
				handler := g.handlers[next.name]
				if state, err = handler(ctx, state); err != nil {
					return nil, err
				}
			}
		}
		return state, nil
	}, nil
}
