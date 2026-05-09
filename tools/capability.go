package tools

import (
	"context"
	"encoding/json"
	"iter"
)

// ReadOnlyTool marks a tool as having no side effects.
type ReadOnlyTool interface {
	Tool
	ReadOnly() bool
}

// DestructiveTool marks a tool as potentially irreversible.
type DestructiveTool interface {
	Tool
	Destructive() bool
}

// StreamingTool supports incremental result streaming.
type StreamingTool interface {
	Tool
	Stream(ctx context.Context, input json.RawMessage) iter.Seq2[*Result, error]
}
