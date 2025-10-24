package flow

import (
	"context"
	"fmt"
)

// GraphHandler is a function that processes the graph state.
// Handlers must not mutate the incoming state; instead, they should return a new state instance.
// This is especially important for reference types (e.g., pointers, slices, maps) to avoid unintended side effects.
type GraphHandler[S any] func(ctx context.Context, state S) (S, error)

// EdgeCondition is a function that determines if an edge should be followed based on the current state.
type EdgeCondition[S any] func(ctx context.Context, state S) bool

// EdgeOption configures an edge before it is added to the graph.
type EdgeOption[S any] func(*conditionalEdge[S])

// WithEdgeCondition sets a condition that must return true for the edge to be taken.
func WithEdgeCondition[S any](condition EdgeCondition[S]) EdgeOption[S] {
	return func(edge *conditionalEdge[S]) {
		edge.condition = condition
	}
}

// conditionalEdge represents an edge with an optional condition.
type conditionalEdge[S any] struct {
	to        string
	condition EdgeCondition[S] // nil means always follow this edge
}

// Graph represents a directed graph of processing nodes. Cycles are allowed.
type Graph[S any] struct {
	nodes       map[string]GraphHandler[S]
	edges       map[string][]conditionalEdge[S]
	entryPoint  string
	finishPoint string
}

// NewGraph creates a new empty Graph.
func NewGraph[S any]() *Graph[S] {
	return &Graph[S]{
		nodes: make(map[string]GraphHandler[S]),
		edges: make(map[string][]conditionalEdge[S]),
	}
}

// AddNode adds a named node with its handler to the graph.
func (g *Graph[S]) AddNode(name string, handler GraphHandler[S]) error {
	if _, ok := g.nodes[name]; ok {
		return fmt.Errorf("graph: node %s already exists", name)
	}
	g.nodes[name] = handler
	return nil
}

// AddEdge adds a directed edge from one node to another. Options can configure the edge.
func (g *Graph[S]) AddEdge(from, to string, opts ...EdgeOption[S]) error {
	for _, edge := range g.edges[from] {
		if edge.to == to {
			return fmt.Errorf("graph: edge from %s to %s already exists", from, to)
		}
	}
	newEdge := conditionalEdge[S]{to: to}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&newEdge)
	}
	g.edges[from] = append(g.edges[from], newEdge)
	return nil
}

// SetEntryPoint marks a node as the entry point.
func (g *Graph[S]) SetEntryPoint(start string) error {
	if g.entryPoint != "" {
		return fmt.Errorf("graph: entry point already set to %s", g.entryPoint)
	}
	g.entryPoint = start
	return nil
}

// SetFinishPoint marks a node as the finish point.
func (g *Graph[S]) SetFinishPoint(end string) error {
	if g.finishPoint != "" {
		return fmt.Errorf("graph: finish point already set to %s", g.finishPoint)
	}
	g.finishPoint = end
	return nil
}

// validate ensures the graph configuration is correct before compiling.
func (g *Graph[S]) validate() error {
	if g.entryPoint == "" {
		return fmt.Errorf("graph: entry point not set")
	}
	if g.finishPoint == "" {
		return fmt.Errorf("graph: finish point not set")
	}
	if _, ok := g.nodes[g.entryPoint]; !ok {
		return fmt.Errorf("graph: start node not found: %s", g.entryPoint)
	}
	if _, ok := g.nodes[g.finishPoint]; !ok {
		return fmt.Errorf("graph: end node not found: %s", g.finishPoint)
	}
	for from, edges := range g.edges {
		if _, ok := g.nodes[from]; !ok {
			return fmt.Errorf("graph: edge from unknown node: %s", from)
		}
		for _, edge := range edges {
			if _, ok := g.nodes[edge.to]; !ok {
				return fmt.Errorf("graph: edge to unknown node: %s", edge.to)
			}
		}
	}
	return nil
}

// ensureReachable verifies that the finish node can be reached from the entry node.
func (g *Graph[S]) ensureReachable() error {
	if g.entryPoint == g.finishPoint {
		return nil
	}
	queue := []string{g.entryPoint}
	visited := make(map[string]bool, len(g.nodes))
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		if visited[node] {
			continue
		}
		visited[node] = true
		if node == g.finishPoint {
			return nil
		}
		for _, edge := range g.edges[node] {
			queue = append(queue, edge.to)
		}
	}
	return fmt.Errorf("graph: finish node not reachable: %s", g.finishPoint)
}

// Compile validates and compiles the graph into a GraphHandler.
// Execution processes unconditional edges in breadth-first order while allowing
// conditional edges to drive dynamic control flow, including loops.
func (g *Graph[S]) Compile() (GraphHandler[S], error) {
	if err := g.validate(); err != nil {
		return nil, err
	}
	if err := g.ensureReachable(); err != nil {
		return nil, err
	}

	return func(ctx context.Context, state S) (S, error) {
		type frame struct {
			node         string
			allowRevisit bool
		}

		queue := []frame{{node: g.entryPoint}}
		visited := make(map[string]bool, len(g.nodes))

		for len(queue) > 0 {
			currentFrame := queue[0]
			queue = queue[1:]
			current := currentFrame.node

			if visited[current] && !currentFrame.allowRevisit {
				continue
			}
			visited[current] = true

			handler := g.nodes[current]
			var err error
			state, err = handler(ctx, state)
			if err != nil {
				return state, fmt.Errorf("graph: node %s: %w", current, err)
			}

			if current == g.finishPoint {
				return state, nil
			}

			edges := g.edges[current]
			if len(edges) == 0 {
				return state, fmt.Errorf("graph: no outgoing edges from node %s", current)
			}

			hasConditional := false
			for _, edge := range edges {
				if edge.condition != nil {
					hasConditional = true
					break
				}
			}

			if hasConditional {
				nextFrame := frame{allowRevisit: true}
				matched := false
				for _, edge := range edges {
					if edge.condition == nil || edge.condition(ctx, state) {
						nextFrame.node = edge.to
						matched = true
						break
					}
				}
				if !matched {
					return state, fmt.Errorf("graph: no condition matched for edges from node %s", current)
				}
				queue = append([]frame{nextFrame}, queue...)
			} else {
				for _, edge := range edges {
					queue = append(queue, frame{
						node:         edge.to,
						allowRevisit: currentFrame.allowRevisit,
					})
				}
			}
		}

		return state, fmt.Errorf("graph: finish node not reachable: %s", g.finishPoint)
	}, nil
}
