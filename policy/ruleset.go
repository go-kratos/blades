package policy

import "context"

// RuleSet evaluates rules in order; first match wins.
type RuleSet struct {
	rules    []Rule
	fallback Action
}

// NewRuleSet creates a RuleSet from rules.
func NewRuleSet(rules []Rule, opts ...RuleSetOption) *RuleSet {
	rs := &RuleSet{rules: rules, fallback: Ask}
	for _, opt := range opts {
		opt(rs)
	}
	return rs
}

// Check implements Policy.
func (rs *RuleSet) Check(_ context.Context, req ToolRequest) (Decision, error) {
	for _, rule := range rs.rules {
		if rule.Matcher.Match(req) {
			return Decision{Action: rule.Action, Reason: rule.Reason}, nil
		}
	}
	return Decision{Action: rs.fallback}, nil
}

// RuleSetOption configures a RuleSet.
type RuleSetOption func(*RuleSet)

// WithFallback sets the action when no rule matches (default: Ask).
func WithFallback(action Action) RuleSetOption {
	return func(rs *RuleSet) { rs.fallback = action }
}
