package flow

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades"
)

// Sentinel node keys used to declare graph start and end.
// Do not use these values as actual node names.
const (
	startKey = "START"
	endKey   = "END"
)

var (
	_ blades.Runner = (*Graph)(nil)
)

// Condition decides the next node key given the current context.
// Return an empty string to indicate termination from the current node.
type Condition func(context.Context) (string, error)

// Graph represents a directed acyclic processing graph of runners.
// - Nodes are registered by name and must be unique.
// - Edges encode the default next node (at most one per node).
// - Conditions optionally override the next node at runtime.
// Execution starts from the single root (node with in-degree 0).
type Graph struct {
	nodes      map[string]blades.Runner
	edges      map[string]string
	conditions map[string]Condition
}

// NewGraph creates an empty graph.
func NewGraph() *Graph {
	return &Graph{
		nodes:      make(map[string]blades.Runner),
		edges:      make(map[string]string),
		conditions: make(map[string]Condition),
	}
}

// AddStart adds the named start node to the graph.
func (g *Graph) AddStart(name string, node blades.Runner) {
	g.nodes[name] = node
	g.edges[startKey] = name
}

// AddNode adds a named node to the graph.
func (g *Graph) AddNode(name string, node blades.Runner) {
	g.nodes[name] = node
}

func (g *Graph) AddEnd(name string, node blades.Runner) {
	g.nodes[name] = node
	g.edges[endKey] = name
}

// AddEdge sets a directed edge from one node to another.
// Each node may have at most one default outgoing edge.
func (g *Graph) AddEdge(from string, to string) {
	g.edges[from] = to
}

// AddBranch sets a runtime condition for choosing the next node from "from".
// If present, the condition result overrides the default edge.
func (g Graph) AddBranch(from string, to Condition) {
	g.conditions[from] = to
}

// Run executes the graph from its single root, visiting nodes sequentially.
// The output of each node becomes the prompt for the next.
func (g *Graph) Run(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (*blades.Generation, error) {
	start, err := g.findStart()
	if err != nil {
		return nil, err
	}
	if _, ok := g.nodes[start]; !ok {
		return nil, fmt.Errorf("start node %q not found", start)
	}

	var (
		visited = make(map[string]struct{})
		current = start
		last    *blades.Generation
		state   = NewState(prompt)
	)
	ctx = NewStateContext(ctx, state)
	for {
		if _, seen := visited[current]; seen {
			return nil, fmt.Errorf("cycle detected at node %q: graph is not a DAG", current)
		}
		visited[current] = struct{}{}

		runner := g.nodes[current]
		if runner == nil {
			return nil, fmt.Errorf("node %q has no runner", current)
		}

		state.Current = current
		state.History = append(state.History, state.Prompt.Messages...)

		// Run the current node with the current prompt.
		gen, err := runner.Run(ctx, state.Prompt, opts...)
		if err != nil {
			return nil, fmt.Errorf("run node %q: %w", current, err)
		}
		last = gen

		state.Prompt = blades.NewPrompt(gen.Messages...)
		state.History = append(state.History, gen.Messages...)

		next, err := g.nextNode(ctx, current)
		if err != nil {
			return nil, err
		}
		if next == "" {
			break
		}
		if _, ok := g.nodes[next]; !ok {
			return nil, fmt.Errorf("next node %q not found from %q", next, current)
		}
		current = next
	}

	return last, nil
}

// RunStream executes the graph and yields each node's generation in order.
func (g *Graph) RunStream(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (blades.Streamer[*blades.Generation], error) {
	state := NewState(prompt)
	ctx = NewStateContext(ctx, state)
	pipe := blades.NewStreamPipe[*blades.Generation]()
	pipe.Go(func() error {
		start, err := g.findStart()
		if err != nil {
			return err
		}
		if _, ok := g.nodes[start]; !ok {
			return fmt.Errorf("start node %q not found", start)
		}

		var (
			visited = make(map[string]struct{})
			current = start
		)

		for {
			if _, seen := visited[current]; seen {
				return fmt.Errorf("cycle detected at node %q: graph is not a DAG", current)
			}
			visited[current] = struct{}{}

			runner := g.nodes[current]
			if runner == nil {
				return fmt.Errorf("node %q has no runner", current)
			}

			state.Current = current
			state.History = append(state.History, state.Prompt.Messages...)

			// Run the current node with the current prompt.
			gen, err := runner.Run(ctx, state.Prompt, opts...)
			if err != nil {
				return fmt.Errorf("run node %q: %w", current, err)
			}
			pipe.Send(gen)

			state.Prompt = blades.NewPrompt(gen.Messages...)
			state.History = append(state.History, gen.Messages...)

			next, err := g.nextNode(ctx, current)
			if err != nil {
				return err
			}
			if next == "" {
				break
			}
			if _, ok := g.nodes[next]; !ok {
				return fmt.Errorf("next node %q not found from %q", next, current)
			}
			current = next
		}
		return nil
	})
	return pipe, nil
}

// nextNode returns the next node key from the given current node.
// If a condition is present, it overrides the default edge.
func (g *Graph) nextNode(ctx context.Context, current string) (string, error) {
	if cond, ok := g.conditions[current]; ok {
		next, err := cond(ctx)
		if err != nil {
			return "", fmt.Errorf("branch from %q: %w", current, err)
		}
		if next == endKey || next == "" {
			return "", nil
		}
		return next, nil
	}
	if next, ok := g.edges[current]; ok {
		if next == endKey {
			return "", nil
		}
		return next, nil
	}
	return "", nil
}

// findRoot finds the single root node (in-degree 0) of the graph.
// Returns an error if none or multiple roots exist.
func (g *Graph) findStart() (string, error) {
	if len(g.nodes) == 0 {
		return "", fmt.Errorf("graph has no nodes")
	}
	if start, ok := g.edges[startKey]; ok {
		if _, exists := g.nodes[start]; !exists {
			return "", fmt.Errorf("start node %q not found", start)
		}
		return start, nil
	}
	return "", fmt.Errorf("graph start not defined; add AddEdge(START, \"<node>\")")
}
