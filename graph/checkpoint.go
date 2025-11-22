package graph

// Checkpoint captures the execution progress of a Task so it can be resumed.
// All maps/slices are shallow copies of the underlying state at the moment the
// checkpoint was taken.
type Checkpoint struct {
	Ready         []string
	Remaining     map[string]int
	Contributions map[string]map[string]State
	Received      map[string]int
	Visited       map[string]bool
	Finished      bool
	FinishState   State
}

// Clone returns a deep copy of the checkpoint so callers can modify it without
// affecting the original snapshot.
func (c Checkpoint) Clone() Checkpoint {
	return Checkpoint{
		Ready:         cloneStringSlice(c.Ready),
		Remaining:     cloneIntMap(c.Remaining),
		Contributions: cloneContributions(c.Contributions),
		Received:      cloneIntMap(c.Received),
		Visited:       cloneBoolMap(c.Visited),
		Finished:      c.Finished,
		FinishState:   c.FinishState.Clone(),
	}
}

func cloneStringSlice(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	out := make([]string, len(input))
	copy(out, input)
	return out
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

func cloneContributions(input map[string]map[string]State) map[string]map[string]State {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]map[string]State, len(input))
	for node, parents := range input {
		if len(parents) == 0 {
			continue
		}
		clonedParents := make(map[string]State, len(parents))
		for parent, state := range parents {
			clonedParents[parent] = state.Clone()
		}
		out[node] = clonedParents
	}
	return out
}
