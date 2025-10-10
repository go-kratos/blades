package flow

import (
	"context"
	"testing"

	"github.com/go-kratos/blades"
)

// runnerStub is a minimal blades.Runner used for tests.
type runnerStub[I, O, Option any] struct {
	run func(context.Context, I, ...Option) (O, error)
}

func (r *runnerStub[I, O, Option]) Run(ctx context.Context, in I, opts ...Option) (O, error) {
	return r.run(ctx, in, opts...)
}

func (r *runnerStub[I, O, Option]) RunStream(ctx context.Context, in I, opts ...Option) (blades.Streamer[O], error) {
	pipe := blades.NewStreamPipe[O]()
	pipe.Go(func() error {
		out, err := r.run(ctx, in, opts...)
		if err != nil {
			return err
		}
		pipe.Send(out)
		return nil
	})
	return pipe, nil
}

func TestGraph_LinearChain(t *testing.T) {
	// Each node adds a fixed number to the input
	add := func(n int) *runnerStub[int, int, struct{}] {
		return &runnerStub[int, int, struct{}]{
			run: func(ctx context.Context, in int, _ ...struct{}) (int, error) {
				return in + n, nil
			},
		}
	}
	state := func(ctx context.Context, out int) (int, error) { return out, nil }

	g := NewGraph[int, int, struct{}]()
	g.AddNode("A", add(1))
	g.AddNode("B", add(2))
	g.AddNode("C", add(3))
	g.AddStart("A")
	g.AddEdge("A", "B", state)
	g.AddEdge("B", "C", state)
	g.AddEnd("C")

	runner, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	got, err := runner.Run(context.Background(), 10)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	want := 10 + 1 + 2 + 3
	if got != want {
		t.Fatalf("want %d, got %d", want, got)
	}
}
