package policy

import (
	"context"
	"sync"
	"time"
)

// AllowAll returns a policy that allows everything.
func AllowAll() Policy {
	return PolicyFunc(func(_ context.Context, _ ToolRequest) (Decision, error) {
		return Decision{Action: Allow}, nil
	})
}

// DenyAll returns a policy that denies everything.
func DenyAll() Policy {
	return PolicyFunc(func(_ context.Context, _ ToolRequest) (Decision, error) {
		return Decision{Action: Deny, Reason: "denied by policy"}, nil
	})
}

// PolicyFunc is a function adapter for Policy.
type PolicyFunc func(ctx context.Context, req ToolRequest) (Decision, error)

func (f PolicyFunc) Check(ctx context.Context, req ToolRequest) (Decision, error) {
	return f(ctx, req)
}

// Chain evaluates policies in order; short-circuits on Deny/Ask/error.
func Chain(ps ...Policy) Policy {
	return PolicyFunc(func(ctx context.Context, req ToolRequest) (Decision, error) {
		for _, p := range ps {
			d, err := p.Check(ctx, req)
			if err != nil {
				return Decision{Action: Deny, Reason: err.Error()}, err
			}
			switch d.Action {
			case Deny, Ask:
				return d, nil
			case Modify:
				if d.Modified != nil {
					req = *d.Modified
				}
			}
		}
		return Decision{Action: Allow}, nil
	})
}

// BudgetLimit defines a budget constraint.
type BudgetLimit struct {
	MaxCalls int
	Window   time.Duration
}

// Budget creates a policy that limits tool calls within a time window.
func Budget(limit BudgetLimit, key func(context.Context, ToolRequest) string) Policy {
	type entry struct {
		count int
		reset time.Time
	}
	var mu sync.Mutex
	buckets := make(map[string]*entry)

	return PolicyFunc(func(ctx context.Context, req ToolRequest) (Decision, error) {
		k := ""
		if key != nil {
			k = key(ctx, req)
		}
		mu.Lock()
		defer mu.Unlock()

		now := time.Now()
		e, ok := buckets[k]
		if !ok || now.After(e.reset) {
			buckets[k] = &entry{count: 1, reset: now.Add(limit.Window)}
			return Decision{Action: Allow}, nil
		}
		if e.count >= limit.MaxCalls {
			return Decision{Action: Deny, Reason: "budget exceeded"}, nil
		}
		e.count++
		return Decision{Action: Allow}, nil
	})
}

// Limiter is an interface for rate limiting.
type Limiter interface {
	Allow(ctx context.Context, key string) bool
}

// RateLimit creates a policy that rate-limits tool calls.
func RateLimit(limiter Limiter, key func(context.Context, ToolRequest) string) Policy {
	return PolicyFunc(func(ctx context.Context, req ToolRequest) (Decision, error) {
		k := ""
		if key != nil {
			k = key(ctx, req)
		}
		if !limiter.Allow(ctx, k) {
			return Decision{Action: Deny, Reason: "rate limited"}, nil
		}
		return Decision{Action: Allow}, nil
	})
}

// SafetyFunc is a function that checks tool safety.
type SafetyFunc func(ctx context.Context, req ToolRequest) (Decision, error)

// SafetyCheck creates a policy from a safety check function.
func SafetyCheck(fn SafetyFunc) Policy {
	return PolicyFunc(func(ctx context.Context, req ToolRequest) (Decision, error) {
		return fn(ctx, req)
	})
}
