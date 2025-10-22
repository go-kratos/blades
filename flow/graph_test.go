package flow

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// appendHandler returns a handler that appends its node name to the state slice.
func appendHandler(name string) GraphHandler[[]string] {
	return func(ctx context.Context, state []string) ([]string, error) {
		return append(state, name), nil
	}
}

// errorHandler returns a handler that returns an error.
func errorHandler(_ string) GraphHandler[[]string] {
	return func(ctx context.Context, state []string) ([]string, error) {
		return state, fmt.Errorf("handler error")
	}
}

func TestGraphCompile_Validation(t *testing.T) {
	t.Run("missing entry", func(t *testing.T) {
		g := NewGraph[[]string]()
		_ = g.AddNode("A", appendHandler("A"))
		_ = g.SetFinishPoint("A")
		if _, err := g.Compile(); err == nil || !strings.Contains(err.Error(), "entry point not set") {
			t.Fatalf("expected missing entry error, got %v", err)
		}
	})

	t.Run("missing finish", func(t *testing.T) {
		g := NewGraph[[]string]()
		_ = g.AddNode("A", appendHandler("A"))
		_ = g.SetEntryPoint("A")
		if _, err := g.Compile(); err == nil || !strings.Contains(err.Error(), "finish point not set") {
			t.Fatalf("expected missing finish error, got %v", err)
		}
	})

	t.Run("start node not found", func(t *testing.T) {
		g := NewGraph[[]string]()
		_ = g.AddNode("A", appendHandler("A"))
		_ = g.SetEntryPoint("X")
		_ = g.SetFinishPoint("A")
		if _, err := g.Compile(); err == nil || !strings.Contains(err.Error(), "start node not found") {
			t.Fatalf("expected start node not found error, got %v", err)
		}
	})

	t.Run("end node not found", func(t *testing.T) {
		g := NewGraph[[]string]()
		_ = g.AddNode("A", appendHandler("A"))
		_ = g.SetEntryPoint("A")
		_ = g.SetFinishPoint("X")
		if _, err := g.Compile(); err == nil || !strings.Contains(err.Error(), "end node not found") {
			t.Fatalf("expected end node not found error, got %v", err)
		}
	})

	t.Run("edge from unknown node", func(t *testing.T) {
		g := NewGraph[[]string]()
		_ = g.AddNode("A", appendHandler("A"))
		_ = g.AddEdge("X", "A")
		_ = g.SetEntryPoint("A")
		_ = g.SetFinishPoint("A")
		if _, err := g.Compile(); err == nil || !strings.Contains(err.Error(), "edge from unknown node") {
			t.Fatalf("expected edge from unknown node error, got %v", err)
		}
	})

	t.Run("edge to unknown node", func(t *testing.T) {
		g := NewGraph[[]string]()
		_ = g.AddNode("A", appendHandler("A"))
		_ = g.AddEdge("A", "X")
		_ = g.SetEntryPoint("A")
		_ = g.SetFinishPoint("A")
		if _, err := g.Compile(); err == nil || !strings.Contains(err.Error(), "edge to unknown node") {
			t.Fatalf("expected edge to unknown node error, got %v", err)
		}
	})

	t.Run("cycle detection", func(t *testing.T) {
		g := NewGraph[[]string]()
		_ = g.AddNode("A", appendHandler("A"))
		_ = g.AddNode("B", appendHandler("B"))
		_ = g.AddNode("C", appendHandler("C"))
		_ = g.AddEdge("A", "B")
		_ = g.AddEdge("B", "C")
		_ = g.AddEdge("C", "A")
		_ = g.SetEntryPoint("A")
		_ = g.SetFinishPoint("C")
		if _, err := g.Compile(); err == nil || !strings.Contains(err.Error(), "cycle detected") {
			t.Fatalf("expected cycle detected error, got %v", err)
		}
	})
}

func TestGraph_Run_BFSOrder(t *testing.T) {
	g := NewGraph[[]string]()
	_ = g.AddNode("A", appendHandler("A"))
	_ = g.AddNode("B", appendHandler("B"))
	_ = g.AddNode("C", appendHandler("C"))
	_ = g.AddNode("D", appendHandler("D"))
	_ = g.AddEdge("A", "B")
	_ = g.AddEdge("A", "C")
	_ = g.AddEdge("B", "D")
	_ = g.AddEdge("C", "D")
	_ = g.SetEntryPoint("A")
	_ = g.SetFinishPoint("D")
	handler, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	got, err := handler(context.Background(), nil)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	want := []string{"A", "B", "C", "D"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected order: got %v, want %v", got, want)
	}
}

func TestGraph_ErrorPropagation(t *testing.T) {
	g := NewGraph[[]string]()
	_ = g.AddNode("A", appendHandler("A"))
	_ = g.AddNode("B", errorHandler("B"))
	_ = g.AddEdge("A", "B")
	_ = g.SetEntryPoint("A")
	_ = g.SetFinishPoint("B")
	handler, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	got, err := handler(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "node B") {
		t.Fatalf("expected error from node B, got %v", err)
	}
	want := []string{"A"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected state on error: got %v, want %v", got, want)
	}
}

func TestGraph_FinishUnreachable(t *testing.T) {
	g := NewGraph[[]string]()
	_ = g.AddNode("A", appendHandler("A"))
	_ = g.AddNode("B", appendHandler("B"))
	_ = g.AddNode("D", appendHandler("D"))
	_ = g.AddEdge("A", "B")
	_ = g.SetEntryPoint("A")
	_ = g.SetFinishPoint("D")
	if _, err := g.Compile(); err == nil || !strings.Contains(err.Error(), "finish node not reachable") {
		t.Fatalf("expected finish not reachable error, got %v", err)
	}
}
