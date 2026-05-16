package blades

import (
	"context"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/prompt"
	"github.com/go-kratos/blades/session"
	"github.com/go-kratos/blades/tools"
)

type contextBuilder struct {
	agent    *llmAgent
	sess     session.Session
	allTools []tools.Tool
}

func (b contextBuilder) Build(ctx context.Context) (*model.Request, error) {
	msgs, err := b.sess.Messages(ctx)
	if err != nil {
		return nil, err
	}
	if b.agent.compactor != nil {
		msgs, err = b.agent.compactor.Compact(ctx, msgs)
		if err != nil {
			return nil, err
		}
	}
	var systemParts []content.Part
	for _, builder := range b.agent.promptBuilders {
		if builder == nil {
			continue
		}
		parts, err := builder.Build(ctx)
		if err != nil {
			return nil, err
		}
		systemParts = append(systemParts, parts...)
	}
	system, err := prompt.JoinText(systemParts)
	if err != nil {
		return nil, err
	}
	toolSpecs := make([]tools.ToolSpec, 0, len(b.allTools))
	for _, t := range b.allTools {
		toolSpecs = append(toolSpecs, t.Spec())
	}
	return &model.Request{
		Model:    b.agent.provider.Name(),
		System:   system,
		Messages: msgs,
		Tools:    toolSpecs,
	}, nil
}
