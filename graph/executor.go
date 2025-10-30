package graph

import (
	"context"
)

// Executor represents a compiled graph ready for execution. It is safe for
// concurrent use; each Execute call runs on an isolated execution context.
type Executor struct {
	graph        *Graph
	predecessors map[string][]string
	dependencies map[string]map[string]int
}

// NewExecutor creates a new Executor for the given graph.
func NewExecutor(g *Graph) *Executor {
	predecessors := make(map[string][]string)
	dependencies := make(map[string]map[string]int)
	for from, edges := range g.edges {
		for _, edge := range edges {
			predecessors[edge.to] = append(predecessors[edge.to], from)
			if dependencies[edge.to] == nil {
				dependencies[edge.to] = make(map[string]int)
			}
			dependencies[edge.to][edge.group]++
		}
	}
	return &Executor{
		graph:        g,
		predecessors: predecessors,
		dependencies: dependencies,
	}
}

func cloneDependencies(src map[string]map[string]int) map[string]map[string]int {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]map[string]int, len(src))
	for node, groups := range src {
		copied := make(map[string]int, len(groups))
		for group, count := range groups {
			copied[group] = count
		}
		dst[node] = copied
	}
	return dst
}

// Execute runs the graph task starting from the given state.
func (e *Executor) Execute(ctx context.Context, state State) (State, error) {
	t := newTask(e)
	return t.run(ctx, state)
}
