package graph

import (
	"context"
	"fmt"
	"strings"
)

// Option configures the Graph behavior.
type Option func(*Graph)

// WithParallel toggles parallel fan-out execution. Defaults to true.
func WithParallel(enabled bool) Option {
	return func(g *Graph) {
		g.parallel = enabled
	}
}

// WithMiddleware sets a global middleware applied to all node handlers.
func WithMiddleware(ms ...Middleware) Option {
	return func(g *Graph) {
		g.middlewares = ms
	}
}

// EdgeCondition is a function that determines if an edge should be followed based on the current state.
type EdgeCondition func(ctx context.Context, state State) bool

// EdgeOption configures an edge before it is added to the graph.
type EdgeOption func(*conditionalEdge)

// WithEdgeCondition sets a condition that must return true for the edge to be taken.
func WithEdgeCondition(condition EdgeCondition) EdgeOption {
	return func(edge *conditionalEdge) {
		edge.condition = condition
	}
}

// WithEdgeGroup assigns the edge to a logical activation group for the target node.
// Groups default to the target node name when unset.
func WithEdgeGroup(group string) EdgeOption {
	return func(edge *conditionalEdge) {
		edge.group = group
	}
}

// conditionalEdge represents an edge with an optional condition.
type conditionalEdge struct {
	to        string
	condition EdgeCondition // nil means always follow this edge
	group     string
}

// Graph represents a directed graph of processing nodes. Cycles are allowed.
type Graph struct {
	nodes       map[string]Handler
	edges       map[string][]conditionalEdge
	entryPoint  string
	finishPoint string
	parallel    bool
	middlewares []Middleware
}

// NewGraph creates a new empty Graph.
func NewGraph(opts ...Option) *Graph {
	g := &Graph{
		nodes:    make(map[string]Handler),
		edges:    make(map[string][]conditionalEdge),
		parallel: true,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(g)
		}
	}
	return g
}

// AddNode adds a named node with its handler to the graph.
// Returns the graph for chaining.
func (g *Graph) AddNode(name string, handler Handler) *Graph {
	if _, ok := g.nodes[name]; ok {
		return g
	}
	g.nodes[name] = handler
	return g
}

// AddEdge adds a directed edge from one node to another. Options can configure the edge.
// Returns the graph for chaining.
func (g *Graph) AddEdge(from, to string, opts ...EdgeOption) *Graph {
	for _, edge := range g.edges[from] {
		if edge.to == to {
			return g
		}
	}
	edge := conditionalEdge{to: to}
	for _, opt := range opts {
		opt(&edge)
	}
	if edge.group == "" {
		edge.group = to
	}
	g.edges[from] = append(g.edges[from], edge)
	return g
}

// SetEntryPoint marks a node as the entry point.
// Returns the graph for chaining.
func (g *Graph) SetEntryPoint(start string) *Graph {
	g.entryPoint = start
	return g
}

// SetFinishPoint marks a node as the finish point.
// Returns the graph for chaining.
func (g *Graph) SetFinishPoint(end string) *Graph {
	g.finishPoint = end
	return g
}

// validate ensures the graph configuration is correct before compiling.
func (g *Graph) validate() error {
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
func (g *Graph) ensureReachable() error {
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

// ensureAcyclic verifies that the graph does not contain directed cycles.
func (g *Graph) ensureAcyclic() error {
	const (
		stateUnvisited = iota
		stateVisiting
		stateVisited
	)
	states := make(map[string]int, len(g.nodes))
	stack := make([]string, 0, len(g.nodes))

	var visit func(string) error
	visit = func(node string) error {
		states[node] = stateVisiting
		stack = append(stack, node)

		for _, edge := range g.edges[node] {
			next := edge.to
			switch states[next] {
			case stateVisiting:
				cycleStart := 0
				for i, name := range stack {
					if name == next {
						cycleStart = i
						break
					}
				}
				cycle := append(append([]string{}, stack[cycleStart:]...), next)
				return fmt.Errorf("graph: cycles are not supported (cycle: %s)", strings.Join(cycle, " -> "))
			case stateUnvisited:
				if err := visit(next); err != nil {
					return err
				}
			}
		}

		stack = stack[:len(stack)-1]
		states[node] = stateVisited
		return nil
	}

	for name := range g.nodes {
		if states[name] == stateUnvisited {
			if err := visit(name); err != nil {
				return err
			}
		}
	}
	return nil
}

// Compile validates and compiles the graph into an Executor.
// Nodes wait for all activated incoming edges to complete before executing (join semantics).
// An edge is "activated" when its source node executes and chooses that edge.
func (g *Graph) Compile() (*Executor, error) {
	if err := g.validate(); err != nil {
		return nil, err
	}
	if err := g.ensureAcyclic(); err != nil {
		return nil, err
	}
	if err := g.ensureReachable(); err != nil {
		return nil, err
	}
	return NewExecutor(g), nil
}
