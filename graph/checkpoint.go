package graph

import (
	"encoding/json"
	"maps"
)

// Checkpointer persists and restores checkpoints for a task identified by taskID.
// Save and Resume must be safe for concurrent use.
type Checkpointer interface {
	Save(taskID string, cp Checkpoint) error
	Resume(taskID string) (Checkpoint, bool, error) // bool indicates whether a checkpoint was found
}

// CheckpointerFuncs adapts functions to the Checkpointer interface.
type CheckpointerFuncs struct {
	SaveFunc   func(taskID string, cp Checkpoint) error
	ResumeFunc func(taskID string) (Checkpoint, bool, error)
}

func (c CheckpointerFuncs) Save(taskID string, cp Checkpoint) error {
	if c.SaveFunc == nil {
		return nil
	}
	return c.SaveFunc(taskID, cp)
}

func (c CheckpointerFuncs) Resume(taskID string) (Checkpoint, bool, error) {
	if c.ResumeFunc == nil {
		return Checkpoint{}, false, nil
	}
	return c.ResumeFunc(taskID)
}

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
