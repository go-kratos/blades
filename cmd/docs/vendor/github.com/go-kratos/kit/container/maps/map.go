package maps

import (
	"encoding/json"
	"sync"
)

// Map is a concurrent map with generic key and value types.
type Map[K comparable, V any] struct {
	m sync.Map
}

// New creates and returns a new Map instance.
func New[K comparable, V any](ms ...map[K]V) *Map[K, V] {
	m := &Map[K, V]{}
	for _, n := range ms {
		for k, v := range n {
			m.Store(k, v)
		}
	}
	return m
}

// Clear removes all entries from the map.
func (m *Map[K, V]) Clear() {
	m.m.Clear()
}

// CompareAndDelete deletes the entry for a key only if it is currently mapped to a given value.
func (m *Map[K, V]) CompareAndDelete(key K, value V) (deleted bool) {
	return m.m.CompareAndDelete(key, value)
}

// CompareAndSwap swaps the entry for a key only if it is currently mapped to a given value.
func (m *Map[K, V]) CompareAndSwap(key, old, new any) (swapped bool) {
	return m.m.CompareAndSwap(key, old, new)
}

// Delete removes the value for a given key.
func (m *Map[K, V]) Delete(key K) {
	m.m.Delete(key)
}

// Load retrieves the value for a given key.
func (m *Map[K, V]) Load(key K) (V, bool) {
	value, ok := m.m.Load(key)
	if !ok {
		var zero V
		return zero, false
	}
	return value.(V), true
}

// LoadAndDelete retrieves and deletes the value for a given key.
func (m *Map[K, V]) LoadAndDelete(key K) (value V, loaded bool) {
	loadedValue, ok := m.m.LoadAndDelete(key)
	if !ok {
		var zero V
		return zero, false
	}
	return loadedValue.(V), true
}

// LoadOrStore retrieves the existing value for a key or stores and returns the given value if the key is not present.
func (m *Map[K, V]) LoadOrStore(key K, value V) (V, bool) {
	loaded, ok := m.m.LoadOrStore(key, value)
	if !ok {
		return value, false
	}
	return loaded.(V), true
}

// Range iterates over all key-value pairs in the map.
func (m *Map[K, V]) Range(f func(key K, value V) bool) {
	m.m.Range(func(key, value any) bool {
		return f(key.(K), value.(V))
	})
}

// Store sets the value for a given key.
func (m *Map[K, V]) Store(key K, value V) {
	m.m.Store(key, value)
}

// Swap sets the value for a key and returns the previous value and whether it was present.
func (m *Map[K, V]) Swap(key, value any) (previous any, loaded bool) {
	return m.m.Swap(key, value)
}

// Clone creates and returns a shallow copy of the map as a standard map.
func (m *Map[K, V]) ToMap() map[K]V {
	clone := make(map[K]V)
	m.Range(func(key K, value V) bool {
		clone[key] = value
		return true
	})
	return clone
}

// ToKeys returns a slice of all keys in the map.
func (m *Map[K, V]) ToKeys() []K {
	keys := make([]K, 0)
	m.Range(func(key K, _ V) bool {
		keys = append(keys, key)
		return true
	})
	return keys
}

// ToValues returns a slice of all values in the map.
func (m *Map[K, V]) ToValues() []V {
	values := make([]V, 0)
	m.Range(func(_ K, value V) bool {
		values = append(values, value)
		return true
	})
	return values
}

// Clone creates and returns a shallow copy of the Map.
func (m *Map[K, V]) Clone() *Map[K, V] {
	clone := New[K, V]()
	m.Range(func(key K, value V) bool {
		clone.Store(key, value)
		return true
	})
	return clone
}

// MarshalJSON implements the json.Marshaler interface for the Map type.
func (m *Map[K, V]) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.ToMap())
}

// UnmarshalJSON implements the json.Unmarshaler interface for the Map type.
func (m *Map[K, V]) UnmarshalJSON(data []byte) error {
	tmp := make(map[K]V)
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	for k, v := range tmp {
		m.Store(k, v)
	}
	return nil
}
