package blades

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades/model"
)

// ContextWindow describes the token budget and actual usage for a model request.
type ContextWindow struct {
	Budget         model.TokenCount
	Usage          model.TokenCount
	MessagesBefore int
	MessagesAfter  int
}

// Enforce checks that Usage does not exceed any non-zero Budget limit.
func (w ContextWindow) Enforce() error {
	type check struct {
		name   string
		limit  int64
		actual int64
	}
	checks := [...]check{
		{"input", w.Budget.Input, w.Usage.Total()},
		{"system", w.Budget.System, w.Usage.System},
		{"messages", w.Budget.Messages, w.Usage.Messages},
		{"tools", w.Budget.Tools, w.Usage.Tools},
	}
	for _, c := range checks {
		if c.limit <= 0 {
			continue
		}
		// For sub-segments, check if we have any segment data at all
		if c.name != "input" && !w.Usage.HasSegments() {
			return &BudgetError{Segment: c.name, Limit: c.limit, Unavailable: true}
		}
		if c.actual > c.limit {
			return &BudgetError{Segment: c.name, Limit: c.limit, Actual: c.actual}
		}
	}
	return nil
}

// BudgetError reports that an assembled request segment exceeds its budget.
type BudgetError struct {
	Segment     string
	Limit       int64
	Actual      int64
	Unavailable bool
}

func (e *BudgetError) Error() string {
	if e.Unavailable {
		return fmt.Sprintf("blades: context %s token count unavailable for budget limit %d", e.Segment, e.Limit)
	}
	return fmt.Sprintf("blades: context %s budget exceeded: %d > %d", e.Segment, e.Actual, e.Limit)
}

type contextWindowKey struct{}

func withContextWindow(ctx context.Context, w ContextWindow) context.Context {
	return context.WithValue(ctx, contextWindowKey{}, w)
}

// ContextWindowFrom retrieves the context window from ctx.
func ContextWindowFrom(ctx context.Context) (ContextWindow, bool) {
	w, ok := ctx.Value(contextWindowKey{}).(ContextWindow)
	return w, ok
}
