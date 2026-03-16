package tools

import (
	"context"
	"encoding/json"

	"github.com/google/jsonschema-go/jsonschema"
)

// LoopExiter is implemented by a loop's LoopState and allows tools running
// inside a loop iteration to signal that the loop should stop.
type LoopExiter interface {
	// ExitLoop signals that the loop should stop after the current iteration.
	// If escalate is true the loop returns blades.ErrLoopEscalated to its caller.
	ExitLoop(reason string, escalate bool)
}

type ctxLoopExiterKey struct{}

// WithLoopExiter returns a child context that carries the given LoopExiter.
func WithLoopExiter(ctx context.Context, e LoopExiter) context.Context {
	return context.WithValue(ctx, ctxLoopExiterKey{}, e)
}

// LoopExiterFromContext retrieves the LoopExiter stored by WithLoopExiter.
func LoopExiterFromContext(ctx context.Context) (LoopExiter, bool) {
	e, ok := ctx.Value(ctxLoopExiterKey{}).(LoopExiter)
	return e, ok
}

// ExitInput is the argument schema for ExitTool.
type ExitInput struct {
	Reason   string `json:"reason"             jsonschema:"Reason for exiting the loop."`
	Escalate bool   `json:"escalate,omitempty" jsonschema:"If true, escalate to the outer handler instead of completing normally."`
}

// ExitTool signals the enclosing loop to stop. Register it via blades.WithTools
// on a sub-agent; the loop picks up the signal through the context automatically.
// If called outside a loop the call is a silent no-op.
type ExitTool struct {
	inputSchema *jsonschema.Schema
}

// NewExitTool creates a ready-to-use ExitTool.
func NewExitTool() *ExitTool {
	schema, _ := jsonschema.For[ExitInput](nil)
	return &ExitTool{inputSchema: schema}
}

func (t *ExitTool) Name() string        { return "exit" }
func (t *ExitTool) InputSchema() *jsonschema.Schema  { return t.inputSchema }
func (t *ExitTool) OutputSchema() *jsonschema.Schema { return nil }

func (t *ExitTool) Description() string {
	return "Signal that the current loop should stop. Call this when the task is complete or when escalation is required."
}

// Handle is called by the agent runtime when the LLM invokes the exit tool.
func (t *ExitTool) Handle(ctx context.Context, input string) (string, error) {
	var req ExitInput
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return "", err
	}
	if e, ok := LoopExiterFromContext(ctx); ok {
		e.ExitLoop(req.Reason, req.Escalate)
	}
	return `{"ok":true}`, nil
}
