package flow

import (
	"context"
	"testing"

	blades "github.com/go-kratos/blades"
)

// stubRunner is a simple blades.Runner that returns its name as an assistant message.
type stubRunner struct{ name string }

func (s *stubRunner) Run(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (*blades.Generation, error) {
	msg := blades.AssistantMessage("node:" + s.name)
	return &blades.Generation{Messages: []*blades.Message{msg}}, nil
}

func (s *stubRunner) RunStream(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (blades.Streamer[*blades.Generation], error) {
	pipe := blades.NewStreamPipe[*blades.Generation]()
	pipe.Go(func() error {
		msg := blades.AssistantMessage("node:" + s.name)
		pipe.Send(&blades.Generation{Messages: []*blades.Message{msg}})
		return nil
	})
	return pipe, nil
}

func TestGraphLinearRun(t *testing.T) {
	g := NewGraph()
	g.AddStart("A", &stubRunner{name: "A"})
	g.AddNode("B", &stubRunner{name: "B"})
	g.AddEnd("C", &stubRunner{name: "C"})
	g.AddEdge("A", "B")
	g.AddEdge("B", "C")

	ctx := context.Background()
	prompt := blades.NewPrompt(blades.UserMessage("start"))

	last, err := g.Run(ctx, prompt)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if got := last.AsText(); got != "node:C" {
		t.Fatalf("want last node C, got %s", got)
	}

	stream, err := g.RunStream(ctx, prompt)
	if err != nil {
		t.Fatalf("RunStream error: %v", err)
	}
	var seq []string
	for stream.Next() {
		gen, err := stream.Current()
		if err != nil {
			t.Fatalf("stream current error: %v", err)
		}
		seq = append(seq, gen.AsText())
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("stream close error: %v", err)
	}
	want := []string{"node:A", "node:B", "node:C"}
	if len(seq) != len(want) {
		t.Fatalf("want %d items, got %d", len(want), len(seq))
	}
	for i := range want {
		if seq[i] != want[i] {
			t.Fatalf("want %v, got %v", want, seq)
		}
	}
}

func TestGraphBranchOverride(t *testing.T) {
	g := NewGraph()
	g.AddStart("A", &stubRunner{name: "A"})
	g.AddNode("B", &stubRunner{name: "B"})
	g.AddEnd("C", &stubRunner{name: "C"})
	g.AddEdge("A", "B")
	g.AddBranch("A", func(ctx context.Context) (string, error) {
		if v, ok := ctx.Value("next").(string); ok {
			return v, nil
		}
		return "", nil
	})

	ctx := context.WithValue(context.Background(), "next", "C")
	prompt := blades.NewPrompt(blades.UserMessage("start"))

	last, err := g.Run(ctx, prompt)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if got := last.AsText(); got != "node:C" {
		t.Fatalf("want last node C, got %s", got)
	}

	stream, err := g.RunStream(ctx, prompt)
	if err != nil {
		t.Fatalf("RunStream error: %v", err)
	}
	var seq []string
	for stream.Next() {
		gen, err := stream.Current()
		if err != nil {
			t.Fatalf("stream current error: %v", err)
		}
		seq = append(seq, gen.AsText())
	}
	_ = stream.Close()
	want := []string{"node:A", "node:C"}
	if len(seq) != len(want) {
		t.Fatalf("want %d items, got %d", len(want), len(seq))
	}
	for i := range want {
		if seq[i] != want[i] {
			t.Fatalf("want %v, got %v", want, seq)
		}
	}
}

func TestGraphCycleDetection(t *testing.T) {
	g := NewGraph()
	g.AddStart("A", &stubRunner{name: "A"})
	g.AddNode("B", &stubRunner{name: "B"})
	g.AddEdge("A", "B")
	g.AddEdge("B", "A")

	_, err := g.Run(context.Background(), blades.NewPrompt())
	if err == nil {
		t.Fatalf("expected cycle detection error, got nil")
	}
}

func TestGraphMissingStart(t *testing.T) {
	g := NewGraph()
	g.AddNode("A", &stubRunner{name: "A"})
	g.AddNode("C", &stubRunner{name: "C"})
	// No edges -> ambiguous start since multiple nodes exist.
	_, err := g.Run(context.Background(), blades.NewPrompt())
	if err == nil {
		t.Fatalf("expected missing start error, got nil")
	}
}

func TestGraphMissingNextNode(t *testing.T) {
	g := NewGraph()
	g.AddStart("A", &stubRunner{name: "A"})
	g.AddEdge("A", "B") // B not registered
	_, err := g.Run(context.Background(), blades.NewPrompt())
	if err == nil {
		t.Fatalf("expected missing next node error, got nil")
	}
}
