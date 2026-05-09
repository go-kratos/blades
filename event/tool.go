package event

import (
	"encoding/json"

	"github.com/go-kratos/blades/content"
)

// ToolStart signals the beginning of a tool invocation.
type ToolStart struct {
	ID    string
	Name  string
	Input json.RawMessage
}

func (ToolStart) output() {}

// ToolDelta carries incremental output from a streaming tool.
type ToolDelta struct {
	ID   string
	Data []byte
}

func (ToolDelta) output() {}

// ToolEnd signals the completion of a tool invocation.
type ToolEnd struct {
	ID      string
	Name    string
	Parts   []content.Part
	IsError bool
}

func (ToolEnd) output() {}
