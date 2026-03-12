package memory

import (
	"context"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/tools"
)

// Memory represents a piece of information stored in the memory system.
type Memory struct {
	Content  *blades.Message `json:"content"`
	Metadata map[string]any  `json:"metadata,omitempty"`
}

// MemoryStore defines the interface for storing and retrieving memories.
type MemoryStore interface {
	AddMemory(context.Context, *Memory) error
	SaveSession(context.Context, blades.Session) error
	SearchMemory(context.Context, string) ([]*Memory, error)
}

// ToolRequest is the request for the memory tool.
type ToolRequest struct {
	Query string `json:"query" jsonschema:"The query to search the memory."`
}

// ToolResponse is the response for the memory tool.
type ToolResponse struct {
	Memories []*Memory `json:"memories" jsonschema:"The memories found for the query."`
}

// NewMemoryTool creates a new memory tool with the given memory store.
func NewMemoryTool(store MemoryStore) (tools.Tool, error) {
	return tools.NewFunc[ToolRequest, ToolResponse](
		"Memory",
		"You have memory. You can use it to answer questions. If any questions need you to look up the memory.",
		func(ctx context.Context, req ToolRequest) (ToolResponse, error) {
			memories, err := store.SearchMemory(ctx, req.Query)
			if err != nil {
				return ToolResponse{}, err
			}
			return ToolResponse{Memories: memories}, nil
		},
	)
}
