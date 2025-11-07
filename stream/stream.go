package stream

import (
	"sync"
)

// Event represents a value or an error emitted by a stream.
type Event[T any] struct {
	Value T
	Err   error
}

// NewEvent creates a new Event that yields the given value without error.
func NewError[T any](err error) Event[T] {
	return Event[T]{Err: err}
}

// NewEvent creates a new Event that yields the given value without error.
func NewEvent[T any](v T) Event[T] {
	return Event[T]{Value: v}
}

// Go runs the given function f in a new goroutine and returns a channel that
// emits values sent by f. The channel is automatically closed when f returns.
func Go[T any](f func(chan Event[T]) error) <-chan Event[T] {
	ch := make(chan Event[T], 8)
	go func() {
		defer close(ch)
		if err := f(ch); err != nil {
			ch <- NewError[T](err)
		}
	}()
	return ch
}

// Just returns a channel that emits the provided values in order and then
// closes the channel.
func Just[T any](values ...T) <-chan Event[T] {
	ch := make(chan Event[T], len(values))
	for _, v := range values {
		ch <- NewEvent(v)
	}
	close(ch)
	return ch
}

// Filter returns a channel that emits only the values from the input channel
// that satisfy the given predicate function.
func Filter[T any](ch <-chan Event[T], predicate func(Event[T]) bool) <-chan Event[T] {
	out := make(chan Event[T], 8)
	go func() {
		defer close(out)
		for v := range ch {
			if predicate(v) {
				out <- v
			}
		}
	}()
	return out
}

// Observe returns a channel that emits the results of applying the given
// observer function to each value from the input channel. The observer function
// can modify the value and return a boolean indicating whether to continue
// observing.
func Observe[T any](ch <-chan Event[T], observer func(Event[T]) error) <-chan Event[T] {
	out := make(chan Event[T], 8)
	go func() {
		draining := false
		for v := range ch {
			if draining {
				continue
			}
			if err := observer(v); err != nil {
				out <- NewError[T](err)
				close(out)
				draining = true
				return
			}
			out <- v
		}
		close(out)
	}()
	return out
}

// Map returns a channel that emits the results of applying the given mapper
// function to each value from the input channel.
func Map[T, R any](ch <-chan Event[T], mapper func(T) (R, error)) <-chan Event[R] {
	out := make(chan Event[R], 8)
	go func() {
		draining := false
		for v := range ch {
			if draining {
				continue
			}
			m, err := mapper(v.Value)
			if err != nil {
				out <- NewError[R](err)
				close(out)
				draining = true
				return
			}
			out <- NewEvent[R](m)
		}
		close(out)
	}()
	return out
}

// Merge takes multiple input channels and merges their outputs into a single
// output channel.
func Merge[T any](chs ...<-chan Event[T]) <-chan Event[T] {
	var (
		wg  sync.WaitGroup
		out = make(chan Event[T], len(chs)*8)
	)
	for _, ch := range chs {
		wg.Add(1)
		go func(c <-chan Event[T]) {
			defer wg.Done()
			for v := range c {
				out <- v
			}
		}(ch)
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}
