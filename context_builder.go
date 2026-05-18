package blades

import (
	"context"

	"github.com/go-kratos/blades/compact"
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
	msgs, err = b.compactIfNeeded(ctx, msgs)
	if err != nil {
		return nil, err
	}

	systemParts, err := buildSystemParts(ctx, b.agent.promptBuilders)
	if err != nil {
		return nil, err
	}
	system, err := prompt.JoinText(systemParts)
	if err != nil {
		return nil, err
	}

	req := &model.Request{
		Model:    b.agent.provider.Name(),
		Tools:    specsFromTools(b.allTools),
		System:   system,
		Messages: msgs,
	}
	if b.agent.outputSchema != nil {
		req.Options = append(req.Options, model.ResponseFormat{
			Schema: b.agent.outputSchema,
			Strict: true,
		})
	}
	return req, nil
}

func (b contextBuilder) compactIfNeeded(ctx context.Context, msgs []*model.Message) ([]*model.Message, error) {
	if b.agent.compactor == nil {
		return msgs, nil
	}
	threshold := b.agent.contextWindow.Threshold()
	if threshold > 0 {
		count, err := b.agent.tokenCounter.CountTokens(ctx, &model.Request{Messages: msgs})
		if err != nil {
			return nil, err
		}
		if count.Total() <= threshold {
			return msgs, nil
		}
	}
	return b.agent.compactor.Compact(ctx, compact.Request{
		Messages:     msgs,
		TokenCounter: b.agent.tokenCounter,
	})
}

func buildSystemParts(ctx context.Context, builders []prompt.Builder) ([]content.Part, error) {
	var systemParts []content.Part
	for _, builder := range builders {
		if builder == nil {
			continue
		}
		parts, err := builder.Build(ctx)
		if err != nil {
			return nil, err
		}
		systemParts = append(systemParts, parts...)
	}
	return systemParts, nil
}

func specsFromTools(allTools []tools.Tool) []tools.ToolSpec {
	toolSpecs := make([]tools.ToolSpec, 0, len(allTools))
	for _, tool := range allTools {
		if tool == nil {
			continue
		}
		toolSpecs = append(toolSpecs, tool.Spec())
	}
	return toolSpecs
}
