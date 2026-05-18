package policy

import "context"

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

// Chain evaluates policies in order; short-circuits on Deny/Ask/error.
// On Allow, continues to the next policy. On Modify, applies the
// modification and continues. If all policies pass, returns Allow.
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

// FromRules creates a Policy from a list of rules.
func FromRules(rules []Rule, opts ...RuleSetOption) Policy {
	return NewRuleSet(rules, opts...)
}
