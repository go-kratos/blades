package stream

// Go runs the given function f in a new goroutine and returns a channel that
// emits values sent by f. The channel is automatically closed when f returns.
func Go[T any](f func(chan T)) <-chan T {
	ch := make(chan T, 8)
	go func() {
		defer close(ch)
		f(ch)
	}()
	return ch
}

// Just returns a channel that emits the provided values in order and then
// closes the channel.
func Just[T any](values ...T) <-chan T {
	ch := make(chan T, len(values))
	for _, v := range values {
		ch <- v
	}
	close(ch)
	return ch
}

// Filter returns a channel that emits only the values from the input channel
// that satisfy the given predicate function.
func Filter[T any](ch <-chan T, predicate func(T) bool) <-chan T {
	out := make(chan T)
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
func Observe[T any](ch <-chan T, observer func(T) (T, bool)) <-chan T {
	out := make(chan T)
	go func() {
		defer close(out)
		for v := range ch {
			r, ok := observer(v)
			if !ok {
				go func() {
					for range ch {
						// drain remaining values
					}
				}()
				return
			}
			out <- r
		}
	}()
	return out
}

// Map returns a channel that emits the results of applying the given mapper
// function to each value from the input channel.
func Map[T, R any](ch <-chan T, mapper func(T) R) <-chan R {
	out := make(chan R)
	go func() {
		defer close(out)
		for v := range ch {
			out <- mapper(v)
		}
	}()
	return out
}

// Merge takes multiple input channels and merges their outputs into a single
// output channel.
func Merge[T any](chs ...<-chan T) <-chan T {
	out := make(chan T)
	go func() {
		defer close(out)
		for _, ch := range chs {
			for v := range ch {
				out <- v
			}
		}
	}()
	return out
}
