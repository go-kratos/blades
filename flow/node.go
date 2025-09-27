package flow

import (
	"context"

	"github.com/go-kratos/blades"
)

type NextNode struct {
	next   blades.Runner
	runner blades.Runner
}

// NewNode creates a simple node that runs the provided runner once.
func NewNode(runner blades.Runner) *NextNode {
	return &NextNode{runner: runner}
}

// To links this node to the next node and returns the next for chaining.
func (n *NextNode) To(next blades.Runner) {
	n.next = next
}

// Run executes the graph from this node onward, returning the final generation.
func (n *NextNode) Run(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (*blades.Generation, error) {
	var (
		err  error
		last *blades.Generation
	)
	state, ok := FromGraphContext(ctx)
	if !ok {
		return nil, ErrNoGraphState
	}
	state.Prompt = prompt
	last, err = n.runner.Run(ctx, state.Prompt, opts...)
	if err != nil {
		return nil, err
	}
	state.Prompt = blades.NewPrompt(last.Messages...)
	state.History = append(state.History, last.Messages...)
	if n.next != nil {
		return n.next.Run(ctx, state.Prompt, opts...)
	}
	return last, nil
}

// RunStream executes the graph from this node onward and streams each step's generation.
func (n *NextNode) RunStream(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (blades.Streamer[*blades.Generation], error) {
	state, ok := FromGraphContext(ctx)
	if !ok {
		return nil, ErrNoGraphState
	}
	state.Prompt = prompt
	pipe := blades.NewStreamPipe[*blades.Generation]()
	defer pipe.Close()
	// Run the first node to completion to get the updated prompt.
	last, err := n.runner.Run(ctx, state.Prompt, opts...)
	if err != nil {
		return nil, err
	}
	pipe.Send(last)
	state.Prompt = blades.NewPrompt(last.Messages...)
	state.History = append(state.History, last.Messages...)
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
