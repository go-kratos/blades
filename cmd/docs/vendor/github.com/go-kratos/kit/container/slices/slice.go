package slices

import (
	"encoding/json"
	"sync"
)

// Slice is a thread-safe generic slice-based list.
// It uses RWMutex to ensure safe concurrent reads and writes.
type Slice[T any] struct {
	mu   sync.RWMutex
	data []T
}

// New creates a new Slice with optional initial elements.
func New[T any](items ...T) *Slice[T] {
	d := make([]T, 0, len(items))
	d = append(d, items...)
	return &Slice[T]{data: d}
}

// Len returns the number of elements in the list.
func (l *Slice[T]) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.data)
}

// Clear removes all elements from the list.
func (l *Slice[T]) Clear() {
	l.mu.Lock()
	l.data = l.data[:0]
	l.mu.Unlock()
}

// Append adds items to the end of the list.
func (l *Slice[T]) Append(items ...T) *Slice[T] {
	if len(items) == 0 {
		return l
	}
	l.mu.Lock()
	l.data = append(l.data, items...)
	l.mu.Unlock()
	return l
}

// Get returns the item at index i.
// It returns false if i is out of bounds.
func (l *Slice[T]) Get(i int) (T, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if i < 0 || i >= len(l.data) {
		var zero T
		return zero, false
	}
	return l.data[i], true
}

// Set replaces the item at index i with value.
// It returns false if i is out of bounds.
func (l *Slice[T]) Set(i int, value T) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if i < 0 || i >= len(l.data) {
		return false
	}
	l.data[i] = value
	return true
}

// RemoveAt removes and returns the item at index i.
// It returns false if i is out of bounds.
func (l *Slice[T]) RemoveAt(i int) (T, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if i < 0 || i >= len(l.data) {
		var zero T
		return zero, false
	}
	v := l.data[i]
	l.data = append(l.data[:i], l.data[i+1:]...)
	return v, true
}

// Slice returns a slice of the underlying data from start to end.
func (l *Slice[T]) Slice(start, end int) []T {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.data[start:end]
}

// SliceStart returns a slice of the underlying data from start to the end of the slice.
func (l *Slice[T]) SliceStart(start int) []T {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.data[start:]
}

// SliceEnd returns a slice of the underlying data from the beginning to end.
func (l *Slice[T]) SliceEnd(end int) []T {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.data[:end]
}

// ToSlice returns a copy of the underlying slice.
// Safe for concurrent use.
func (l *Slice[T]) ToSlice() []T {
	l.mu.RLock()
	defer l.mu.RUnlock()
	cpy := make([]T, len(l.data))
	copy(cpy, l.data)
	return cpy
}

// Range iterates over a snapshot of the list.
// The callback receives the index and item. If it returns false, iteration stops.
func (l *Slice[T]) Range(f func(index int, item T) bool) {
	l.mu.RLock()
	cpy := make([]T, len(l.data))
	copy(cpy, l.data)
	l.mu.RUnlock()
	for i, v := range cpy {
		if !f(i, v) {
			break
		}
	}
}

// Clone creates and returns a shallow copy of the list.
func (l *Slice[T]) Clone() *Slice[T] {
	l.mu.RLock()
	defer l.mu.RUnlock()
	c := make([]T, len(l.data))
	copy(c, l.data)
	return &Slice[T]{data: c}
}

// MarshalJSON implements the json.Marshaler interface.
func (l *Slice[T]) MarshalJSON() ([]byte, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return json.Marshal(l.data)
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (l *Slice[T]) UnmarshalJSON(b []byte) error {
	var data []T
	if err := json.Unmarshal(b, &data); err != nil {
		return err
	}
	l.mu.Lock()
	l.data = data
	l.mu.Unlock()
	return nil
}
