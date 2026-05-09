package tools

import (
	"context"
	"encoding/json"

	"github.com/go-kratos/blades/content"
)

// Tool is the core interface for agent tools.
type Tool interface {
	Spec() ToolSpec
	Handle(ctx context.Context, input json.RawMessage) (*Result, error)
}

// Result is the output of a tool invocation.
type Result struct {
	Parts []content.Part
}

// TextResult creates a Result with a single text part.
func TextResult(text string) *Result {
	return &Result{Parts: []content.Part{content.Text{Text: text}}}
}
