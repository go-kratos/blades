package graph

import (
	"encoding/json"
	"maps"
)

// CheckpointSaver persists checkpoints during execution.
type CheckpointSaver interface {
	Save(Checkpoint)
}

// CheckpointSaverFunc adapts a function to the CheckpointSaver interface.
type CheckpointSaverFunc func(Checkpoint)

func (f CheckpointSaverFunc) Save(cp Checkpoint) { f(cp) }

// Checkpoint captures the execution progress of a Task so it can be resumed.
// Use Clone() to create a deep copy if you need to modify the checkpoint.
type Checkpoint struct {
	Received map[string]int
	Visited  map[string]bool
	State    map[string]any
}

// Clone returns a deep copy of the checkpoint so callers can modify it without
// affecting the original snapshot.
func (c Checkpoint) Clone() Checkpoint {
	return Checkpoint{
		Received: maps.Clone(c.Received),
		Visited:  maps.Clone(c.Visited),
		State:    maps.Clone(c.State),
	}
}

// Marshal serializes the checkpoint as JSON.
func (c Checkpoint) Marshal() ([]byte, error) {
	return json.Marshal(c)
}

// Unmarshal populates the checkpoint from JSON.
func (c *Checkpoint) Unmarshal(data []byte) error {
	return json.Unmarshal(data, c)
}
