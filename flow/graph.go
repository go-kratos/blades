package flow

import (
	"context"
	"fmt"
)

// GraphHandler is a function that processes the graph state.
type GraphHandler[S any] func(ctx context.Context, state S) (S, error)

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
type Graph[S any] struct {
	handlers    map[string]GraphHandler[S]
	edges       map[string][]string
	entryPoint  string
	finishPoint string
}

// NewGraph creates an empty graph.
func NewGraph[S any]() *Graph[S] {
	return &Graph[S]{
		handlers: make(map[string]GraphHandler[S]),
		edges:    make(map[string][]string),
	}
}

// AddNode registers a named runner node.
func (g *Graph[S]) AddNode(name string, handler GraphHandler[S]) error {
	if _, ok := g.handlers[name]; ok {
		return fmt.Errorf("graph: node %s already exists", name)
	}
	g.handlers[name] = handler
	return nil
}

// AddEdge connects two named nodes. Optionally supply a transformer that maps
// the upstream node's output (O) into the downstream node's input (I).
func (g *Graph[S]) AddEdge(from, to string) error {
	for _, name := range g.edges[from] {
		if name == to {
			return fmt.Errorf("graph: edge from %s to %s already exists", from, to)
		}
	}
	g.edges[from] = append(g.edges[from], to)
	return nil
}

// SetEntryPoint marks a node as the entry point.
func (g *Graph[S]) SetEntryPoint(start string) error {
	if g.entryPoint != "" {
		return fmt.Errorf("graph: start node %s already exists", start)
	}
	g.entryPoint = start
	return nil
}

// SetFinishPoint marks a node as the finish point.
func (g *Graph[S]) SetFinishPoint(end string) error {
	if g.finishPoint != "" {
		return fmt.Errorf("graph: end node %s already exists", end)
	}
	g.finishPoint = end
	return nil
}

// checkAcyclic verifies the reachable portion of the graph has no cycles using Kahn's algorithm.
func (g *Graph[S]) checkAcyclic() error {
	// discover reachable nodes from entry
	reachable := make(map[string]bool, len(g.handlers))
	if g.entryPoint != "" {
		queue := []string{g.entryPoint}
		for len(queue) > 0 {
			node := queue[0]
			queue = queue[1:]
			if reachable[node] {
				continue
			}
			reachable[node] = true
			for _, to := range g.edges[node] {
				if !reachable[to] {
					queue = append(queue, to)
				}
			}
		}
	}
	if len(reachable) == 0 {
		return nil
	}
	// compute indegree within reachable subgraph
	indegree := make(map[string]int, len(reachable))
	for n := range reachable {
		indegree[n] = 0
	}
	for from, tos := range g.edges {
		if !reachable[from] {
			continue
		}
		for _, to := range tos {
			if reachable[to] {
				indegree[to]++
			}
		}
	}
	// Kahn's topological sort
	queue := make([]string, 0, len(reachable))
	for n := range reachable {
		if indegree[n] == 0 {
			queue = append(queue, n)
		}
	}
	processed := 0
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		processed++
		for _, to := range g.edges[node] {
			if !reachable[to] {
				continue
			}
			indegree[to]--
			if indegree[to] == 0 {
				queue = append(queue, to)
			}
		}
	}
	if processed != len(indegree) {
		return fmt.Errorf("graph: cycle detected")
	}
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
	if _, ok := g.handlers[g.entryPoint]; !ok {
		return fmt.Errorf("graph: start node not found: %s", g.entryPoint)
	}
	if _, ok := g.handlers[g.finishPoint]; !ok {
		return fmt.Errorf("graph: end node not found: %s", g.finishPoint)
	}
	for from, tos := range g.edges {
		if _, ok := g.handlers[from]; !ok {
			return fmt.Errorf("graph: edge from unknown node: %s", from)
		}
		for _, to := range tos {
			if _, ok := g.handlers[to]; !ok {
				return fmt.Errorf("graph: edge to unknown node: %s", to)
			}
		}
	}
	if err := g.checkAcyclic(); err != nil {
		return err
	}
	return nil
}

// buildExecutionPlan computes the BFS order from entry to finish.
func (g *Graph[S]) buildExecutionPlan() ([]string, error) {
	queue := []string{g.entryPoint}
	visited := make(map[string]bool, len(g.handlers))
	order := make([]string, 0, len(g.handlers))

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		if visited[node] {
			continue
		}
		visited[node] = true
		order = append(order, node)
		if node == g.finishPoint {
			return order, nil
		}
		for _, next := range g.edges[node] {
			if !visited[next] {
				queue = append(queue, next)
			}
		}
	}
	return order, fmt.Errorf("graph: finish node not reachable: %s", g.finishPoint)
}

func (g *Graph[S]) Compile() (GraphHandler[S], error) {
	if err := g.validate(); err != nil {
		return nil, err
	}
	plan, err := g.buildExecutionPlan()
	if err != nil {
		return nil, fmt.Errorf("graph: compile: %w", err)
	}
	return func(ctx context.Context, state S) (S, error) {
		for _, node := range plan {
			var err error
			handler := g.handlers[node]
			state, err = handler(ctx, state)
			if err != nil {
				return state, fmt.Errorf("graph: node %s: %w", node, err)
			}
			if node == g.finishPoint {
				break
			}
		}
		return state, nil
	}, nil
}
