package flow

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades"
)

// BranchSelector is a function that selects which branch to take.
type BranchSelector func(context.Context) (string, error)

// BranchNode represents a branching node in a prompt processing graph.
type BranchNode struct {
	branch   map[string]blades.Runner
	selector BranchSelector
}

// NewBranch creates a branch node with the given selector function.
func NewBranch(selector BranchSelector) *BranchNode {
	return &BranchNode{selector: selector, branch: make(map[string]blades.Runner)}
}

// Add adds a branch with the given key and runner.
func (b *BranchNode) Add(name string, node blades.Runner) {
	b.branch[name] = node
}

// Run executes the graph from this node onward, returning the final generation.
func (n *BranchNode) Run(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (*blades.Generation, error) {
	var (
		err  error
		last *blades.Generation
	)
	state, ok := FromGraphContext(ctx)
	if !ok {
		return nil, ErrNoGraphState
	}
	state.Prompt = prompt
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
	return last, nil
}

// RunStream executes the graph from this node onward and streams each step's generation.
func (n *BranchNode) RunStream(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (blades.Streamer[*blades.Generation], error) {
	state, ok := FromGraphContext(ctx)
	if !ok {
		return nil, ErrNoGraphState
	}
	state.Prompt = prompt
	pipe := blades.NewStreamPipe[*blades.Generation]()
	defer pipe.Close()
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
	return pipe, nil
}
