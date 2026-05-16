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

func (b contextBuilder) Build(ctx context.Context) (*model.Request, ContextStats, error) {
	counter := b.agent.tokenCounter
	ctx = newContextInfo(ctx, ContextInfo{
		Purpose: ContextPurposeMain,
		Budget:  b.agent.contextBudget,
	})

	msgs, err := b.sess.Messages(ctx)
	if err != nil {
		return nil, ContextStats{}, err
	}
	messagesBefore := len(msgs)
	if b.agent.compactor != nil {
		msgs, err = b.agent.compactor.Compact(ctx, compact.Request{
			Messages:     msgs,
			TokenCounter: counter,
		})
		if err != nil {
			return nil, ContextStats{}, err
		}
	}

	systemParts, err := buildSystemParts(ctx, b.agent.promptBuilders)
	if err != nil {
		return nil, ContextStats{}, err
	}
	system, err := prompt.JoinText(systemParts)
	if err != nil {
		return nil, ContextStats{}, err
	}

	toolSpecs := specsFromTools(b.allTools)
	req := &model.Request{
		Model:    b.agent.provider.Name(),
		System:   system,
		Messages: msgs,
		Tools:    toolSpecs,
	}
	stats, err := contextStatsForRequest(ctx, req, b.agent.contextBudget, messagesBefore, counter)
	if err != nil {
		return nil, ContextStats{}, err
	}
	if err := enforceContextBudget(b.agent.contextBudget, stats); err != nil {
		return nil, ContextStats{}, err
	}
	return req, stats, nil
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

func contextStatsForRequest(ctx context.Context, req *model.Request, budget ContextBudget, messagesBefore int, counter model.TokenCounter) (ContextStats, error) {
	stats := ContextStats{
		Purpose:        ContextPurposeMain,
		Budget:         budget,
		MessagesBefore: messagesBefore,
		MessagesAfter:  len(req.Messages),
	}
	if counter == nil {
		return stats, nil
	}
	counts, err := counter.CountTokens(ctx, req)
	if err != nil {
		return ContextStats{}, fmt.Errorf("blades: count model request tokens: %w", err)
	}
	stats.Count = normalizeTokenCount(counts)
	return stats, nil
}

func enforceContextBudget(budget ContextBudget, stats ContextStats) error {
	if budget.InputTokens > 0 && stats.Count.InputTokens > budget.InputTokens {
		return &ContextBudgetError{Segment: ContextSegmentInput, Limit: budget.InputTokens, Actual: stats.Count.InputTokens}
	}
	if budget.SystemTokens > 0 {
		if !stats.Count.HasBreakdown {
			return &ContextBudgetError{Segment: ContextSegmentSystem, Limit: budget.SystemTokens, Unavailable: true}
		}
		if stats.Count.SystemTokens > budget.SystemTokens {
			return &ContextBudgetError{Segment: ContextSegmentSystem, Limit: budget.SystemTokens, Actual: stats.Count.SystemTokens}
		}
	}
	if budget.MessagesTokens > 0 {
		if !stats.Count.HasBreakdown {
			return &ContextBudgetError{Segment: ContextSegmentMessages, Limit: budget.MessagesTokens, Unavailable: true}
		}
		if stats.Count.MessagesTokens > budget.MessagesTokens {
			return &ContextBudgetError{Segment: ContextSegmentMessages, Limit: budget.MessagesTokens, Actual: stats.Count.MessagesTokens}
		}
	}
	if budget.ToolTokens > 0 {
		if !stats.Count.HasBreakdown {
			return &ContextBudgetError{Segment: ContextSegmentTools, Limit: budget.ToolTokens, Unavailable: true}
		}
		if stats.Count.ToolTokens > budget.ToolTokens {
			return &ContextBudgetError{Segment: ContextSegmentTools, Limit: budget.ToolTokens, Actual: stats.Count.ToolTokens}
		}
	}
	return nil
}

func normalizeTokenCount(count model.TokenCount) model.TokenCount {
	if count.HasBreakdown && count.InputTokens == 0 {
		count.InputTokens = count.SystemTokens + count.MessagesTokens + count.ToolTokens
	}
	return count
}
