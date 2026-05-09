package policy

import (
	"context"
	"path"
)

// Permissions defines allow/deny rules for tool invocation.
// Rules are tool name glob patterns evaluated in deny-first order.
type Permissions struct {
	Allow []string
	Deny  []string
}

// NewPermissions creates a Policy from allow/deny rules.
func NewPermissions(p Permissions) Policy {
	return PolicyFunc(func(_ context.Context, req ToolRequest) (Decision, error) {
		name := req.Tool.Spec().Name

		for _, pattern := range p.Deny {
			if matchPattern(pattern, name) {
				return Decision{Action: Deny, Reason: "denied by rule: " + pattern}, nil
			}
		}

		for _, pattern := range p.Allow {
			if matchPattern(pattern, name) {
				return Decision{Action: Allow}, nil
			}
		}

		return Decision{Action: Ask}, nil
	})
}

func matchPattern(pattern, name string) bool {
	matched, _ := path.Match(pattern, name)
	return matched
}
