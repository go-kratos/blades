package blades

import "sync/atomic"

// MappedStream maps the output of one Streamer to another type.
type MappedStream[M any, T any] struct {
	stream   Streamable[M]
	transfer func(M) (T, error)
}

// NewMappedStream creates a new MappedStream.
func NewMappedStream[M any, T any](stream Streamable[M], transfer func(M) (T, error)) *MappedStream[M, T] {
	return &MappedStream[M, T]{stream: stream, transfer: transfer}
}

// Next advances the stream to the next item.
func (ws *MappedStream[M, T]) Next() bool {
	return ws.stream.Next()
}

// Current returns the current item in the stream, mapped to the target type.
func (ws *MappedStream[M, T]) Current() (T, error) {
	m, err := ws.stream.Current()
	if err != nil {
		return *new(T), err
	}
	return ws.transfer(m)
}

// Close closes the underlying stream.
func (ws *MappedStream[M, T]) Close() error {
	return ws.stream.Close()
}

// StreamPipe directs the yielding of values.
type StreamPipe[T any] struct {
	err    error
	closed atomic.Bool
	queue  chan T
	next   T
}

// NewStreamPipe creates a new StreamPipe director.
func NewStreamPipe[T any]() *StreamPipe[T] {
	return &StreamPipe[T]{
		queue: make(chan T, 8),
	}
}

func (d *StreamPipe[T]) Send(v T) {
	d.queue <- v
}

// Next returns true if there is a value to yield.
func (d *StreamPipe[T]) Next() bool {
	v, ok := <-d.queue
	if !ok {
		return false
	}
	d.next = v
	return true
}

// Current returns the value and marks it as yielded.
func (d *StreamPipe[T]) Current() (T, error) {
	return d.next, d.err
}

// Go runs the provided function in a goroutine, closing the StreamPipe when done.
func (d *StreamPipe[T]) Go(fn func() error) {
	go func() {
		defer d.Close()
		d.err = fn()
	}()
}

// Close closes the StreamPipe.
func (d *StreamPipe[T]) Close() error {
	if d.closed.Swap(true) {
		return nil
	}
	close(d.queue)
	return nil
}
