package memory

import (
	"context"

	"github.com/go-kratos/blades"
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
