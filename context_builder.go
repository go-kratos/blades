package blades

import (
	"context"

	"github.com/go-kratos/blades/compact"
	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/prompt"
	"github.com/go-kratos/blades/session"
	"github.com/go-kratos/blades/skills"
	"github.com/go-kratos/blades/tools"
)

type contextBuilder struct {
	agent    *llmAgent
	sess     session.Session
	allTools []tools.Tool
	skills   *skills.Runtime
}

func (b contextBuilder) Build(ctx context.Context) (*model.Request, error) {
	msgs, err := b.sess.Messages(ctx)
	if err != nil {
		return nil, err
	}

	systemParts, err := buildSystemParts(ctx, b.agent.promptBuilders)
	if err != nil {
		return nil, err
	}
	if b.agent.skillToolset != nil {
		if instruction := b.agent.skillToolset.Instruction(); instruction != "" {
			systemParts = append(systemParts, content.Text{Text: instruction})
		}
	}
	system, err := prompt.JoinText(systemParts)
	if err != nil {
		return nil, err
	}

	visibleTools := b.allTools
	if b.skills != nil {
		visibleTools = b.skills.DisclosureFromMessages(msgs).FilterTools(b.allTools)
	}
	req := &model.Request{
		Model:    b.agent.provider.Name(),
		Tools:    specsFromTools(visibleTools),
		System:   system,
		Messages: msgs,
	}
	if b.agent.outputSchema != nil {
		req.Options = append(req.Options, model.ResponseFormat{
			Schema: b.agent.outputSchema,
			Strict: true,
		})
	}
	msgs, err = b.compactIfNeeded(ctx, req)
	if err != nil {
		return nil, err
	}
	req.Messages = msgs
	if b.skills != nil {
		disclosure := b.skills.DisclosureFromMessages(msgs)
		req.Tools = specsFromTools(disclosure.FilterTools(b.allTools))
	}
	return req, nil
}

func (b contextBuilder) compactIfNeeded(ctx context.Context, req *model.Request) ([]*model.Message, error) {
	if b.agent.compactor == nil {
		return req.Messages, nil
	}
	counter := b.agent.tokenCounter
	if counter == nil {
		counter = model.ApproxTokenCounter{}
	}
	threshold := b.agent.contextWindow.Threshold()
	if threshold > 0 {
		count, err := counter.CountTokens(ctx, req)
		if err != nil {
			return nil, err
		}
		if count.Total() <= threshold {
			return req.Messages, nil
		}
	}
	return b.agent.compactor.Compact(ctx, compact.Request{
		Messages:     req.Messages,
		TokenCounter: counter,
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
