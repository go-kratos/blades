package flow

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades"
)

var (
	edgeEmpty blades.Runner = nil
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
	nodes   map[blades.Runner]struct{}
	edges   map[blades.Runner]blades.Runner
	branchs map[blades.Runner]*graphBranch
}

// NewGraph creates and returns a new empty Graph.
func NewGraph() *Graph {
	return &Graph{
		nodes:   make(map[blades.Runner]struct{}),
		edges:   make(map[blades.Runner]blades.Runner),
		branchs: make(map[blades.Runner]*graphBranch),
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
func (g *Graph) AddEdgeStart(to blades.Runner) {
	g.edges[edgeEmpty] = to
}

// AddEdgeEnd adds an edge from 'from' node to the special end node.
func (g *Graph) AddEdgeEnd(from blades.Runner) {
	g.edges[from] = edgeEmpty
}

// AddBranch adds a branching condition to the graph.
func (g *Graph) AddBranch(from blades.Runner, branch Branch, nodes map[string]blades.Runner) {
	g.branchs[from] = &graphBranch{branch: branch, nodes: nodes}
}

// Compile validates the graph structure and prepares it for execution.
func (g *Graph) Compile() (blades.Runner, error) {
	// Validate start node exists
	start, ok := g.edges[edgeEmpty]
	if !ok || start == nil {
		return nil, fmt.Errorf("graph: start node not defined")
	}
	// Validate nodes referenced by edges are registered
	if _, ok := g.nodes[start]; !ok {
		return nil, fmt.Errorf("graph: start node not registered")
	}
	for from, to := range g.edges {
		if from != edgeEmpty {
			if _, ok := g.nodes[from]; !ok {
				return nil, fmt.Errorf("graph: edge from node not registered")
			}
		}
		if to != edgeEmpty && to != nil { // ignore special end sentinel
			if _, ok := g.nodes[to]; !ok {
				return nil, fmt.Errorf("graph: edge to node not registered")
			}
		}
	}
	// Validate branch targets are registered
	for from, b := range g.branchs {
		if _, ok := g.nodes[from]; !ok {
			return nil, fmt.Errorf("graph: branch from node not registered")
		}
		if b == nil || b.branch == nil {
			return nil, fmt.Errorf("graph: branch function not defined")
		}
		for _, node := range b.nodes {
			if node == nil {
				return nil, fmt.Errorf("graph: branch target is nil")
			}
			if _, ok := g.nodes[node]; !ok {
				return nil, fmt.Errorf("graph: branch target node not registered")
			}
		}
	}
	// Return compiled runner
	return &graphRunner{graph: g, start: start}, nil
}

// graphRunner executes a compiled Graph.
type graphRunner struct {
	graph *Graph
	start blades.Runner
}

// next returns the next node given the current 'from' node.
func (gr *graphRunner) next(ctx context.Context, from blades.Runner) (blades.Runner, error) {
	if b, ok := gr.graph.branchs[from]; ok && b != nil && b.branch != nil {
		label, err := b.branch(ctx)
		if err != nil {
			return edgeEmpty, fmt.Errorf("graph: branch eval failed: %w", err)
		}
		next, ok := b.nodes[label]
		if !ok || next == nil {
			return edgeEmpty, fmt.Errorf("graph: branch target not found: %s", label)
		}
		return next, nil
	}
	if to, ok := gr.graph.edges[from]; ok {
		return to, nil
	}
	// If no edge, treat as end
	return edgeEmpty, nil
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
	for current != edgeEmpty {
		last, err = current.Run(ctx, state.Prompt, opts...)
		if err != nil {
			return nil, err
		}
		// Append output to prompt for downstream nodes and branching
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
		for current != edgeEmpty {
			stream, err := current.RunStream(ctx, state.Prompt, opts...)
			if err != nil {
				return err
			}
			defer stream.Close()
			var (
				last *blades.Generation
			)
			for stream.Next() {
				last, err = stream.Current()
				if err != nil {
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
