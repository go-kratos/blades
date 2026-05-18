package blades

import (
	"errors"
	"fmt"

	"github.com/go-kratos/blades/model"
)

var (
	ErrModelProviderRequired = errors.New("blades: model provider is required")
	ErrNoToolsConfigured     = errors.New("blades: no tools configured")
	ErrMaxStepsExceeded      = errors.New("blades: maximum steps exceeded")
	ErrAgentNotStarted       = errors.New("blades: agent failed to start")
	ErrNoResult              = errors.New("blades: no result")
	ErrAgentNotForkable      = errors.New("blades: agent is not forkable")
)

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

// checkBudget verifies that usage does not exceed any non-zero budget limit.
func checkBudget(budget, usage model.TokenCount) error {
	type check struct {
		name   string
		limit  int64
		actual int64
	}
	checks := [...]check{
		{"input", budget.Input, usage.Total()},
		{"system", budget.System, usage.System},
		{"messages", budget.Messages, usage.Messages},
		{"tools", budget.Tools, usage.Tools},
	}
	for _, c := range checks {
		if c.limit <= 0 {
			continue
		}
		// For sub-segments, check if we have any segment data at all
		if c.name != "input" && !usage.HasSegments() {
			return &BudgetError{Segment: c.name, Limit: c.limit, Unavailable: true}
		}
		if c.actual > c.limit {
			return &BudgetError{Segment: c.name, Limit: c.limit, Actual: c.actual}
		}
	}
	return nil
}
