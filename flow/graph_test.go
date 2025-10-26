package flow

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
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

}

func TestGraph_Run_BFSOrder(t *testing.T) {
	g := NewGraph[[]string](WithParallel[[]string](false))
	if g.parallel {
		t.Fatalf("expected graph to run sequentially")
	}
	var execOrder []string
	handlerFor := func(name string) GraphHandler[[]string] {
		return func(ctx context.Context, state []string) ([]string, error) {
			execOrder = append(execOrder, name)
			return append(state, name), nil
		}
	}
	_ = g.AddNode("A", handlerFor("A"))
	_ = g.AddNode("B", handlerFor("B"))
	_ = g.AddNode("C", handlerFor("C"))
	_ = g.AddNode("D", handlerFor("D"))
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
	wantOrder := []string{"A", "B", "C", "D"}
	if !reflect.DeepEqual(execOrder, wantOrder) {
		t.Fatalf("unexpected execution order: got %v, want %v", execOrder, wantOrder)
	}
	if last := got[len(got)-1]; last != "D" {
		t.Fatalf("expected final node D, got %v", last)
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

func TestGraph_ConditionalEdges(t *testing.T) {
	t.Run("condition_true_path", func(t *testing.T) {
		g := NewGraph[[]string]()
		_ = g.AddNode("A", appendHandler("A"))
		_ = g.AddNode("B", appendHandler("B"))
		_ = g.AddNode("C", appendHandler("C"))
		_ = g.AddNode("D", appendHandler("D"))

		_ = g.AddEdge("A", "B")
		// Conditional edges: if state contains "B", go to C; otherwise go to D
		_ = g.AddEdge("B", "C", WithEdgeCondition(func(_ context.Context, state []string) bool {
			for _, s := range state {
				if s == "B" {
					return true
				}
			}
			return false
		}))
		_ = g.AddEdge("B", "D", WithEdgeCondition(func(_ context.Context, state []string) bool {
			for _, s := range state {
				if s == "B" {
					return false
				}
			}
			return true
		}))

		_ = g.SetEntryPoint("A")
		_ = g.SetFinishPoint("C")

		handler, err := g.Compile()
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}

		got, err := handler(context.Background(), nil)
		if err != nil {
			t.Fatalf("run error: %v", err)
		}

		want := []string{"A", "B", "C"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected path: got %v, want %v", got, want)
		}
	})

	t.Run("condition_false_path", func(t *testing.T) {
		g := NewGraph[[]string]()
		_ = g.AddNode("A", appendHandler("A"))
		_ = g.AddNode("B", appendHandler("B"))
		_ = g.AddNode("C", appendHandler("C"))
		_ = g.AddNode("D", appendHandler("D"))

		_ = g.AddEdge("A", "B")
		// Conditional edges: if state contains "X", go to C; otherwise go to D
		_ = g.AddEdge("B", "C", WithEdgeCondition(func(_ context.Context, state []string) bool {
			for _, s := range state {
				if s == "X" {
					return true
				}
			}
			return false
		}))
		_ = g.AddEdge("B", "D", WithEdgeCondition(func(_ context.Context, state []string) bool {
			for _, s := range state {
				if s == "X" {
					return false
				}
			}
			return true
		}))

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

		want := []string{"A", "B", "D"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected path: got %v, want %v", got, want)
		}
	})

	t.Run("no_condition_matches", func(t *testing.T) {
		g := NewGraph[[]string]()
		_ = g.AddNode("A", appendHandler("A"))
		_ = g.AddNode("B", appendHandler("B"))
		_ = g.AddNode("C", appendHandler("C"))

		_ = g.AddEdge("A", "B")
		// Only conditional edge that always returns false
		_ = g.AddEdge("B", "C", WithEdgeCondition(func(_ context.Context, state []string) bool {
			return false
		}))

		_ = g.SetEntryPoint("A")
		_ = g.SetFinishPoint("C")

		handler, err := g.Compile()
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}

		_, err = handler(context.Background(), nil)
		if err == nil || !strings.Contains(err.Error(), "no condition matched") {
			t.Fatalf("expected no condition matched error, got %v", err)
		}
	})
}

func TestGraph_ConditionalEdges_Loop(t *testing.T) {
	g := NewGraph[int]()
	_ = g.AddNode("start", func(ctx context.Context, state int) (int, error) {
		return state + 1, nil
	})
	_ = g.AddNode("loop", func(ctx context.Context, state int) (int, error) {
		return state + 1, nil
	})
	_ = g.AddNode("done", func(ctx context.Context, state int) (int, error) {
		return state, nil
	})

	_ = g.AddEdge("start", "loop")
	_ = g.AddEdge("loop", "loop", WithEdgeCondition(func(_ context.Context, state int) bool {
		return state < 3
	}))
	_ = g.AddEdge("loop", "done")

	_ = g.SetEntryPoint("start")
	_ = g.SetFinishPoint("done")

	handler, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	got, err := handler(context.Background(), 0)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != 3 {
		t.Fatalf("unexpected final state: got %v, want %v", got, 3)
	}
}

func TestGraph_DuplicateOperations(t *testing.T) {
	t.Run("duplicate node", func(t *testing.T) {
		g := NewGraph[[]string]()
		g.AddNode("A", appendHandler("A"))
		g.AddNode("A", appendHandler("A"))
		g.SetEntryPoint("A").SetFinishPoint("A")

		_, err := g.Compile()
		if err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("expected duplicate node error, got %v", err)
		}
	})

	t.Run("duplicate edge", func(t *testing.T) {
		g := NewGraph[[]string]()
		g.AddNode("A", appendHandler("A")).
			AddNode("B", appendHandler("B")).
			AddEdge("A", "B").
			AddEdge("A", "B"). // duplicate
			SetEntryPoint("A").
			SetFinishPoint("B")

		_, err := g.Compile()
		if err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("expected duplicate edge error, got %v", err)
		}
	})

	t.Run("duplicate entry point", func(t *testing.T) {
		g := NewGraph[[]string]()
		g.AddNode("A", appendHandler("A")).
			AddNode("B", appendHandler("B")).
			SetEntryPoint("A").
			SetEntryPoint("B"). // duplicate
			SetFinishPoint("A")

		_, err := g.Compile()
		if err == nil || !strings.Contains(err.Error(), "already set") {
			t.Fatalf("expected duplicate entry point error, got %v", err)
		}
	})

	t.Run("duplicate finish point", func(t *testing.T) {
		g := NewGraph[[]string]()
		g.AddNode("A", appendHandler("A")).
			AddNode("B", appendHandler("B")).
			SetEntryPoint("A").
			SetFinishPoint("A").
			SetFinishPoint("B") // duplicate

		_, err := g.Compile()
		if err == nil || !strings.Contains(err.Error(), "already set") {
			t.Fatalf("expected duplicate finish point error, got %v", err)
		}
	})
}

func TestGraph_SingleNode(t *testing.T) {
	g := NewGraph[[]string]()
	_ = g.AddNode("A", appendHandler("A"))
	_ = g.SetEntryPoint("A")
	_ = g.SetFinishPoint("A")

	handler, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	got, err := handler(context.Background(), nil)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}

	want := []string{"A"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected result: got %v, want %v", got, want)
	}
}

func TestGraph_NoOutgoingEdges(t *testing.T) {
	g := NewGraph[[]string]()
	_ = g.AddNode("A", appendHandler("A"))
	_ = g.AddNode("B", appendHandler("B"))
	_ = g.AddNode("C", appendHandler("C"))

	_ = g.AddEdge("A", "B")
	// B has conditional edges that may not match at runtime
	_ = g.AddEdge("B", "C", WithEdgeCondition(func(_ context.Context, state []string) bool {
		// This condition will never be true
		return false
	}))

	_ = g.SetEntryPoint("A")
	_ = g.SetFinishPoint("C")

	handler, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	_, err = handler(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "no condition matched") {
		t.Fatalf("expected no condition matched error (runtime no outgoing edges), got %v", err)
	}
}

func TestGraph_MultipleConditionsMatch(t *testing.T) {
	g := NewGraph[[]string](WithParallel[[]string](false)) // Use serial mode for global state flow
	_ = g.AddNode("A", appendHandler("A"))
	_ = g.AddNode("B", appendHandler("B"))
	_ = g.AddNode("C", appendHandler("C"))
	_ = g.AddNode("D", appendHandler("D"))
	_ = g.AddNode("E", appendHandler("E"))
	_ = g.AddNode("F", appendHandler("F"))
	_ = g.AddNode("G", appendHandler("G"))

	// A has two conditional edges that both return true - both should be executed
	_ = g.AddEdge("A", "B", WithEdgeCondition(func(_ context.Context, state []string) bool { return true }))
	_ = g.AddEdge("A", "C", WithEdgeCondition(func(_ context.Context, state []string) bool { return true }))
	_ = g.AddEdge("B", "D")
	_ = g.AddEdge("C", "E")
	_ = g.AddEdge("D", "F")
	_ = g.AddEdge("E", "F")
	_ = g.AddEdge("F", "G")

	_ = g.SetEntryPoint("A")
	_ = g.SetFinishPoint("G")

	handler, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	got, err := handler(context.Background(), nil)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}

	// With global state flow in serial mode, all nodes contribute to final state
	want := []string{"A", "B", "C", "D", "E", "F", "G"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected path (global state flow): got %v, want %v", got, want)
	}
}

func TestGraph_MixedEdges(t *testing.T) {
	t.Run("conditional_takes_precedence", func(t *testing.T) {
		g := NewGraph[[]string]()
		_ = g.AddNode("A", appendHandler("A"))
		_ = g.AddNode("B", appendHandler("B"))
		_ = g.AddNode("C", appendHandler("C"))
		_ = g.AddNode("D", appendHandler("D"))

		_ = g.AddEdge("A", "B")
		// Mix: one conditional edge and one unconditional edge
		_ = g.AddEdge("B", "C", WithEdgeCondition(func(_ context.Context, state []string) bool {
			return true
		}))
		_ = g.AddEdge("B", "D") // unconditional

		_ = g.SetEntryPoint("A")
		_ = g.SetFinishPoint("C")

		handler, err := g.Compile()
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}

		got, err := handler(context.Background(), nil)
		if err != nil {
			t.Fatalf("run error: %v", err)
		}

		// Should follow conditional logic, not fan-out
		want := []string{"A", "B", "C"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected path: got %v, want %v", got, want)
		}
	})

	t.Run("unconditional_as_fallback", func(t *testing.T) {
		g := NewGraph[[]string]()
		_ = g.AddNode("A", appendHandler("A"))
		_ = g.AddNode("B", appendHandler("B"))
		_ = g.AddNode("C", appendHandler("C"))
		_ = g.AddNode("D", appendHandler("D"))

		_ = g.AddEdge("A", "B")
		// Mix: conditional edge that returns false, followed by unconditional edge
		_ = g.AddEdge("B", "C", WithEdgeCondition(func(_ context.Context, state []string) bool {
			return false
		}))
		_ = g.AddEdge("B", "D") // unconditional acts as fallback (condition==nil always matches)

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

		// Should take the unconditional edge as fallback
		want := []string{"A", "B", "D"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected path: got %v, want %v", got, want)
		}
	})
}

func TestGraph_ContextCancellation(t *testing.T) {
	g := NewGraph[[]string]()

	cancelled := false
	slowHandler := func(ctx context.Context, state []string) ([]string, error) {
		select {
		case <-ctx.Done():
			cancelled = true
			return state, ctx.Err()
		default:
			return append(state, "slow"), nil
		}
	}

	_ = g.AddNode("A", slowHandler)
	_ = g.SetEntryPoint("A")
	_ = g.SetFinishPoint("A")

	handler, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = handler(ctx, nil)
	if err == nil {
		t.Fatalf("expected context cancellation error")
	}
	if !cancelled {
		t.Fatalf("expected handler to detect cancellation")
	}
}

func TestGraph_ComplexCycles(t *testing.T) {
	t.Run("nested_loops", func(t *testing.T) {
		g := NewGraph[int]()

		_ = g.AddNode("start", func(ctx context.Context, state int) (int, error) {
			return state, nil
		})
		_ = g.AddNode("outer_loop", func(ctx context.Context, state int) (int, error) {
			return state + 1, nil
		})
		_ = g.AddNode("inner_loop", func(ctx context.Context, state int) (int, error) {
			return state + 10, nil
		})
		_ = g.AddNode("done", func(ctx context.Context, state int) (int, error) {
			return state, nil
		})

		_ = g.AddEdge("start", "outer_loop")

		// Inner loop: increment by 10 while < 30
		_ = g.AddEdge("outer_loop", "inner_loop")
		_ = g.AddEdge("inner_loop", "inner_loop", WithEdgeCondition(func(_ context.Context, state int) bool {
			return state < 30
		}))
		_ = g.AddEdge("inner_loop", "outer_loop", WithEdgeCondition(func(_ context.Context, state int) bool {
			return state >= 30 && state < 100
		}))

		// Exit outer loop
		_ = g.AddEdge("inner_loop", "done", WithEdgeCondition(func(_ context.Context, state int) bool {
			return state >= 100
		}))

		_ = g.SetEntryPoint("start")
		_ = g.SetFinishPoint("done")

		handler, err := g.Compile()
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}

		got, err := handler(context.Background(), 0)
		if err != nil {
			t.Fatalf("run error: %v", err)
		}

		// Should loop until reaching >= 100
		if got < 100 {
			t.Fatalf("unexpected final state: got %v, want >= 100", got)
		}
	})

	t.Run("self_loop_with_exit", func(t *testing.T) {
		g := NewGraph[int]()

		_ = g.AddNode("loop", func(ctx context.Context, state int) (int, error) {
			return state + 1, nil
		})
		_ = g.AddNode("exit", func(ctx context.Context, state int) (int, error) {
			return state, nil
		})

		// Self-loop while < 5
		_ = g.AddEdge("loop", "loop", WithEdgeCondition(func(_ context.Context, state int) bool {
			return state < 5
		}))
		// Exit when >= 5
		_ = g.AddEdge("loop", "exit", WithEdgeCondition(func(_ context.Context, state int) bool {
			return state >= 5
		}))

		_ = g.SetEntryPoint("loop")
		_ = g.SetFinishPoint("exit")

		handler, err := g.Compile()
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}

		got, err := handler(context.Background(), 0)
		if err != nil {
			t.Fatalf("run error: %v", err)
		}

		if got != 5 {
			t.Fatalf("unexpected final state: got %v, want 5", got)
		}
	})
}

// TestGraph_ConditionalJoinInteraction tests how conditional edges interact with joins
// When A has two conditional edges but only one matches, the join should only wait for the active path
func TestGraph_ConditionalJoinInteraction(t *testing.T) {
	g := NewGraph[[]string](WithParallel[[]string](false))

	_ = g.AddNode("A", appendHandler("A"))
	_ = g.AddNode("B", appendHandler("B"))
	_ = g.AddNode("C", appendHandler("C"))
	_ = g.AddNode("D", appendHandler("D"))
	_ = g.AddNode("E", appendHandler("E"))
	_ = g.AddNode("F", appendHandler("F"))
	_ = g.AddNode("G", appendHandler("G"))

	// A has two conditional edges, but only first one returns true
	_ = g.AddEdge("A", "B", WithEdgeCondition(func(_ context.Context, state []string) bool {
		return true
	}))
	_ = g.AddEdge("A", "C", WithEdgeCondition(func(_ context.Context, state []string) bool {
		return false
	}))

	_ = g.AddEdge("B", "D")
	_ = g.AddEdge("C", "E")
	_ = g.AddEdge("D", "F")
	_ = g.AddEdge("E", "F") // This edge will never be activated
	_ = g.AddEdge("F", "G")

	_ = g.SetEntryPoint("A")
	_ = g.SetFinishPoint("G")

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	result, err := compiled(context.Background(), nil)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}

	// Only A->B->D->F->G path should execute
	want := []string{"A", "B", "D", "F", "G"}
	if !reflect.DeepEqual(result, want) {
		t.Errorf("unexpected execution path: got %v, want %v", result, want)
	}

	// C and E should not execute
	for _, node := range result {
		if node == "C" || node == "E" {
			t.Errorf("node %s should not have executed (inactive branch)", node)
		}
	}
}

// TestGraph_SerialFanOut tests that serial mode executes all fan-out branches
func TestGraph_SerialFanOut(t *testing.T) {
	var executed []string
	var mu sync.Mutex

	handler := func(name string) GraphHandler[[]string] {
		return func(ctx context.Context, state []string) ([]string, error) {
			mu.Lock()
			executed = append(executed, name)
			mu.Unlock()
			return append(state, name), nil
		}
	}

	g := NewGraph[[]string](WithParallel[[]string](false))

	_ = g.AddNode("A", handler("A"))
	_ = g.AddNode("B", handler("B"))
	_ = g.AddNode("C", handler("C"))
	_ = g.AddNode("D", handler("D"))
	_ = g.AddNode("E", handler("E"))
	_ = g.AddNode("F", handler("F"))
	_ = g.AddNode("G", handler("G"))

	// Unconditional edges - both branches should execute
	_ = g.AddEdge("A", "B")
	_ = g.AddEdge("A", "C")
	_ = g.AddEdge("B", "D")
	_ = g.AddEdge("C", "E")
	_ = g.AddEdge("D", "F")
	_ = g.AddEdge("E", "F")
	_ = g.AddEdge("F", "G")

	_ = g.SetEntryPoint("A")
	_ = g.SetFinishPoint("G")

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	result, err := compiled(context.Background(), nil)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}

	// All 7 nodes should execute
	if len(executed) != 7 {
		t.Errorf("expected 7 nodes to execute, got %d: %v", len(executed), executed)
	}

	// Final state should include all nodes
	if len(result) != 7 {
		t.Errorf("expected final state to have 7 elements, got %d: %v", len(result), result)
	}
}

// TestGraph_StateFlowSerial tests state propagation in serial mode
func TestGraph_StateFlowSerial(t *testing.T) {
	g := NewGraph[[]string](WithParallel[[]string](false))

	_ = g.AddNode("A", appendHandler("A"))
	_ = g.AddNode("B", appendHandler("B"))
	_ = g.AddNode("C", appendHandler("C"))
	_ = g.AddNode("D", appendHandler("D"))
	_ = g.AddNode("E", appendHandler("E"))
	_ = g.AddNode("F", appendHandler("F"))
	_ = g.AddNode("G", appendHandler("G"))

	_ = g.AddEdge("A", "B")
	_ = g.AddEdge("A", "C")
	_ = g.AddEdge("B", "D")
	_ = g.AddEdge("C", "E")
	_ = g.AddEdge("D", "F")
	_ = g.AddEdge("E", "F")
	_ = g.AddEdge("F", "G")

	_ = g.SetEntryPoint("A")
	_ = g.SetFinishPoint("G")

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	result, err := compiled(context.Background(), []string{})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}

	// All nodes should be in final state
	want := []string{"A", "B", "C", "D", "E", "F", "G"}
	if !reflect.DeepEqual(result, want) {
		t.Errorf("unexpected state flow: got %v, want %v", result, want)
	}
}
