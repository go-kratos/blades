package tools

import (
	"context"
	"encoding/json"

	"github.com/google/jsonschema-go/jsonschema"
)

// ActionLoopExit is the action key set by ExitTool on the ToolContext to
// signal that the enclosing loop should stop. The value is an ExitInput.
const ActionLoopExit = "loop_exit"

// ExitInput is the argument schema for ExitTool.
type ExitInput struct {
	Reason   string `json:"reason"             jsonschema:"Reason for exiting the loop."`
	Escalate bool   `json:"escalate,omitempty" jsonschema:"If true, escalate to the outer handler instead of completing normally."`
}

// ExitTool signals the enclosing loop to stop. Register it via blades.WithTools
// on a sub-agent. When invoked by the LLM it sets ActionLoopExit on the
// ToolContext so that the loop agent can observe it via message.Actions.
// If called outside a loop (no ToolContext in context) the call is a no-op.
type ExitTool struct {
	inputSchema *jsonschema.Schema
}

// NewExitTool creates a ready-to-use ExitTool.
func NewExitTool() *ExitTool {
	schema, _ := jsonschema.For[ExitInput](nil)
	return &ExitTool{inputSchema: schema}
}

func (t *ExitTool) Name() string                     { return "exit" }
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
	if tc, ok := FromContext(ctx); ok {
		tc.SetAction(ActionLoopExit, &req)
	}
	return `{"ok":true}`, nil
}
