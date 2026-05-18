package policy

import (
	"context"
	"encoding/json"

	"github.com/go-kratos/blades/tools"
)

// Action represents the outcome of a policy check.
type Action string

const (
	Allow  Action = "allow"
	Deny   Action = "deny"
	Ask    Action = "ask"
	Modify Action = "modify"
)

// Decision is the result of a Policy check.
type Decision struct {
	Action   Action
	Reason   string
	Modified *ToolRequest
	Metadata map[string]any
}

// ToolRequest is the input to a policy check.
type ToolRequest struct {
	Tool  tools.Tool
	Input json.RawMessage
}

// Policy decides whether a tool invocation is allowed.
type Policy interface {
	Check(ctx context.Context, req ToolRequest) (Decision, error)
}

// PolicyFunc is a function adapter for Policy.
type PolicyFunc func(ctx context.Context, req ToolRequest) (Decision, error)

func (f PolicyFunc) Check(ctx context.Context, req ToolRequest) (Decision, error) {
	return f(ctx, req)
}
