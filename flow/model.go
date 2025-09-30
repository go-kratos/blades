package flow

import (
	"context"

	"github.com/go-kratos/blades"
)

var _ Node = (*ModelNode)(nil)

type ModelNode struct {
	runner         *blades.OutputConverter[*NodeOutput]
	transformer    Transformer[*NodeInput]
	userTemplate   string
	systemTemplate string
}

func NewModelNode(runner blades.Runner[*blades.Prompt, *blades.Generation, blades.ModelOption]) *ModelNode {
	return &ModelNode{
		runner: blades.NewOutputConverter[*NodeOutput](runner),
	}
}

func (n *ModelNode) Run(ctx context.Context, input *NodeInput, opts ...NodeOption) (*NodeOutput, error) {
	input, err := n.transformer(input)
	if err != nil {
		return nil, err
	}
	params := make(map[string]any)
	for _, param := range input.Inputs {
		params[param.Name] = param.Value
	}
	prompt, err := blades.NewPromptTemplate().
		User(n.userTemplate, params).
		System(n.systemTemplate, params).
		Build()
	if err != nil {
		return nil, err
	}
	return n.runner.Run(ctx, prompt)
}

func (n *ModelNode) RunStream(context.Context, *NodeInput, ...NodeOption) (blades.Streamer[*NodeOutput], error) {
	return nil, nil
}
