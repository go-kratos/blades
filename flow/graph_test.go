package flow

import (
	"context"
	"testing"

	"github.com/go-kratos/blades"
)

// testRunner is a simple Runner used for testing Graph execution.
type testRunner struct {
	name string
	seen []*blades.Prompt
}

func (r *testRunner) Run(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (*blades.Generation, error) {
	// record a copy of the prompt for assertions
	dup := blades.NewPrompt(prompt.Messages...)
	r.seen = append(r.seen, dup)
	return &blades.Generation{Messages: []*blades.Message{
		blades.AssistantMessage("node:" + r.name),
	}}, nil
}

func (r *testRunner) RunStream(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (blades.Streamer[*blades.Generation], error) {
	gen, err := r.Run(ctx, prompt, opts...)
	if err != nil {
		return nil, err
	}
	pipe := blades.NewStreamPipe[*blades.Generation]()
	pipe.Send(gen)
	pipe.Close()
	return pipe, nil
}

func TestGraph_Linear(t *testing.T) {
	// nodes
	a := &testRunner{name: "A"}
	b := &testRunner{name: "B"}
	c := &testRunner{name: "C"}

	g := NewGraph()
	g.AddNode(a)
	g.AddNode(b)
	g.AddNode(c)
	g.AddEdgeStart(a)
	g.AddEdge(a, b)
	g.AddEdge(b, c)
	g.AddEdgeEnd(c)

	runner, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	// run
	prompt := blades.NewPrompt(blades.UserMessage("start"))
	got, err := runner.Run(context.Background(), prompt)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got.AsText() != "node:C" {
		t.Fatalf("unexpected final generation: %q", got.AsText())
	}

	// ensure prompt accumulation for downstream nodes
	if len(b.seen) == 0 || len(c.seen) == 0 {
		t.Fatalf("expected downstream prompts to be observed")
	}
	if s := b.seen[0].String(); s == "" || !contains(s, "node:A") {
		t.Fatalf("expected B prompt to contain A output; got %q", s)
	}
	if s := c.seen[0].String(); s == "" || !contains(s, "node:B") {
		t.Fatalf("expected C prompt to contain A and B outputs; got %q", s)
	}
}

func TestGraph_Branch(t *testing.T) {
	a := &testRunner{name: "A"}
	b := &testRunner{name: "B"}
	left := &testRunner{name: "L"}
	right := &testRunner{name: "R"}

	g := NewGraph()
	g.AddNode(a)
	g.AddNode(b)
	g.AddNode(left)
	g.AddNode(right)
	g.AddEdgeStart(a)
	g.AddEdge(a, b)
	// default would go right, but branch chooses left
	g.AddEdge(b, right)
	g.AddBranch(b, func(ctx context.Context) (string, error) { return "left", nil }, map[string]blades.Runner{
		"left":  left,
		"right": right,
	})
	g.AddEdgeEnd(left)

	runner, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	prompt := blades.NewPrompt(blades.UserMessage("branch"))
	got, err := runner.Run(context.Background(), prompt)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got.AsText() != "node:L" {
		t.Fatalf("expected to end at left branch; got %q", got.AsText())
	}
	if len(right.seen) != 0 {
		t.Fatalf("right branch should not execute")
	}
}

func TestGraph_CycleDetection(t *testing.T) {
	a := &testRunner{name: "A"}
	b := &testRunner{name: "B"}
	g := NewGraph()
	g.AddNode(a)
	g.AddNode(b)
	g.AddEdgeStart(a)
	g.AddEdge(a, b)
	g.AddEdge(b, a) // cycle
	if _, err := g.Compile(); err == nil {
		t.Fatalf("expected cycle detection error, got nil")
	}
}

// contains reports whether substr is in s.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || indexOf(s, substr) >= 0)
}

// indexOf is a minimal substring search to avoid importing strings in this test file.
func indexOf(s, sep string) int {
	n := len(sep)
	if n == 0 {
		return 0
	}
	for i := 0; i+n <= len(s); i++ {
		match := true
		for j := 0; j < n; j++ {
			if s[i+j] != sep[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
