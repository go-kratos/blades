package graph

import (
	"context"
	"maps"
)

// Checkpointer persists and restores checkpoints for a task identified by checkpointID.
// Save and Resume must be safe for concurrent use.
type Checkpointer interface {
	Save(ctx context.Context, checkpoint *Checkpoint) error
	Resume(ctx context.Context, checkpointID string) (*Checkpoint, error)
}

// Checkpoint captures the execution progress of a Task so it can be resumed.
// Use Clone() to create a deep copy if you need to modify the checkpoint.
type Checkpoint struct {
	ID       string          `json:"id"`
	Received map[string]int  `json:"received"`
	Visited  map[string]bool `json:"visited"`
	State    map[string]any  `json:"state"`
}

// Clone returns a deep copy of the checkpoint so callers can modify it without
// affecting the original snapshot.
func (c *Checkpoint) Clone() *Checkpoint {
	return &Checkpoint{
		ID:       c.ID,
		Received: maps.Clone(c.Received),
		Visited:  maps.Clone(c.Visited),
		State:    maps.Clone(c.State),
	}
}
