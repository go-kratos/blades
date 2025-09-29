package flow

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades"
)

// Branch is a function that determines the next node to execute based on the context.
type Branch func(context.Context) (string, error)

type graphBranch struct {
	branch Branch
	nodes  map[string]blades.Runner
}

// Graph represents a directed acyclic processing graph of runners.
// - Nodes are registered by name and must be unique.
// - Edges encode the default next node (at most one per node).
// - Conditions optionally override the next node at runtime.
// Execution starts from the single root (node with in-degree 0).
type Graph struct {
	nodes    map[blades.Runner]struct{}
	edges    map[blades.Runner]blades.Runner
	branches map[blades.Runner]*graphBranch
	start    blades.Runner
}

// NewGraph creates and returns a new empty Graph.
func NewGraph() *Graph {
	return &Graph{
		nodes:    make(map[blades.Runner]struct{}),
		edges:    make(map[blades.Runner]blades.Runner),
		branches: make(map[blades.Runner]*graphBranch),
	}
}

// AddNode adds a node (runner) to the graph.
func (g *Graph) AddNode(r blades.Runner) {
	g.nodes[r] = struct{}{}
}

// AddEdge adds a directed edge from 'from' node to 'to' node.
func (g *Graph) AddEdge(from, to blades.Runner) {
	g.edges[from] = to
}

// AddEdgeStart adds an edge from the special start node to 'to' node.
func (g *Graph) AddEdgeStart(to blades.Runner) { g.start = to }

// AddEdgeEnd adds an edge from 'from' node to the special end node.
func (g *Graph) AddEdgeEnd(from blades.Runner) {
	delete(g.edges, from)
}

// AddBranch adds a branching condition to the graph.
func (g *Graph) AddBranch(from blades.Runner, branch Branch, nodes map[string]blades.Runner) {
	g.branches[from] = &graphBranch{branch: branch, nodes: nodes}
}

// Compile validates the graph structure and prepares it for execution.
func (g *Graph) Compile() (blades.Runner, error) {
	// Validate start node exists and is registered
	if g.start == nil {
		return nil, fmt.Errorf("graph: start node not defined")
	}
	if _, ok := g.nodes[g.start]; !ok {
		return nil, fmt.Errorf("graph: start node not registered")
	}
	// Validate nodes referenced by edges are registered
	for from, to := range g.edges {
		if _, ok := g.nodes[from]; !ok {
			return nil, fmt.Errorf("graph: edge from node not registered")
		}
		if to != nil {
			if _, ok := g.nodes[to]; !ok {
				return nil, fmt.Errorf("graph: edge to node not registered")
			}
		}
	}
	// Validate branch targets are registered
	for from, b := range g.branches {
		if _, ok := g.nodes[from]; !ok {
			return nil, fmt.Errorf("graph: branch from node not registered")
		}
		if b == nil || b.branch == nil {
			return nil, fmt.Errorf("graph: branch function not defined")
		}
		for label, node := range b.nodes {
			if node == nil {
				return nil, fmt.Errorf("graph: branch target is nil")
			}
			if _, ok := g.nodes[node]; !ok {
				return nil, fmt.Errorf("graph: branch target node not registered: %s", label)
			}
		}
	}
	// Detect cycles in default edges (ignoring dynamic branches)
	visiting := make(map[blades.Runner]uint8) // 0=unseen,1=visiting,2=done
	var dfs func(n blades.Runner) error
	dfs = func(n blades.Runner) error {
		if n == nil {
			return nil
		}
		switch visiting[n] {
		case 1:
			return fmt.Errorf("graph: cycle detected in default edges")
		case 2:
			return nil
		}
		visiting[n] = 1
		if to, ok := g.edges[n]; ok {
			if err := dfs(to); err != nil {
				return err
			}
		}
		visiting[n] = 2
		return nil
	}
	if err := dfs(g.start); err != nil {
		return nil, err
	}
	// Return compiled runner
	return &graphRunner{graph: g, start: g.start}, nil
}

// graphRunner executes a compiled Graph.
type graphRunner struct {
	graph *Graph
	start blades.Runner
}

// next returns the next node given the current 'from' node.
func (gr *graphRunner) next(ctx context.Context, from blades.Runner) (blades.Runner, error) {
	if b, ok := gr.graph.branches[from]; ok && b != nil && b.branch != nil {
		label, err := b.branch(ctx)
		if err != nil {
			return nil, fmt.Errorf("graph: branch eval failed: %w", err)
		}
		next, ok := b.nodes[label]
		if !ok || next == nil {
			return nil, fmt.Errorf("graph: branch target not found: %s", label)
		}
		return next, nil
	}
	if to, ok := gr.graph.edges[from]; ok {
		return to, nil
	}
	// If no edge, treat as end
	return nil, nil
}

// Run executes the graph to completion and returns the final node's generation.
func (gr *graphRunner) Run(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (*blades.Generation, error) {
	state := NewGraphState(prompt)
	ctx = NewGraphContext(ctx, state)
	var (
		err  error
		last *blades.Generation
	)
	current := gr.start
	for current != nil {
		last, err = current.Run(ctx, state.Prompt, opts...)
		if err != nil {
			return nil, err
		}
		state.Prompt = blades.NewPrompt(last.Messages...)
		state.History = append(state.History, last.Messages...)
		next, err := gr.next(ctx, current)
		if err != nil {
			return nil, err
		}
		current = next
	}
	if last == nil {
		return &blades.Generation{}, nil
	}
	return last, nil
}

// RunStream executes the graph and streams each node's output sequentially.
func (gr *graphRunner) RunStream(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (blades.Streamer[*blades.Generation], error) {
	state := NewGraphState(prompt)
	ctx = NewGraphContext(ctx, state)
	pipe := blades.NewStreamPipe[*blades.Generation]()
	pipe.Go(func() error {
		current := gr.start
		for current != nil {
			stream, err := current.RunStream(ctx, state.Prompt, opts...)
			if err != nil {
				return err
			}
			var last *blades.Generation
			for stream.Next() {
				last, err = stream.Current()
				if err != nil {
					_ = stream.Close()
					return err
				}
				pipe.Send(last)
			}
			if err := stream.Close(); err != nil {
				return err
			}
			state.Prompt = blades.NewPrompt(last.Messages...)
			state.History = append(state.History, last.Messages...)
			next, err := gr.next(ctx, current)
			if err != nil {
				return err
			}
			current = next
		}
		return nil
	})
	return pipe, nil
}
