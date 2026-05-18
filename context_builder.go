package blades

import (
	"context"
	"fmt"

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
	if b.agent.compactor != nil {
		msgs, err = b.agent.compactor.Compact(ctx, compact.Request{
			Messages:     msgs,
			TokenCounter: b.agent.tokenCounter,
		})
		if err != nil {
			return nil, err
		}
	}

	systemParts, err := buildSystemParts(ctx, b.agent.promptBuilders)
	if err != nil {
		return nil, err
	}
	system, err := prompt.JoinText(systemParts)
	if err != nil {
		return nil, err
	}

	toolSpecs := specsFromTools(b.allTools)
	req := &model.Request{
		Model:    b.agent.provider.Name(),
		System:   system,
		Messages: msgs,
		Tools:    toolSpecs,
	}
	if err := b.enforceBudget(ctx, req); err != nil {
		return nil, err
	}
	return req, nil
}

func (b contextBuilder) enforceBudget(ctx context.Context, req *model.Request) error {
	budget := b.agent.contextBudget
	if budget == (model.TokenCount{}) {
		return nil // no budget configured
	}
	if b.agent.tokenCounter == nil {
		return nil
	}
	usage, err := b.agent.tokenCounter.CountTokens(ctx, req)
	if err != nil {
		return fmt.Errorf("blades: count model request tokens: %w", err)
	}
	usage = normalizeUsage(usage)
	return checkBudget(budget, usage)
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

func normalizeUsage(c model.TokenCount) model.TokenCount {
	if c.Input == 0 {
		c.Input = c.Total()
	}
	return c
}
