package flow

import (
	"context"

	"github.com/go-kratos/blades"
)

var _ Flowable = (*FlowNode)(nil)

// FlowNode is a node in a flow graph that runs a single runner and passes its output to the next node.
type FlowNode struct {
	next   Node
	runner blades.Runner
}

// NewNode creates a simple node that runs the provided runner once.
func NewNode(runner blades.Runner) *FlowNode {
	return &FlowNode{runner: runner}
}

// isNode is a marker method to indicate this struct is a FlowNode.
func (n *FlowNode) isNode() {}

// To links this node to the next node and returns the next for chaining.
func (n *FlowNode) To(next Node) {
	n.next = next
}

// Run executes the graph from this node onward, returning the final generation.
func (n *FlowNode) Run(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (*blades.Generation, error) {
	var (
		err  error
		last *blades.Generation
	)
	state, ok := FromContext(ctx)
	if !ok {
		return nil, ErrNoFlowState
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
func (n *FlowNode) RunStream(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (blades.Streamer[*blades.Generation], error) {
	state, ok := FromContext(ctx)
	if !ok {
		return nil, ErrNoFlowState
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
