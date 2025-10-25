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

func TestGraph_ParallelFanOut(t *testing.T) {
	type parallelState struct {
		steps []string
	}

	var mu sync.Mutex
	called := map[string]int{}
	handler := func(name string) GraphHandler[parallelState] {
		return func(ctx context.Context, state parallelState) (parallelState, error) {
			mu.Lock()
			called[name]++
			mu.Unlock()
			next := append(append([]string(nil), state.steps...), name)
			return parallelState{steps: next}, nil
		}
	}

	g := NewGraph[parallelState]()
	_ = g.AddNode("start", handler("start"))
	_ = g.AddNode("branch_a", handler("branch_a"))
	_ = g.AddNode("branch_b", handler("branch_b"))
	_ = g.AddNode("join", handler("join"))

	_ = g.AddEdge("start", "branch_a")
	_ = g.AddEdge("start", "branch_b")
	_ = g.AddEdge("branch_a", "join")
	_ = g.AddEdge("branch_b", "join")

	_ = g.SetEntryPoint("start")
	_ = g.SetFinishPoint("join")

	out, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	final, err := out(context.Background(), parallelState{})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if final.steps[len(final.steps)-1] != "join" {
		t.Fatalf("expected to finish at join, got %v", final.steps)
	}
	if final.steps[1] != "branch_b" {
		t.Fatalf("expected last branch result to win, got steps %v", final.steps)
	}
	if called["branch_a"] != 1 || called["branch_b"] != 1 {
		t.Fatalf("expected both branches to run once, got %v", called)
	}
}

func TestGraph_ParallelPropagatesError(t *testing.T) {
	g := NewGraph[[]string]()
	_ = g.AddNode("start", appendHandler("start"))
	_ = g.AddNode("ok_branch", appendHandler("ok_branch"))
	_ = g.AddNode("fail_branch", func(ctx context.Context, state []string) ([]string, error) {
		return state, fmt.Errorf("boom")
	})
	_ = g.AddNode("join", appendHandler("join"))

	_ = g.AddEdge("start", "ok_branch")
	_ = g.AddEdge("start", "fail_branch")
	_ = g.AddEdge("ok_branch", "join")
	_ = g.AddEdge("fail_branch", "join")
	_ = g.SetEntryPoint("start")
	_ = g.SetFinishPoint("join")

	out, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	_, err = out(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "fail_branch") {
		t.Fatalf("expected error from fail_branch, got %v", err)
	}
}

func TestGraph_DuplicateOperations(t *testing.T) {
	t.Run("duplicate node", func(t *testing.T) {
		g := NewGraph[[]string]()
		if err := g.AddNode("A", appendHandler("A")); err != nil {
			t.Fatalf("unexpected error adding node: %v", err)
		}
		if err := g.AddNode("A", appendHandler("A")); err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("expected duplicate node error, got %v", err)
		}
	})

	t.Run("duplicate edge", func(t *testing.T) {
		g := NewGraph[[]string]()
		_ = g.AddNode("A", appendHandler("A"))
		_ = g.AddNode("B", appendHandler("B"))
		if err := g.AddEdge("A", "B"); err != nil {
			t.Fatalf("unexpected error adding edge: %v", err)
		}
		if err := g.AddEdge("A", "B"); err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("expected duplicate edge error, got %v", err)
		}
	})

	t.Run("duplicate entry point", func(t *testing.T) {
		g := NewGraph[[]string]()
		_ = g.AddNode("A", appendHandler("A"))
		_ = g.AddNode("B", appendHandler("B"))
		if err := g.SetEntryPoint("A"); err != nil {
			t.Fatalf("unexpected error setting entry point: %v", err)
		}
		if err := g.SetEntryPoint("B"); err == nil || !strings.Contains(err.Error(), "already set") {
			t.Fatalf("expected duplicate entry point error, got %v", err)
		}
	})

	t.Run("duplicate finish point", func(t *testing.T) {
		g := NewGraph[[]string]()
		_ = g.AddNode("A", appendHandler("A"))
		_ = g.AddNode("B", appendHandler("B"))
		if err := g.SetFinishPoint("A"); err != nil {
			t.Fatalf("unexpected error setting finish point: %v", err)
		}
		if err := g.SetFinishPoint("B"); err == nil || !strings.Contains(err.Error(), "already set") {
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
	g := NewGraph[[]string]()
	_ = g.AddNode("A", appendHandler("A"))
	_ = g.AddNode("B", appendHandler("B"))
	_ = g.AddNode("C", appendHandler("C"))
	_ = g.AddNode("D", appendHandler("D"))

	_ = g.AddEdge("A", "B")
	// Both conditions are true, but only the first should be taken
	_ = g.AddEdge("B", "C", WithEdgeCondition(func(_ context.Context, state []string) bool {
		return true // First condition: always true
	}))
	_ = g.AddEdge("B", "D", WithEdgeCondition(func(_ context.Context, state []string) bool {
		return true // Second condition: also always true
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
		t.Fatalf("unexpected path (should take first matching edge): got %v, want %v", got, want)
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
