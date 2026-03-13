package blades

import (
	"maps"
)

// State holds arbitrary key-value pairs representing the state.
type State map[string]any

// Clone creates a deep copy of the State.
func (s State) Clone() State {
	return State(maps.Clone(map[string]any(s)))
}
