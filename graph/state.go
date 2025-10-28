package graph

import "maps"

// State represents the mutable data that flows through the graph.
// It is implemented as a map of string keys to arbitrary values.
// Handlers should treat State as immutable and always return a cloned instance.
type State map[string]any

// Clone performs a shallow copy using maps.Clone so callers can mutate without
// affecting the original map (nested references are shared intentionally).
func (s State) Clone() State {
	if s == nil {
		return State{}
	}
	return State(maps.Clone(map[string]any(s)))
}
