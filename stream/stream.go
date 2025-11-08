package stream

import (
	"iter"
	"sync"
)

// Streamable represents an iterator sequence that yields values of type T along with potential errors.
type Streamable[T any] iter.Seq2[T, error]

// Just returns a Streamable that emits the provided values in order.
func Just[T any](values ...T) Streamable[T] {
	return func(yield func(T, error) bool) {
		for _, v := range values {
			if !yield(v, nil) {
				return
			}
		}
	}
}

// Filter returns a Streamable that emits only the values from the input stream
// that satisfy the given predicate function.
func Filter[T any](stream Streamable[T], predicate func(T) bool) Streamable[T] {
	return func(yield func(T, error) bool) {
		stream(func(v T, err error) bool {
			if err != nil {
				return yield(*new(T), err)
			}
			if predicate(v) {
				return yield(v, nil)
			}
			return true
		})
	}
}

// Observe returns a channel that emits the results of applying the given
// observer function to each value from the input channel. The observer function
// is called for each value and returns an error; if a non-nil error is returned,
// observation stops and the error is emitted.
func Observe[T any](stream Streamable[T], observer func(T) error) Streamable[T] {
	return func(yield func(T, error) bool) {
		stream(func(v T, err error) bool {
			if err != nil {
				return yield(*new(T), err)
			}
			if err := observer(v); err != nil {
				return yield(*new(T), err)
			}
			return yield(v, nil)
		})
	}
}

// Map returns a Streamable that emits the results of applying the given mapper
// function to each value from the input stream.
func Map[T, R any](stream Streamable[T], mapper func(T) (R, error)) Streamable[R] {
	return func(yield func(R, error) bool) {
		stream(func(v T, err error) bool {
			if err != nil {
				return yield(*new(R), err)
			}
			mapped, err := mapper(v)
			if err != nil {
				return yield(*new(R), err)
			}
			return yield(mapped, nil)
		})
	}
}

// Merge takes multiple input channels and merges their outputs into a single
// output channel.
func Merge[T any](streams ...Streamable[T]) Streamable[T] {
	return func(yield func(T, error) bool) {
		var (
			mu sync.Mutex
			wg sync.WaitGroup
		)
		wg.Add(len(streams))
		for _, stream := range streams {
			go func(next Streamable[T]) {
				defer wg.Done()
				next(func(v T, err error) bool {
					mu.Lock()
					defer mu.Unlock()
					return yield(v, err)
				})
			}(stream)
		}
		wg.Wait()
	}
}
