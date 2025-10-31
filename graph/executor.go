package graph

import (
	"context"
	"sort"
)

// Executor represents a compiled graph ready for execution. It is safe for
// concurrent use; each Execute call runs on an isolated execution context.
type Executor struct {
	graph        *Graph
	predecessors map[string][]string
	dependencies map[string]int
}

// NewExecutor creates a new Executor for the given graph.
func NewExecutor(g *Graph) *Executor {
	predecessors := make(map[string][]string)
	dependencies := make(map[string]int)
	for from, edges := range g.edges {
		for _, edge := range edges {
			predecessors[edge.to] = append(predecessors[edge.to], from)
			dependencies[edge.to]++
		}
	}
	for node, parents := range predecessors {
		sort.Strings(parents)
		predecessors[node] = parents
	}
	return &Executor{
		graph:        g,
		predecessors: predecessors,
		dependencies: dependencies,
	}
}

func cloneDependencies(src map[string]int) map[string]int {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]int, len(src))
	for node, count := range src {
		dst[node] = count
	}
	return dst
}

// Execute runs the graph task starting from the given state.
func (e *Executor) Execute(ctx context.Context, state State) (State, error) {
	t := newTask(e)
	return t.run(ctx, state)
}
