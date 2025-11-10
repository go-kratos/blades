package memory

import (
	"context"

	"github.com/go-kratos/blades/tools"
)

// Request is the request for the memory tool.
type Request struct {
	Query string `json:"query" jsonschema:"The query to search the memory."`
}

// Response is the response for the memory tool.
type Response struct {
	Memories []*Memory `json:"memories" jsonschema:"The memories found for the query."`
}

// NewMemoryTool creates a new memory tool with the given memory store.
func NewMemoryTool(store MemoryStore) (tools.Tool, error) {
	return tools.NewFunc[Request, Response](
		"Memory",
		"You have memory. You can use it to answer questions. If any questions need you to look up the memory.",
		tools.HandleFunc[Request, Response](func(ctx context.Context, req Request) (Response, error) {
			memories, err := store.SearchMemory(ctx, req.Query)
			if err != nil {
				return Response{}, err
			}
			return Response{Memories: memories}, nil
		}),
	)
}
