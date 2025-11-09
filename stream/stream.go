package stream

import (
	"sync"

	"github.com/go-kratos/blades"
)

// Just returns a blades.Sequence that emits the provided values in order.
func Just[T any](values ...T) blades.Sequence[T, error] {
	return func(yield func(T, error) bool) {
		for _, v := range values {
			if !yield(v, nil) {
				return
			}
		}
	}
}

// Filter returns a blades.Sequence that emits only the values from the input stream
// that satisfy the given predicate function.
func Filter[T any](stream blades.Sequence[T, error], predicate func(T) bool) blades.Sequence[T, error] {
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
func Observe[T any](stream blades.Sequence[T, error], observer func(T, error) error) blades.Sequence[T, error] {
	return func(yield func(T, error) bool) {
		stream(func(v T, err error) bool {
			if err := observer(v, err); err != nil {
				return yield(v, err)
			}
			return yield(v, err)
		})
	}
}

// Map returns a blades.Sequence that emits the results of applying the given mapper
// function to each value from the input stream.
func Map[T, R any](stream blades.Sequence[T, error], mapper func(T) (R, error)) blades.Sequence[R, error] {
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
func Merge[T any](streams ...blades.Sequence[T, error]) blades.Sequence[T, error] {
	return func(yield func(T, error) bool) {
		var (
			mu sync.Mutex
			wg sync.WaitGroup
		)
		wg.Add(len(streams))
		for _, stream := range streams {
			go func(next blades.Sequence[T, error]) {
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
