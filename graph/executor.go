package graph

import (
	"context"
)

// Executor represents a compiled graph ready for execution. It is safe for
// concurrent use; each Execute call runs on an isolated execution context.
type Executor struct {
	graph        *Graph
	predecessors map[string][]string
}

// NewExecutor creates a new Executor for the given graph.
func NewExecutor(g *Graph) *Executor {
	predecessors := make(map[string][]string)
	for from, edges := range g.edges {
		for _, edge := range edges {
			predecessors[edge.to] = append(predecessors[edge.to], from)
		}
	}
	return &Executor{
		graph:        g,
		predecessors: predecessors,
	}
}

// Execute runs the graph task starting from the given state.
func (e *Executor) Execute(ctx context.Context, state State) (State, error) {
	t := &Task{
		executor: e,
		queue: []Step{{
			node:  e.graph.entryPoint,
			state: state,
		}},
		pending:     make(map[string]State),
		visited:     make(map[string]bool, len(e.graph.nodes)),
		skippedCnt:  make(map[string]int, len(e.graph.nodes)),
		skippedFrom: make(map[string]map[string]bool, len(e.graph.nodes)),
	}
	return t.execute(ctx)
}
