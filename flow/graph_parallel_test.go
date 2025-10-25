package flow

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// Common state structure with slice field
type testState struct {
	Steps []string
}

func TestGraph_ParallelFanOut(t *testing.T) {
	var mu sync.Mutex
	called := map[string]int{}
	handler := func(name string) GraphHandler[*testState] {
		return func(ctx context.Context, state *testState) (*testState, error) {
			mu.Lock()
			called[name]++
			mu.Unlock()
			state.Steps = append(state.Steps, name)
			return state, nil
		}
	}

	g := NewGraph[*testState]()
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

	final, err := out(context.Background(), &testState{})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if final.Steps[len(final.Steps)-1] != "join" {
		t.Fatalf("expected to finish at join, got %v", final.Steps)
	}
	if final.Steps[1] != "branch_b" {
		t.Fatalf("expected last branch result to win, got steps %v", final.Steps)
	}
	if called["branch_a"] != 1 || called["branch_b"] != 1 {
		t.Fatalf("expected both branches to run once, got %v", called)
	}
}

func TestGraph_ParallelPropagatesError(t *testing.T) {
	appendHandler := func(name string) GraphHandler[*testState] {
		return func(ctx context.Context, state *testState) (*testState, error) {
			state.Steps = append(state.Steps, name)
			return state, nil
		}
	}

	g := NewGraph[*testState]()
	_ = g.AddNode("start", appendHandler("start"))
	_ = g.AddNode("ok_branch", appendHandler("ok_branch"))
	_ = g.AddNode("fail_branch", func(ctx context.Context, state *testState) (*testState, error) {
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

	_, err = out(context.Background(), &testState{})
	if err == nil || !strings.Contains(err.Error(), "fail_branch") {
		t.Fatalf("expected error from fail_branch, got %v", err)
	}
}

func TestGraph_SerialVsParallelStateSemantics(t *testing.T) {
	// This test reveals a semantic difference between serial and parallel modes
	type counter struct {
		Value int
	}

	incrementHandler := func(name string, amount int) GraphHandler[*counter] {
		return func(ctx context.Context, state *counter) (*counter, error) {
			t.Logf("%s: received state.Value=%d, adding %d", name, state.Value, amount)
			return &counter{Value: state.Value + amount}, nil
		}
	}

	t.Run("parallel_mode_independent_branches", func(t *testing.T) {
		g := NewGraph[*counter](WithParallel[*counter](true))
		_ = g.AddNode("A", incrementHandler("A", 1))
		_ = g.AddNode("B", incrementHandler("B", 10))
		_ = g.AddNode("C", incrementHandler("C", 100))
		_ = g.AddNode("D", incrementHandler("D", 0))

		_ = g.AddEdge("A", "B")
		_ = g.AddEdge("A", "C")
		_ = g.AddEdge("B", "D")
		_ = g.AddEdge("C", "D")

		_ = g.SetEntryPoint("A")
		_ = g.SetFinishPoint("D")

		handler, _ := g.Compile()
		result, err := handler(context.Background(), &counter{Value: 0})
		if err != nil {
			t.Fatalf("error: %v", err)
		}

		// In parallel mode: A=0+1=1, then B and C both see 1
		// B: 1+10=11, C: 1+100=101
		// Winner is C (last branch), so D sees 101
		// D: 101+0=101
		t.Logf("Parallel mode result: %d", result.Value)
		if result.Value != 101 {
			t.Errorf("expected 101, got %d", result.Value)
		}
	})

	t.Run("serial_mode_independent_branches", func(t *testing.T) {
		g := NewGraph[*counter](WithParallel[*counter](false))
		_ = g.AddNode("A", incrementHandler("A", 1))
		_ = g.AddNode("B", incrementHandler("B", 10))
		_ = g.AddNode("C", incrementHandler("C", 100))
		_ = g.AddNode("D", incrementHandler("D", 0))

		_ = g.AddEdge("A", "B")
		_ = g.AddEdge("A", "C")
		_ = g.AddEdge("B", "D")
		_ = g.AddEdge("C", "D")

		_ = g.SetEntryPoint("A")
		_ = g.SetFinishPoint("D")

		handler, _ := g.Compile()
		result, err := handler(context.Background(), &counter{Value: 0})
		if err != nil {
			t.Fatalf("error: %v", err)
		}

		// After fix: Serial mode now uses global state flow
		// A: 0+1=1, state=1
		// B: 1+10=11, state=11
		// C: 11+100=111, state=111 (uses global state)
		// D: 111+0=111
		t.Logf("Serial mode result: %d", result.Value)

		// D receives global accumulated state
		if result.Value != 111 {
			t.Errorf("expected 111 (global state), got %d", result.Value)
		}
	})
}

func TestGraph_ParallelEarlyTermination(t *testing.T) {
	var executed sync.Map

	slowHandler := func(name string, delay int) GraphHandler[*testState] {
		return func(ctx context.Context, state *testState) (*testState, error) {
			executed.Store(name, "started")

			// Simulate work and check for cancellation
			for i := range delay {
				select {
				case <-ctx.Done():
					executed.Store(name, "cancelled")
					t.Logf("%s: cancelled after %d/%d iterations", name, i, delay)
					return state, ctx.Err()
				default:
					// Simulate a small unit of work
				}
			}

			executed.Store(name, "completed")
			t.Logf("%s: completed", name)
			state.Steps = append(state.Steps, name)
			return state, nil
		}
	}

	errorHandler := func(name string) GraphHandler[*testState] {
		return func(ctx context.Context, state *testState) (*testState, error) {
			executed.Store(name, "error")
			t.Logf("%s: returning error", name)
			return state, fmt.Errorf("error from %s", name)
		}
	}

	appendHandler := func(name string) GraphHandler[*testState] {
		return func(ctx context.Context, state *testState) (*testState, error) {
			state.Steps = append(state.Steps, name)
			return state, nil
		}
	}

	t.Run("error_cancels_slow_branches", func(t *testing.T) {
		executed = sync.Map{}

		g := NewGraph[*testState](WithParallel[*testState](true))
		_ = g.AddNode("start", appendHandler("start"))
		_ = g.AddNode("fast_fail", errorHandler("fast_fail"))
		_ = g.AddNode("slow_ok", slowHandler("slow_ok", 1000000)) // Would be slow without cancellation
		_ = g.AddNode("join", appendHandler("join"))

		_ = g.AddEdge("start", "fast_fail")
		_ = g.AddEdge("start", "slow_ok")
		_ = g.AddEdge("fast_fail", "join")
		_ = g.AddEdge("slow_ok", "join")

		_ = g.SetEntryPoint("start")
		_ = g.SetFinishPoint("join")

		handler, _ := g.Compile()
		_, err := handler(context.Background(), &testState{})

		// Should get error from fast_fail
		if err == nil || !strings.Contains(err.Error(), "fast_fail") {
			t.Fatalf("expected error from fast_fail, got %v", err)
		}

		// Check that slow_ok was cancelled (not completed)
		if val, ok := executed.Load("slow_ok"); ok {
			status := val.(string)
			if status == "completed" {
				t.Errorf("slow_ok should have been cancelled, but it completed")
			}
			t.Logf("slow_ok status: %s", status)
		}
	})
}

// TestGraph_JoinPattern tests that a join node waits for all incoming branches
// Graph: a->b, a->c, b->d, c->d
func TestGraph_JoinPattern(t *testing.T) {
	type joinState struct {
		ExecutedNodes []string
	}

	handler := func(name string) GraphHandler[*joinState] {
		return func(ctx context.Context, state *joinState) (*joinState, error) {
			state.ExecutedNodes = append(state.ExecutedNodes, name)
			return state, nil
		}
	}

	t.Run("parallel_mode", func(t *testing.T) {
		g := NewGraph[*joinState](WithParallel[*joinState](true))
		_ = g.AddNode("a", handler("a"))
		_ = g.AddNode("b", handler("b"))
		_ = g.AddNode("c", handler("c"))
		_ = g.AddNode("d", handler("d"))

		_ = g.AddEdge("a", "b")
		_ = g.AddEdge("a", "c")
		_ = g.AddEdge("b", "d")
		_ = g.AddEdge("c", "d")

		_ = g.SetEntryPoint("a")
		_ = g.SetFinishPoint("d")

		compiled, err := g.Compile()
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}

		result, err := compiled(context.Background(), &joinState{})
		if err != nil {
			t.Fatalf("run error: %v", err)
		}

		// Check all nodes executed
		expectedNodes := map[string]bool{"a": false, "b": false, "c": false, "d": false}
		for _, node := range result.ExecutedNodes {
			expectedNodes[node] = true
		}
		for node, executed := range expectedNodes {
			if !executed {
				t.Errorf("node %s was not executed", node)
			}
		}
	})

	t.Run("serial_mode", func(t *testing.T) {
		g := NewGraph[*joinState](WithParallel[*joinState](false))
		_ = g.AddNode("a", handler("a"))
		_ = g.AddNode("b", handler("b"))
		_ = g.AddNode("c", handler("c"))
		_ = g.AddNode("d", handler("d"))

		_ = g.AddEdge("a", "b")
		_ = g.AddEdge("a", "c")
		_ = g.AddEdge("b", "d")
		_ = g.AddEdge("c", "d")

		_ = g.SetEntryPoint("a")
		_ = g.SetFinishPoint("d")

		compiled, err := g.Compile()
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}

		result, err := compiled(context.Background(), &joinState{})
		if err != nil {
			t.Fatalf("run error: %v", err)
		}

		// Check all nodes executed
		expectedNodes := map[string]bool{"a": false, "b": false, "c": false, "d": false}
		for _, node := range result.ExecutedNodes {
			expectedNodes[node] = true
		}
		for node, executed := range expectedNodes {
			if !executed {
				t.Errorf("node %s was not executed", node)
			}
		}
	})
}

// TestGraph_AdvancedJoin tests more complex join scenarios with longer branches
// Graph: a->b->d->finish, a->c->e->finish
func TestGraph_AdvancedJoin(t *testing.T) {
	type advancedState struct {
		ExecutedNodes []string
	}

	handler := func(name string) GraphHandler[*advancedState] {
		return func(ctx context.Context, state *advancedState) (*advancedState, error) {
			state.ExecutedNodes = append(state.ExecutedNodes, name)
			return state, nil
		}
	}

	t.Run("parallel_mode", func(t *testing.T) {
		g := NewGraph[*advancedState](WithParallel[*advancedState](true))

		_ = g.AddNode("a", handler("a"))
		_ = g.AddNode("b", handler("b"))
		_ = g.AddNode("c", handler("c"))
		_ = g.AddNode("d", handler("d"))
		_ = g.AddNode("e", handler("e"))
		_ = g.AddNode("finish", handler("finish"))

		_ = g.AddEdge("a", "b")
		_ = g.AddEdge("a", "c")
		_ = g.AddEdge("b", "d")
		_ = g.AddEdge("c", "e")
		_ = g.AddEdge("d", "finish")
		_ = g.AddEdge("e", "finish")

		_ = g.SetEntryPoint("a")
		_ = g.SetFinishPoint("finish")

		compiled, err := g.Compile()
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}

		result, err := compiled(context.Background(), &advancedState{})
		if err != nil {
			t.Fatalf("run error: %v", err)
		}

		// Check that both branches executed
		hasD := false
		hasE := false
		for _, node := range result.ExecutedNodes {
			if node == "d" {
				hasD = true
			}
			if node == "e" {
				hasE = true
			}
		}

		if !hasD || !hasE {
			t.Errorf("both branches should execute: d=%v, e=%v, executed=%v", hasD, hasE, result.ExecutedNodes)
		}
	})

	t.Run("serial_mode", func(t *testing.T) {
		g := NewGraph[*advancedState](WithParallel[*advancedState](false))

		_ = g.AddNode("a", handler("a"))
		_ = g.AddNode("b", handler("b"))
		_ = g.AddNode("c", handler("c"))
		_ = g.AddNode("d", handler("d"))
		_ = g.AddNode("e", handler("e"))
		_ = g.AddNode("finish", handler("finish"))

		_ = g.AddEdge("a", "b")
		_ = g.AddEdge("a", "c")
		_ = g.AddEdge("b", "d")
		_ = g.AddEdge("c", "e")
		_ = g.AddEdge("d", "finish")
		_ = g.AddEdge("e", "finish")

		_ = g.SetEntryPoint("a")
		_ = g.SetFinishPoint("finish")

		compiled, err := g.Compile()
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}

		result, err := compiled(context.Background(), &advancedState{})
		if err != nil {
			t.Fatalf("run error: %v", err)
		}

		// Check that both branches executed
		hasD := false
		hasE := false
		for _, node := range result.ExecutedNodes {
			if node == "d" {
				hasD = true
			}
			if node == "e" {
				hasE = true
			}
		}

		if !hasD || !hasE {
			t.Errorf("both branches should execute: d=%v, e=%v", hasD, hasE)
		}
	})
}

// TestGraph_StateFlowParallel tests state propagation in parallel mode
// In parallel mode, all branches execute but only the winner's state is used
func TestGraph_StateFlowParallel(t *testing.T) {
	var executed sync.Map

	handler := func(name string) GraphHandler[*testState] {
		return func(ctx context.Context, state *testState) (*testState, error) {
			executed.Store(name, true)
			state.Steps = append(state.Steps, name)
			return state, nil
		}
	}

	g := NewGraph[*testState](WithParallel[*testState](true))

	_ = g.AddNode("A", handler("A"))
	_ = g.AddNode("B", handler("B"))
	_ = g.AddNode("C", handler("C"))
	_ = g.AddNode("D", handler("D"))
	_ = g.AddNode("E", handler("E"))
	_ = g.AddNode("F", handler("F"))
	_ = g.AddNode("G", handler("G"))

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

	result, err := compiled(context.Background(), &testState{})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}

	// In parallel mode, all nodes should execute even if only one branch's state wins
	expectedNodes := []string{"A", "B", "C", "D", "E", "F", "G"}
	for _, node := range expectedNodes {
		if val, ok := executed.Load(node); !ok || !val.(bool) {
			t.Errorf("node %s was not executed", node)
		}
	}

	// The final result should at least include A, F, and G (common path)
	hasA := false
	hasF := false
	hasG := false
	for _, node := range result.Steps {
		if node == "A" {
			hasA = true
		}
		if node == "F" {
			hasF = true
		}
		if node == "G" {
			hasG = true
		}
	}

	if !hasA || !hasF || !hasG {
		t.Errorf("expected A, F, G in result, got %v", result.Steps)
	}
}

// TestGraph_ParallelConditionalEdges tests conditional edges in parallel mode
func TestGraph_ParallelConditionalEdges(t *testing.T) {
	appendHandler := func(name string) GraphHandler[*testState] {
		return func(ctx context.Context, state *testState) (*testState, error) {
			state.Steps = append(state.Steps, name)
			return state, nil
		}
	}

	contains := func(steps []string, target string) bool {
		for _, s := range steps {
			if s == target {
				return true
			}
		}
		return false
	}

	t.Run("parallel_condition_true_path", func(t *testing.T) {
		g := NewGraph[*testState](WithParallel[*testState](true))
		_ = g.AddNode("A", appendHandler("A"))
		_ = g.AddNode("B", appendHandler("B"))
		_ = g.AddNode("C", appendHandler("C"))
		_ = g.AddNode("D", appendHandler("D"))

		_ = g.AddEdge("A", "B")
		// Conditional edges: if state contains "B", go to C; otherwise go to D
		_ = g.AddEdge("B", "C", WithEdgeCondition(func(_ context.Context, state *testState) bool {
			return contains(state.Steps, "B")
		}))
		_ = g.AddEdge("B", "D", WithEdgeCondition(func(_ context.Context, state *testState) bool {
			return !contains(state.Steps, "B")
		}))

		_ = g.SetEntryPoint("A")
		_ = g.SetFinishPoint("C")

		handler, err := g.Compile()
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}

		got, err := handler(context.Background(), &testState{})
		if err != nil {
			t.Fatalf("run error: %v", err)
		}

		// Should follow path A->B->C
		if !contains(got.Steps, "A") || !contains(got.Steps, "B") || !contains(got.Steps, "C") {
			t.Fatalf("unexpected path: got %v, want path through A, B, C", got.Steps)
		}
		if contains(got.Steps, "D") {
			t.Fatalf("unexpected node D in path: got %v", got.Steps)
		}
	})

	t.Run("parallel_condition_false_path", func(t *testing.T) {
		g := NewGraph[*testState](WithParallel[*testState](true))
		_ = g.AddNode("A", appendHandler("A"))
		_ = g.AddNode("B", appendHandler("B"))
		_ = g.AddNode("C", appendHandler("C"))
		_ = g.AddNode("D", appendHandler("D"))

		_ = g.AddEdge("A", "B")
		// Conditional edges: if state contains "X", go to C; otherwise go to D
		_ = g.AddEdge("B", "C", WithEdgeCondition(func(_ context.Context, state *testState) bool {
			return contains(state.Steps, "X")
		}))
		_ = g.AddEdge("B", "D", WithEdgeCondition(func(_ context.Context, state *testState) bool {
			return !contains(state.Steps, "X")
		}))

		_ = g.SetEntryPoint("A")
		_ = g.SetFinishPoint("D")

		handler, err := g.Compile()
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}

		got, err := handler(context.Background(), &testState{})
		if err != nil {
			t.Fatalf("run error: %v", err)
		}

		// Should follow path A->B->D
		if !contains(got.Steps, "A") || !contains(got.Steps, "B") || !contains(got.Steps, "D") {
			t.Fatalf("unexpected path: got %v, want path through A, B, D", got.Steps)
		}
		if contains(got.Steps, "C") {
			t.Fatalf("unexpected node C in path: got %v", got.Steps)
		}
	})
}

// TestGraph_ParallelComplexCycles tests complex cycles in parallel mode
func TestGraph_ParallelComplexCycles(t *testing.T) {
	t.Run("parallel_nested_loops", func(t *testing.T) {
		type loopState struct {
			Value int
		}

		g := NewGraph[*loopState](WithParallel[*loopState](true))

		_ = g.AddNode("start", func(ctx context.Context, state *loopState) (*loopState, error) {
			return state, nil
		})
		_ = g.AddNode("outer_loop", func(ctx context.Context, state *loopState) (*loopState, error) {
			state.Value = state.Value + 1
			return state, nil
		})
		_ = g.AddNode("inner_loop", func(ctx context.Context, state *loopState) (*loopState, error) {
			state.Value = state.Value + 10
			return state, nil
		})
		_ = g.AddNode("done", func(ctx context.Context, state *loopState) (*loopState, error) {
			return state, nil
		})

		_ = g.AddEdge("start", "outer_loop")

		// Inner loop: increment by 10 while < 30
		_ = g.AddEdge("outer_loop", "inner_loop")
		_ = g.AddEdge("inner_loop", "inner_loop", WithEdgeCondition(func(_ context.Context, state *loopState) bool {
			return state.Value < 30
		}))
		_ = g.AddEdge("inner_loop", "outer_loop", WithEdgeCondition(func(_ context.Context, state *loopState) bool {
			return state.Value >= 30 && state.Value < 100
		}))

		// Exit outer loop
		_ = g.AddEdge("inner_loop", "done", WithEdgeCondition(func(_ context.Context, state *loopState) bool {
			return state.Value >= 100
		}))

		_ = g.SetEntryPoint("start")
		_ = g.SetFinishPoint("done")

		handler, err := g.Compile()
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}

		got, err := handler(context.Background(), &loopState{Value: 0})
		if err != nil {
			t.Fatalf("run error: %v", err)
		}

		// Should loop until reaching >= 100
		if got.Value < 100 {
			t.Fatalf("unexpected final state: got %v, want >= 100", got.Value)
		}
	})

	t.Run("parallel_self_loop_with_exit", func(t *testing.T) {
		type loopState struct {
			Value int
		}

		g := NewGraph[*loopState](WithParallel[*loopState](true))

		_ = g.AddNode("loop", func(ctx context.Context, state *loopState) (*loopState, error) {
			state.Value = state.Value + 1
			return state, nil
		})
		_ = g.AddNode("exit", func(ctx context.Context, state *loopState) (*loopState, error) {
			return state, nil
		})

		// Self-loop while < 5
		_ = g.AddEdge("loop", "loop", WithEdgeCondition(func(_ context.Context, state *loopState) bool {
			return state.Value < 5
		}))
		// Exit when >= 5
		_ = g.AddEdge("loop", "exit", WithEdgeCondition(func(_ context.Context, state *loopState) bool {
			return state.Value >= 5
		}))

		_ = g.SetEntryPoint("loop")
		_ = g.SetFinishPoint("exit")

		handler, err := g.Compile()
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}

		got, err := handler(context.Background(), &loopState{Value: 0})
		if err != nil {
			t.Fatalf("run error: %v", err)
		}

		if got.Value != 5 {
			t.Fatalf("unexpected final state: got %v, want 5", got.Value)
		}
	})
}

// TestGraph_ParallelMultipleConditionsMatch tests multiple conditions matching in parallel mode
func TestGraph_ParallelMultipleConditionsMatch(t *testing.T) {
	appendHandler := func(name string) GraphHandler[*testState] {
		return func(ctx context.Context, state *testState) (*testState, error) {
			state.Steps = append(state.Steps, name)
			return state, nil
		}
	}

	g := NewGraph[*testState](WithParallel[*testState](true))
	_ = g.AddNode("A", appendHandler("A"))
	_ = g.AddNode("B", appendHandler("B"))
	_ = g.AddNode("C", appendHandler("C"))
	_ = g.AddNode("D", appendHandler("D"))
	_ = g.AddNode("E", appendHandler("E"))
	_ = g.AddNode("F", appendHandler("F"))
	_ = g.AddNode("G", appendHandler("G"))

	// A has two conditional edges that both return true - both branches should be executed in parallel
	_ = g.AddEdge("A", "B", WithEdgeCondition(func(_ context.Context, state *testState) bool { return true }))
	_ = g.AddEdge("A", "C", WithEdgeCondition(func(_ context.Context, state *testState) bool { return true }))
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

	got, err := handler(context.Background(), &testState{})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}

	// In parallel mode, both branches execute but winner's state is used
	// Should have A and G for sure (start and end)
	hasA := false
	hasG := false
	for _, step := range got.Steps {

		if step == "A" {
			hasA = true
		}
		if step == "G" {
			hasG = true
		}
	}

	if !hasA || !hasG {
		t.Errorf("expected A and G in result, got %v", got.Steps)
	}

	// In parallel mode, at least one path should complete
	t.Logf("Parallel multiple conditions result: %v", got.Steps)
}

// TestGraph_ParallelContextTimeout tests context timeout in parallel mode
func TestGraph_ParallelContextTimeout(t *testing.T) {
	slowHandler := func(name string) GraphHandler[*testState] {
		return func(ctx context.Context, state *testState) (*testState, error) {
			select {
			case <-ctx.Done():
				return state, ctx.Err()
			case <-make(chan struct{}): // Block forever
				return state, nil
			}
		}
	}

	g := NewGraph[*testState](WithParallel[*testState](true))
	_ = g.AddNode("A", slowHandler("A"))
	_ = g.SetEntryPoint("A")
	_ = g.SetFinishPoint("A")

	handler, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1)
	defer cancel()

	_, err = handler(ctx, &testState{})
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if !strings.Contains(err.Error(), "context") {
		t.Errorf("expected context error, got: %v", err)
	}
}

// TestGraph_ParallelManyBranches tests many parallel branches
func TestGraph_ParallelManyBranches(t *testing.T) {
	var mu sync.Mutex
	executed := make(map[string]bool)

	handler := func(name string) GraphHandler[*testState] {
		return func(ctx context.Context, state *testState) (*testState, error) {
			mu.Lock()
			executed[name] = true
			mu.Unlock()
			state.Steps = append(state.Steps, name)
			return state, nil
		}
	}

	g := NewGraph[*testState](WithParallel[*testState](true))
	_ = g.AddNode("start", handler("start"))
	_ = g.AddNode("join", handler("join"))

	// Create 20 parallel branches
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("branch_%d", i)
		_ = g.AddNode(name, handler(name))
		_ = g.AddEdge("start", name)
		_ = g.AddEdge(name, "join")
	}

	_ = g.SetEntryPoint("start")
	_ = g.SetFinishPoint("join")

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	result, err := compiled(context.Background(), &testState{})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}

	// All branches should have executed
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("branch_%d", i)
		if !executed[name] {
			t.Errorf("branch %s was not executed", name)
		}
	}

	// Final result should include start and join
	hasStart := false
	hasJoin := false
	for _, step := range result.Steps {
		if step == "start" {
			hasStart = true
		}
		if step == "join" {
			hasJoin = true
		}
	}
	if !hasStart || !hasJoin {
		t.Errorf("expected start and join in result, got %v", result.Steps)
	}
}

// TestGraph_ParallelAllBranchesFail tests when all parallel branches fail
func TestGraph_ParallelAllBranchesFail(t *testing.T) {
	errorHandler := func(name string) GraphHandler[*testState] {
		return func(ctx context.Context, state *testState) (*testState, error) {
			return state, fmt.Errorf("error from %s", name)
		}
	}

	startHandler := func(ctx context.Context, state *testState) (*testState, error) {
		state.Steps = append(state.Steps, "start")
		return state, nil
	}

	g := NewGraph[*testState](WithParallel[*testState](true))
	_ = g.AddNode("start", startHandler)
	_ = g.AddNode("branch_a", errorHandler("branch_a"))
	_ = g.AddNode("branch_b", errorHandler("branch_b"))
	_ = g.AddNode("branch_c", errorHandler("branch_c"))
	_ = g.AddNode("join", startHandler) // Won't reach here

	_ = g.AddEdge("start", "branch_a")
	_ = g.AddEdge("start", "branch_b")
	_ = g.AddEdge("start", "branch_c")
	_ = g.AddEdge("branch_a", "join")
	_ = g.AddEdge("branch_b", "join")
	_ = g.AddEdge("branch_c", "join")

	_ = g.SetEntryPoint("start")
	_ = g.SetFinishPoint("join")

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	_, err = compiled(context.Background(), &testState{})
	if err == nil {
		t.Fatalf("expected error from branches")
	}

	// Should contain error from one of the branches
	hasError := strings.Contains(err.Error(), "branch_a") ||
		strings.Contains(err.Error(), "branch_b") ||
		strings.Contains(err.Error(), "branch_c")
	if !hasError {
		t.Errorf("expected error from one of the branches, got: %v", err)
	}
}

// TestGraph_ParallelSingleNode tests single node in parallel mode
func TestGraph_ParallelSingleNode(t *testing.T) {
	g := NewGraph[*testState](WithParallel[*testState](true))
	_ = g.AddNode("A", func(ctx context.Context, state *testState) (*testState, error) {
		state.Steps = append(state.Steps, "A")
		return state, nil
	})
	_ = g.SetEntryPoint("A")
	_ = g.SetFinishPoint("A")

	handler, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	got, err := handler(context.Background(), &testState{})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}

	if len(got.Steps) != 1 || got.Steps[0] != "A" {
		t.Fatalf("unexpected result: got %v, want [A]", got.Steps)
	}
}

// TestGraph_ParallelMixedEdges tests mixed conditional and unconditional edges in parallel
func TestGraph_ParallelMixedEdges(t *testing.T) {
	appendHandler := func(name string) GraphHandler[*testState] {
		return func(ctx context.Context, state *testState) (*testState, error) {
			state.Steps = append(state.Steps, name)
			return state, nil
		}
	}

	t.Run("parallel_conditional_takes_precedence", func(t *testing.T) {
		g := NewGraph[*testState](WithParallel[*testState](true))
		_ = g.AddNode("A", appendHandler("A"))
		_ = g.AddNode("B", appendHandler("B"))
		_ = g.AddNode("C", appendHandler("C"))
		_ = g.AddNode("D", appendHandler("D"))

		_ = g.AddEdge("A", "B")
		// Mix: one conditional edge and one unconditional edge
		_ = g.AddEdge("B", "C", WithEdgeCondition(func(_ context.Context, state *testState) bool {
			return true
		}))
		_ = g.AddEdge("B", "D") // unconditional

		_ = g.SetEntryPoint("A")
		_ = g.SetFinishPoint("C")

		handler, err := g.Compile()
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}

		got, err := handler(context.Background(), &testState{})
		if err != nil {
			t.Fatalf("run error: %v", err)
		}

		// Should follow conditional logic, not fan-out to D
		hasA := false
		hasB := false
		hasC := false
		for _, step := range got.Steps {
			if step == "A" {
				hasA = true
			}
			if step == "B" {
				hasB = true
			}
			if step == "C" {
				hasC = true
			}
		}

		if !hasA || !hasB || !hasC {
			t.Errorf("expected A, B, C in path, got %v", got.Steps)
		}
	})

	t.Run("parallel_unconditional_as_fallback", func(t *testing.T) {
		g := NewGraph[*testState](WithParallel[*testState](true))
		_ = g.AddNode("A", appendHandler("A"))
		_ = g.AddNode("B", appendHandler("B"))
		_ = g.AddNode("C", appendHandler("C"))
		_ = g.AddNode("D", appendHandler("D"))

		_ = g.AddEdge("A", "B")
		// Mix: conditional edge that returns false, followed by unconditional edge
		_ = g.AddEdge("B", "C", WithEdgeCondition(func(_ context.Context, state *testState) bool {
			return false
		}))
		_ = g.AddEdge("B", "D") // unconditional acts as fallback

		_ = g.SetEntryPoint("A")
		_ = g.SetFinishPoint("D")

		handler, err := g.Compile()
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}

		got, err := handler(context.Background(), &testState{})
		if err != nil {
			t.Fatalf("run error: %v", err)
		}

		// Should take the unconditional edge as fallback
		hasA := false
		hasB := false
		hasD := false
		for _, step := range got.Steps {
			if step == "A" {
				hasA = true
			}
			if step == "B" {
				hasB = true
			}
			if step == "D" {
				hasD = true
			}
		}

		if !hasA || !hasB || !hasD {
			t.Errorf("expected A, B, D in path, got %v", got.Steps)
		}
	})
}

// TestGraph_ParallelEmptyState tests parallel with empty/zero state
func TestGraph_ParallelEmptyState(t *testing.T) {
	appendHandler := func(name string) GraphHandler[*testState] {
		return func(ctx context.Context, state *testState) (*testState, error) {
			state.Steps = append(state.Steps, name)
			return state, nil
		}
	}

	g := NewGraph[*testState](WithParallel[*testState](true))
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

	// Test with empty state
	result, err := handler(context.Background(), &testState{})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}

	if len(result.Steps) == 0 {
		t.Errorf("expected non-empty result")
	}
}

// TestGraph_ParallelDeepNesting tests deeply nested parallel execution
func TestGraph_ParallelDeepNesting(t *testing.T) {
	appendHandler := func(name string) GraphHandler[*testState] {
		return func(ctx context.Context, state *testState) (*testState, error) {
			state.Steps = append(state.Steps, name)
			return state, nil
		}
	}

	g := NewGraph[*testState](WithParallel[*testState](true))

	// Create a deep nested structure: A -> (B1, B2) -> (C1, C2, C3, C4) -> D
	_ = g.AddNode("A", appendHandler("A"))
	_ = g.AddNode("B1", appendHandler("B1"))
	_ = g.AddNode("B2", appendHandler("B2"))
	_ = g.AddNode("C1", appendHandler("C1"))
	_ = g.AddNode("C2", appendHandler("C2"))
	_ = g.AddNode("C3", appendHandler("C3"))
	_ = g.AddNode("C4", appendHandler("C4"))
	_ = g.AddNode("D", appendHandler("D"))

	_ = g.AddEdge("A", "B1")
	_ = g.AddEdge("A", "B2")
	_ = g.AddEdge("B1", "C1")
	_ = g.AddEdge("B1", "C2")
	_ = g.AddEdge("B2", "C3")
	_ = g.AddEdge("B2", "C4")
	_ = g.AddEdge("C1", "D")
	_ = g.AddEdge("C2", "D")
	_ = g.AddEdge("C3", "D")
	_ = g.AddEdge("C4", "D")

	_ = g.SetEntryPoint("A")
	_ = g.SetFinishPoint("D")

	handler, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	result, err := handler(context.Background(), &testState{})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}

	// Should have A and D at least
	hasA := false
	hasD := false
	for _, step := range result.Steps {
		if step == "A" {
			hasA = true
		}
		if step == "D" {
			hasD = true
		}
	}

	if !hasA || !hasD {
		t.Errorf("expected A and D in result, got %v", result.Steps)
	}
}

// TestGraph_ParallelNoOpHandler tests handler that does nothing
func TestGraph_ParallelNoOpHandler(t *testing.T) {
	noopHandler := func(ctx context.Context, state *testState) (*testState, error) {
		// Do nothing, just return state
		return state, nil
	}

	appendHandler := func(name string) GraphHandler[*testState] {
		return func(ctx context.Context, state *testState) (*testState, error) {
			state.Steps = append(state.Steps, name)
			return state, nil
		}
	}

	g := NewGraph[*testState](WithParallel[*testState](true))
	_ = g.AddNode("A", appendHandler("A"))
	_ = g.AddNode("B", noopHandler) // No-op
	_ = g.AddNode("C", noopHandler) // No-op
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

	result, err := handler(context.Background(), &testState{})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}

	// Should complete successfully even with no-op handlers
	hasA := false
	hasD := false
	for _, step := range result.Steps {
		if step == "A" {
			hasA = true
		}
		if step == "D" {
			hasD = true
		}
	}

	if !hasA || !hasD {
		t.Errorf("expected A and D in result, got %v", result.Steps)
	}
}
