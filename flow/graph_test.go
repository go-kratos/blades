package flow

import (
	"context"
	"testing"

	"github.com/go-kratos/blades"
)

// runnerStub is a minimal blades.Runner used for tests.
type runnerStub struct{ name string }

func (r *runnerStub) Name() string { return r.name }
func (r *runnerStub) Run(ctx context.Context, in *blades.Prompt, opts ...blades.ModelOption) (*blades.Message, error) {
	return &blades.Message{}, nil
}
func (r *runnerStub) RunStream(ctx context.Context, in *blades.Prompt, opts ...blades.ModelOption) (blades.Streamable[*blades.Message], error) {
	pipe := blades.NewStreamPipe[*blades.Message]()
	pipe.Go(func() error {
		pipe.Send(&blades.Message{})
		return nil
	})
	return pipe, nil
}

func TestGraph_LinearChain(t *testing.T) {
	a := &runnerStub{name: "A"}
	b := &runnerStub{name: "B"}
	c := &runnerStub{name: "C"}

	g := NewGraph("test")
	if err := g.AddNode(a); err != nil {
		t.Fatalf("AddNode A error: %v", err)
	}
	if err := g.AddNode(b); err != nil {
		t.Fatalf("AddNode B error: %v", err)
	}
	if err := g.AddNode(c); err != nil {
		t.Fatalf("AddNode C error: %v", err)
	}
	if err := g.AddStart(a); err != nil {
		t.Fatalf("AddStart error: %v", err)
	}
	if err := g.AddEdge(a, b); err != nil {
		t.Fatalf("AddEdge A->B error: %v", err)
	}
	if err := g.AddEdge(b, c); err != nil {
		t.Fatalf("AddEdge B->C error: %v", err)
	}

	runner, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	gr, ok := runner.(*graphRunner)
	if !ok {
		t.Fatalf("expected *graphRunner, got %T", runner)
	}
	queue, ok := gr.compiled["A"]
	if !ok {
		t.Fatalf("compiled missing start A")
	}
	if len(queue) != 3 {
		t.Fatalf("expected 3 nodes in compiled queue, got %d", len(queue))
	}
	want := []string{"A", "B", "C"}
	for i, n := range queue {
		if n.name != want[i] {
			t.Fatalf("at position %d want %s, got %s", i, want[i], n.name)
		}
	}
}
