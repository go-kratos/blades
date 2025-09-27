package flow

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-kratos/blades"
)

var (
	ErrNoGraphState = errors.New("no graph state in context")
)

// GraphNodeOption defines options for configuring a GraphNode.
type GraphNodeOption func(*GraphNode)

// WithMaxIterations sets the maximum number of iterations for loop nodes.
func WithMaxIterations(max int) GraphNodeOption {
	return func(n *GraphNode) {
		n.maxIterations = max
	}
}

// LoopCondition decides branching or loop continuation.
// Return true to select the first branch or continue the loop.
type LoopCondition func(context.Context) (bool, error)

// BranchCondition decides which branch to take.
type BranchCondition func(context.Context) (string, error)

// GraphNode represents a node in a prompt processing graph.
// A node can be one of:
// - single runner (`node`)
// - branch with two runners (`branch` with `condition`)
// - loop runner (`loop` with optional `condition`)
type GraphNode struct {
	next *GraphNode
	node blades.Runner
	// loop
	loop      blades.Runner
	condition LoopCondition
	// branch
	branch   map[string]blades.Runner
	selector BranchCondition
	// maxIterations limits loop iterations; defaults to 2 if not set.
	maxIterations int
}

// NewNode creates a simple node that runs the provided runner once.
func NewNode(runner blades.Runner) *GraphNode {
	return &GraphNode{node: runner}
}

// NewLoop creates a loop node that will run the runner.
// If a condition is set via `WithCondition`, it continues while condition is true;
// otherwise it runs exactly once.
func NewLoop(condition LoopCondition, runner blades.Runner, opts ...GraphNodeOption) *GraphNode {
	n := &GraphNode{condition: condition, loop: runner, maxIterations: 2}
	for _, opt := range opts {
		opt(n)
	}
	return n
}

// NewBranch creates a branch node; when condition is true it uses `a`, otherwise `b`.
func NewBranch(condition BranchCondition, branch map[string]blades.Runner) *GraphNode {
	return &GraphNode{selector: condition, branch: branch}
}

// To links this node to the next node and returns the next for chaining.
func (n *GraphNode) To(next *GraphNode) *GraphNode {
	n.next = next
	return next
}

// Run executes the graph from this node onward, returning the final generation.
func (n *GraphNode) Run(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (*blades.Generation, error) {
	var (
		err  error
		last *blades.Generation
	)
	state, ok := FromGraphContext(ctx)
	if !ok {
		return nil, ErrNoGraphState
	}
	state.Prompt = prompt
	switch {
	case n.node != nil:
		last, err = n.node.Run(ctx, state.Prompt, opts...)
		if err != nil {
			return nil, err
		}
		state.Prompt = blades.NewPrompt(last.Messages...)
		state.History = append(state.History, last.Messages...)
	case n.loop != nil:
		iterations := 0
		for {
			if iterations >= n.maxIterations {
				break
			}
			iterations++
			loop, err := n.condition(ctx)
			if err != nil {
				return nil, err
			}
			if !loop {
				break
			}
			last, err = n.loop.Run(ctx, state.Prompt, opts...)
			if err != nil {
				return nil, err
			}
			state.Prompt = blades.NewPrompt(last.Messages...)
			state.History = append(state.History, last.Messages...)
		}
	case len(n.branch) > 0:
		choose, err := n.selector(ctx)
		if err != nil {
			return nil, err
		}
		runner, ok := n.branch[choose]
		if !ok {
			return nil, fmt.Errorf("invalid branch choice: %s", choose)
		}
		last, err = runner.Run(ctx, state.Prompt, opts...)
		if err != nil {
			return nil, err
		}
		state.Prompt = blades.NewPrompt(last.Messages...)
		state.History = append(state.History, last.Messages...)
	}
	if n.next != nil {
		return n.next.Run(ctx, state.Prompt, opts...)
	}
	return last, nil
}

// RunStream executes the graph from this node onward and streams each step's generation.
func (n *GraphNode) RunStream(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (blades.Streamer[*blades.Generation], error) {
	state, ok := FromGraphContext(ctx)
	if !ok {
		return nil, ErrNoGraphState
	}
	state.Prompt = prompt
	pipe := blades.NewStreamPipe[*blades.Generation]()
	defer pipe.Close()
	// Mirror Run's logic: execute current node, then stream the rest recursively.
	switch {
	case n.node != nil:
		last, err := n.node.Run(ctx, state.Prompt, opts...)
		if err != nil {
			return nil, err
		}
		pipe.Send(last)
		state.Prompt = blades.NewPrompt(last.Messages...)
		state.History = append(state.History, last.Messages...)
	case n.loop != nil:
		for {
			loop, err := n.condition(ctx)
			if err != nil {
				return nil, err
			}
			if !loop {
				break
			}
			last, err := n.loop.Run(ctx, state.Prompt, opts...)
			if err != nil {
				return nil, err
			}
			pipe.Send(last)
			state.Prompt = blades.NewPrompt(last.Messages...)
			state.History = append(state.History, last.Messages...)
		}
	case len(n.branch) > 0:
		choose, err := n.selector(ctx)
		if err != nil {
			return nil, err
		}
		runner, ok := n.branch[choose]
		if !ok {
			return nil, fmt.Errorf("invalid branch choice: %s", choose)
		}
		last, err := runner.Run(ctx, state.Prompt, opts...)
		if err != nil {
			return nil, err
		}
		pipe.Send(last)
		state.Prompt = blades.NewPrompt(last.Messages...)
		state.History = append(state.History, last.Messages...)
	}
	// Stream the remainder of the graph using recursion, mirroring Run.
	if n.next != nil {
		stream, err := n.next.RunStream(ctx, state.Prompt, opts...)
		if err != nil {
			return nil, err
		}
		defer stream.Close()
		for stream.Next() {
			gen, err := stream.Current()
			if err != nil {
				return nil, err
			}
			pipe.Send(gen)
		}
	}
	return pipe, nil
}
