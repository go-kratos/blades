package blades

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades/model"
)

// ContextPurpose identifies why a model request context is being built.
type ContextPurpose string

const (
	// ContextPurposeMain is the normal agent model-call path.
	ContextPurposeMain ContextPurpose = "main"
)

// ContextBudget describes caller-owned token budget slices for a request view.
//
// Zero values mean "not enforced" for that segment. ResponseReserveTokens is
// descriptive context for prompt and memory builders; the default Agent does
// not enforce it.
type ContextBudget struct {
	InputTokens           int64
	SystemTokens          int64
	MessagesTokens        int64
	ToolTokens            int64
	ResponseReserveTokens int64
}

// ContextInfo is available while the default Agent builds a model request.
type ContextInfo struct {
	Purpose ContextPurpose
	Budget  ContextBudget
}

type contextInfoKey struct{}

func newContextInfo(ctx context.Context, info ContextInfo) context.Context {
	if info.Purpose == "" {
		info.Purpose = ContextPurposeMain
	}
	return context.WithValue(ctx, contextInfoKey{}, info)
}

// ContextInfoFromContext retrieves request-context information from ctx.
func ContextInfoFromContext(ctx context.Context) (ContextInfo, bool) {
	info, ok := ctx.Value(contextInfoKey{}).(ContextInfo)
	return info, ok
}

// ContextStats describes the assembled model request view.
type ContextStats struct {
	Purpose        ContextPurpose
	Budget         ContextBudget
	Count          model.TokenCount
	MessagesBefore int
	MessagesAfter  int
}

type contextStatsKey struct{}

func newContextStats(ctx context.Context, stats ContextStats) context.Context {
	if stats.Purpose == "" {
		stats.Purpose = ContextPurposeMain
	}
	return context.WithValue(ctx, contextStatsKey{}, stats)
}

// ContextStatsFromContext retrieves request-context stats from ctx.
func ContextStatsFromContext(ctx context.Context) (ContextStats, bool) {
	stats, ok := ctx.Value(contextStatsKey{}).(ContextStats)
	return stats, ok
}

// ContextBudgetSegment identifies a budgeted request segment.
type ContextBudgetSegment string

const (
	// ContextSegmentInput is the full provider input request.
	ContextSegmentInput ContextBudgetSegment = "input"
	// ContextSegmentSystem is the model.Request.System segment.
	ContextSegmentSystem ContextBudgetSegment = "system"
	// ContextSegmentMessages is the model.Request.Messages segment.
	ContextSegmentMessages ContextBudgetSegment = "messages"
	// ContextSegmentTools is the model.Request.Tools segment.
	ContextSegmentTools ContextBudgetSegment = "tools"
)

// ContextBudgetError reports that an assembled request segment exceeds budget.
type ContextBudgetError struct {
	Segment     ContextBudgetSegment
	Limit       int64
	Actual      int64
	Unavailable bool
}

func (e *ContextBudgetError) Error() string {
	if e.Unavailable {
		return fmt.Sprintf("blades: context %s token count unavailable for budget limit %d", e.Segment, e.Limit)
	}
	return fmt.Sprintf("blades: context %s budget exceeded: %d > %d", e.Segment, e.Actual, e.Limit)
}
