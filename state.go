package blades

import (
	"sync"
)

// State is a thread-safe map for storing state information.
type State struct {
	m sync.Map
}

// Load retrieves the value for a key.
func (s *State) Load(key string) (value any, ok bool) {
	return s.m.Load(key)
}

// Store sets the value for a key.
func (s *State) Store(key string, value any) {
	s.m.Store(key, value)
}

// Clone returns a copy of the state as a standard map.
func (s *State) Clone() map[string]any {
	values := make(map[string]any)
	s.m.Range(func(key, value any) bool {
		if k, ok := key.(string); ok {
			values[k] = value
		}
		return true
	})
	return values
}
