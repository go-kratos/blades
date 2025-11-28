package graph

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
		Received: cloneIntMap(c.Received),
		Visited:  cloneBoolMap(c.Visited),
		State:    cloneAnyMap(c.State),
	}
}

func cloneIntMap(input map[string]int) map[string]int {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]int, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}

func cloneBoolMap(input map[string]bool) map[string]bool {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]bool, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}

func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}
