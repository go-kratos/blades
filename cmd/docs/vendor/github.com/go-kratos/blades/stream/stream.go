package stream

import (
	"iter"
	"sync"
)

// Just returns a iter.Seq2 that emits the provided values in order.
func Just[T any](values ...T) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		for _, v := range values {
			if !yield(v, nil) {
				return
			}
		}
	}
}

// Error returns a iter.Seq2 that emits the provided error.
func Error[T any](err error) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		yield(*new(T), err)
	}
}

// Filter returns a iter.Seq2 that emits only the values from the input stream
// that satisfy the given predicate function.
func Filter[T any](stream iter.Seq2[T, error], predicate func(T) bool) iter.Seq2[T, error] {
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
func Observe[T any](stream iter.Seq2[T, error], observer func(T, error) error) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		stream(func(v T, err error) bool {
			if err := observer(v, err); err != nil {
				return yield(v, err)
			}
			return yield(v, err)
		})
	}
}

// Map returns a iter.Seq2 that emits the results of applying the given mapper
// function to each value from the input stream.
func Map[T, R any](stream iter.Seq2[T, error], mapper func(T) (R, error)) iter.Seq2[R, error] {
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

// Merge takes multiple input streams (as iter.Seq2) and merges their outputs into a single
// output stream.
func Merge[T any](streams ...iter.Seq2[T, error]) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		var (
			mu sync.Mutex
			wg sync.WaitGroup
		)
		wg.Add(len(streams))
		for _, stream := range streams {
			go func(next iter.Seq2[T, error]) {
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
