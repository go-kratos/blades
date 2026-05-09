package content

import "encoding/json"

// ToolUse represents a tool invocation request from the model.
type ToolUse struct {
	ID    string
	Name  string
	Input json.RawMessage
}

func (ToolUse) part() {}

// ToolResult represents the result of a tool invocation.
type ToolResult struct {
	ID      string
	Name    string
	Parts   []Part
	IsError bool
}

func (ToolResult) part() {}
