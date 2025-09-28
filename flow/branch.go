package flow

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades"
)

// BranchSelector is a function that selects which branch to take.
type BranchSelector func(context.Context) (string, error)

// Branch represents a branching node in a prompt processing graph.
type Branch struct {
	branch   map[string]Flowable
	selector BranchSelector
}

// NewBranch creates a branch node with the given selector function.
func NewBranch(selector BranchSelector) *Branch {
	return &Branch{selector: selector, branch: make(map[string]Flowable)}
}

// isFlowable marks Branch as implementing the Flowable interface.
func (b *Branch) isFlowable() {}

// Add adds a branch with the given key and runner.
func (b *Branch) Add(name string, node Flowable) {
	b.branch[name] = node
}

// Run executes the graph from this node onward, returning the final generation.
func (n *Branch) Run(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (*blades.Generation, error) {
	var (
		err  error
		last *blades.Generation
	)
	state, ok := FromContext(ctx)
	if !ok {
		return nil, ErrNoFlowState
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
func (n *Branch) RunStream(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (blades.Streamer[*blades.Generation], error) {
	pipe := blades.NewStreamPipe[*blades.Generation]()
	pipe.Go(func() error {
		last, err := n.Run(ctx, prompt, opts...)
		if err != nil {
			return err
		}
		pipe.Send(last)
		return nil
	})
	return pipe, nil
}
