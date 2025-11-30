package graph

import "maps"

// State represents a collection of key-value pairs that can be used to store
type State map[string]any

func (s State) Clone() State {
	if s == nil {
		return State{}
	}
	return maps.Clone(s)
}
