package flow

import (
	"context"

	"github.com/go-kratos/blades"
)

var _ Flowable = (*Loop)(nil)

// LoopOption defines options for configuring a Loop.
type LoopOption func(*Loop)

// LoopMaxIterations sets the maximum number of iterations for loop nodes.
func LoopMaxIterations(max int) LoopOption {
	return func(n *Loop) {
		n.maxIterations = max
	}
}

// ShouldContinue is a function that determines whether to continue looping.
type ShouldContinue func(context.Context) (bool, error)

// Loop represents a node that executes a loop.
type Loop struct {
	next           Node
	runner         blades.Runner
	shouldContinue ShouldContinue
	maxIterations  int
}

// NewLoop creates a loop node that will run the runner.
// If a condition is set via `WithCondition`, it continues while condition is true;
// otherwise it runs exactly once.
func NewLoop(shouldContinue ShouldContinue, runner blades.Runner, opts ...LoopOption) *Loop {
	n := &Loop{shouldContinue: shouldContinue, runner: runner, maxIterations: 2}
	for _, opt := range opts {
		opt(n)
	}
	return n
}

// isFlowable marks this struct as a Flowable.
func (b *Loop) isNode() {}

// To links this node to the next node and returns the next for chaining.
func (n *Loop) To(next Node) {
	n.next = next
}

// Run executes the graph from this node onward, returning the final generation.
func (n *Loop) Run(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (*blades.Generation, error) {
	var (
		last *blades.Generation
	)
	state, ok := FromContext(ctx)
	if !ok {
		return nil, ErrNoFlowState
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
		last, err = n.runner.Run(ctx, state.Prompt, opts...)
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
func (n *Loop) RunStream(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (blades.Streamer[*blades.Generation], error) {
	state, ok := FromContext(ctx)
	if !ok {
		return nil, ErrNoFlowState
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
		last, err := n.runner.Run(ctx, state.Prompt, opts...)
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
