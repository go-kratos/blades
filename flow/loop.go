package flow

import (
	"context"

	"github.com/go-kratos/blades"
)

// LoopNodeOption defines options for configuring a LoopNode.
type LoopNodeOption func(*LoopNode)

// LoopMaxIterations sets the maximum number of iterations for loop nodes.
func LoopMaxIterations(max int) LoopNodeOption {
	return func(n *LoopNode) {
		n.maxIterations = max
	}
}

// ShouldContinue is a function that determines whether to continue looping.
type ShouldContinue func(context.Context) (bool, error)

// LoopNode represents a node that executes a loop.
type LoopNode struct {
	next           blades.Runner
	loop           blades.Runner
	shouldContinue ShouldContinue
	maxIterations  int
}

// NewLoop creates a loop node that will run the runner.
// If a condition is set via `WithCondition`, it continues while condition is true;
// otherwise it runs exactly once.
func NewLoop(shouldContinue ShouldContinue, runner blades.Runner, opts ...LoopNodeOption) *LoopNode {
	n := &LoopNode{shouldContinue: shouldContinue, loop: runner, maxIterations: 2}
	for _, opt := range opts {
		opt(n)
	}
	return n
}

// isNode marks LoopNode as a node type.
func (b *LoopNode) isNode() {}

// To links this node to the next node and returns the next for chaining.
func (n *LoopNode) To(next NodeRunner) {
	n.next = next
}

// Run executes the graph from this node onward, returning the final generation.
func (n *LoopNode) Run(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (*blades.Generation, error) {
	var (
		last *blades.Generation
	)
	state, ok := FromGraphContext(ctx)
	if !ok {
		return nil, ErrNoGraphState
	}
	state.Prompt = prompt
	iterations := 0
	for {
		if iterations >= n.maxIterations {
			break
		}
		iterations++
		loop, err := n.shouldContinue(ctx)
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
	if n.next != nil {
		return n.next.Run(ctx, state.Prompt, opts...)
	}
	return last, nil
}

// RunStream executes the graph from this node onward and streams each step's generation.
func (n *LoopNode) RunStream(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (blades.Streamer[*blades.Generation], error) {
	state, ok := FromGraphContext(ctx)
	if !ok {
		return nil, ErrNoGraphState
	}
	state.Prompt = prompt
	pipe := blades.NewStreamPipe[*blades.Generation]()
	defer pipe.Close()
	iterations := 0
	for {
		if iterations >= n.maxIterations {
			break
		}
		iterations++
		loop, err := n.shouldContinue(ctx)
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
