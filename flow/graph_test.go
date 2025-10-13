package flow

import (
	"context"
	"testing"

	"github.com/go-kratos/blades"
)

// runnerStub is a minimal blades.Runner used for tests.
type runnerStub[I, O, Option any] struct {
	name string
	run  func(context.Context, I, ...Option) (O, error)
}

func (r *runnerStub[I, O, Option]) Name() string {
	return r.name
}

func (r *runnerStub[I, O, Option]) Run(ctx context.Context, in I, opts ...Option) (O, error) {
	return r.run(ctx, in, opts...)
}

func (r *runnerStub[I, O, Option]) RunStream(ctx context.Context, in I, opts ...Option) (blades.Streamable[O], error) {
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
	add := func(name string, n int) *runnerStub[int, int, struct{}] {
		return &runnerStub[int, int, struct{}]{
			name: name,
			run: func(ctx context.Context, in int, _ ...struct{}) (int, error) {
				return in + n, nil
			},
		}
	}

	transition := func(ctx context.Context, transition Transition, output int) (int, error) {
		return output, nil
	}

	a := add("A", 1)
	b := add("B", 2)
	c := add("C", 3)

	g := NewGraph[int, int, struct{}]("test", transition)
	g.AddNode(a)
	g.AddNode(b)
	g.AddNode(c)
	g.AddStart(a)
	g.AddEdge(a, b)
	g.AddEdge(b, c)

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
