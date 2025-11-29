package graph

import "sync"

// State is a concurrency-safe, shared state bag backed by sync.Map.
// All nodes observe and mutate the same instance; cloning is intentionally
// unsupported to keep state globally unique.
type State struct {
	data *sync.Map
}

// NewState constructs an empty State.
func NewState(ms ...map[string]any) State {
	state := State{data: &sync.Map{}}
	for _, m := range ms {
		for k, v := range m {
			state.data.Store(k, v)
		}
	}
	return state
}

// ensure lazily initializes the backing map.
func (s *State) ensure() {
	if s.data == nil {
		s.data = &sync.Map{}
	}
}

// Load retrieves a value.
func (s State) Load(key string) (any, bool) {
	if s.data == nil {
		return nil, false
	}
	return s.data.Load(key)
}

// Store sets a value.
func (s State) Store(key string, value any) {
	s.ensure()
	s.data.Store(key, value)
}

// Delete removes a value.
func (s State) Delete(key string) {
	if s.data == nil {
		return
	}
	s.data.Delete(key)
}

func (s State) Merge(other State) {
	other.data.Range(func(key, value any) bool {
		s.Store(key.(string), value)
		return true
	})
}

// Snapshot copies the contents into a plain map for serialization or testing.
func (s State) Snapshot() map[string]any {
	if s.data == nil {
		return nil
	}
	out := make(map[string]any)
	s.data.Range(func(key, value any) bool {
		if k, ok := key.(string); ok {
			out[k] = value
		}
		return true
	})
	if len(out) == 0 {
		return nil
	}
	return out
}
