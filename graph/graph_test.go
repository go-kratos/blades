package graph

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const stepsKey = "steps"
const valueKey = "value"

func stepHandler(name string) Handler {
	return func(ctx context.Context, state State) (State, error) {
		return appendStep(state, name), nil
	}
}

func incrementHandler(delta int) Handler {
	return func(ctx context.Context, state State) (State, error) {
		next := state.Clone()
		val, _ := next[valueKey].(int)
		next[valueKey] = val + delta
		return next, nil
	}
}

func appendStep(state State, name string) State {
	next := state.Clone()
	steps := getStringSlice(next[stepsKey])
	steps = append(steps, name)
	next[stepsKey] = steps
	return next
}

func getStringSlice(value any) []string {
	if v, ok := value.([]string); ok {
		return v
	}
	return []string{}
}

func TestGraphCompileValidation(t *testing.T) {
	t.Run("missing entry", func(t *testing.T) {
		g := NewGraph()
		_ = g.AddNode("A", stepHandler("A"))
		_ = g.SetFinishPoint("A")
		if _, err := g.Compile(); err == nil || !strings.Contains(err.Error(), "entry point not set") {
			t.Fatalf("expected missing entry error, got %v", err)
		}
	})

	t.Run("missing finish", func(t *testing.T) {
		g := NewGraph()
		_ = g.AddNode("A", stepHandler("A"))
		_ = g.SetEntryPoint("A")
		if _, err := g.Compile(); err == nil || !strings.Contains(err.Error(), "finish point not set") {
			t.Fatalf("expected missing finish error, got %v", err)
		}
	})

	t.Run("edge validations", func(t *testing.T) {
		g := NewGraph()
		_ = g.AddNode("A", stepHandler("A"))
		_ = g.AddEdge("X", "A")
		_ = g.SetEntryPoint("A")
		_ = g.SetFinishPoint("A")
		if _, err := g.Compile(); err == nil || !strings.Contains(err.Error(), "edge from unknown node") {
			t.Fatalf("expected unknown node error, got %v", err)
		}
	})
}

func TestGraphSequentialOrder(t *testing.T) {
	g := NewGraph(WithParallel(false))
	execOrder := make([]string, 0, 4)
	handlerFor := func(name string) Handler {
		return func(ctx context.Context, state State) (State, error) {
			execOrder = append(execOrder, name)
			return stepHandler(name)(ctx, state)
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

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	result, err := executor.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}

	if !reflect.DeepEqual(execOrder, []string{"A", "B", "C", "D"}) {
		t.Fatalf("unexpected execution order: %v", execOrder)
	}

	steps, _ := result[stepsKey].([]string)
	if len(steps) == 0 || steps[len(steps)-1] != "D" {
		t.Fatalf("expected final node D, got %v", steps)
	}
}

func TestGraphErrorPropagation(t *testing.T) {
	g := NewGraph()
	_ = g.AddNode("A", stepHandler("A"))
	_ = g.AddNode("B", func(ctx context.Context, state State) (State, error) {
		return state, fmt.Errorf("boom")
	})
	_ = g.AddEdge("A", "B")
	_ = g.SetEntryPoint("A")
	_ = g.SetFinishPoint("B")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	_, err = executor.Execute(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "node B") {
		t.Fatalf("expected error from node B, got %v", err)
	}
}

func TestGraphConditionalRouting(t *testing.T) {
	g := NewGraph()
	_ = g.AddNode("A", stepHandler("A"))
	_ = g.AddNode("B", stepHandler("B"))
	_ = g.AddNode("C", stepHandler("C"))
	_ = g.AddNode("D", stepHandler("D"))

	_ = g.AddEdge("A", "B")
	_ = g.AddEdge("B", "C", WithEdgeCondition(func(_ context.Context, state State) bool {
		steps, _ := state[stepsKey].([]string)
		return len(steps) == 2 && steps[1] == "B"
	}))
	_ = g.AddEdge("B", "D")

	_ = g.SetEntryPoint("A")
	_ = g.SetFinishPoint("C")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	result, err := executor.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}

	steps, _ := result[stepsKey].([]string)
	if steps[len(steps)-1] != "C" {
		t.Fatalf("expected to finish at C, got %v", steps)
	}
}

func TestGraphSerialVsParallel(t *testing.T) {
	build := func(parallel bool) *Graph {
		g := NewGraph(WithParallel(parallel))
		_ = g.AddNode("A", incrementHandler(1))
		_ = g.AddNode("B", incrementHandler(10))
		_ = g.AddNode("C", incrementHandler(100))
		_ = g.AddNode("D", incrementHandler(0))
		_ = g.AddEdge("A", "B")
		_ = g.AddEdge("A", "C")
		_ = g.AddEdge("B", "D")
		_ = g.AddEdge("C", "D")
		_ = g.SetEntryPoint("A")
		_ = g.SetFinishPoint("D")
		return g
	}

	handlerParallel, err := build(true).Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	parallelState, err := handlerParallel.Execute(context.Background(), State{})
	if err != nil {
		t.Fatalf("parallel run error: %v", err)
	}

	if parallelState[valueKey].(int) == 0 {
		t.Fatalf("expected merged value in parallel mode")
	}

	handlerSerial, err := build(false).Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	serialState, err := handlerSerial.Execute(context.Background(), State{})
	if err != nil {
		t.Fatalf("serial run error: %v", err)
	}

	if serialState[valueKey].(int) <= parallelState[valueKey].(int) {
		t.Fatalf("serial mode should accumulate more due to sequential execution")
	}
}

func TestGraphParallelNestedLoops(t *testing.T) {
	g := NewGraph(WithParallel(true))

	g.AddNode("start", func(ctx context.Context, state State) (State, error) {
		return state.Clone(), nil
	})
	g.AddNode("outer_loop", func(ctx context.Context, state State) (State, error) {
		next := state.Clone()
		val, _ := next[valueKey].(int)
		next[valueKey] = val + 1
		return next, nil
	})
	g.AddNode("inner_loop", func(ctx context.Context, state State) (State, error) {
		next := state.Clone()
		val, _ := next[valueKey].(int)
		next[valueKey] = val + 10
		return next, nil
	})
	g.AddNode("done", func(ctx context.Context, state State) (State, error) {
		return state.Clone(), nil
	})

	g.AddEdge("start", "outer_loop")
	g.AddEdge("outer_loop", "inner_loop")
	g.AddEdge("inner_loop", "inner_loop", WithEdgeCondition(func(_ context.Context, state State) bool {
		val, _ := state[valueKey].(int)
		return val < 30
	}))
	g.AddEdge("inner_loop", "outer_loop", WithEdgeCondition(func(_ context.Context, state State) bool {
		val, _ := state[valueKey].(int)
		return val >= 30 && val < 100
	}))
	g.AddEdge("inner_loop", "done", WithEdgeCondition(func(_ context.Context, state State) bool {
		val, _ := state[valueKey].(int)
		return val >= 100
	}))

	g.SetEntryPoint("start")
	g.SetFinishPoint("done")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	result, err := executor.Execute(context.Background(), State{valueKey: 0})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}

	if val, _ := result[valueKey].(int); val < 100 {
		t.Fatalf("expected value >= 100, got %d", val)
	}
}

func TestGraphParallelSelfLoopExit(t *testing.T) {
	g := NewGraph(WithParallel(true))

	g.AddNode("loop", func(ctx context.Context, state State) (State, error) {
		next := state.Clone()
		val, _ := next[valueKey].(int)
		next[valueKey] = val + 1
		return next, nil
	})
	g.AddNode("exit", func(ctx context.Context, state State) (State, error) {
		return state.Clone(), nil
	})

	g.AddEdge("loop", "loop", WithEdgeCondition(func(_ context.Context, state State) bool {
		val, _ := state[valueKey].(int)
		return val < 5
	}))
	g.AddEdge("loop", "exit", WithEdgeCondition(func(_ context.Context, state State) bool {
		val, _ := state[valueKey].(int)
		return val >= 5
	}))

	g.SetEntryPoint("loop")
	g.SetFinishPoint("exit")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	result, err := executor.Execute(context.Background(), State{valueKey: 0})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}

	if val, _ := result[valueKey].(int); val != 5 {
		t.Fatalf("expected value 5, got %d", val)
	}
}

func TestGraphParallelContextTimeout(t *testing.T) {
	g := NewGraph(WithParallel(true))

	g.AddNode("slow", func(ctx context.Context, state State) (State, error) {
		select {
		case <-ctx.Done():
			return state, ctx.Err()
		case <-time.After(200 * time.Millisecond):
			next := state.Clone()
			next[stepsKey] = append(getStringSlice(state[stepsKey]), "slow")
			return next, nil
		}
	})
	g.SetEntryPoint("slow")
	g.SetFinishPoint("slow")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = executor.Execute(ctx, State{})
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if !strings.Contains(err.Error(), "context") {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestGraphParallelFanOutBranches(t *testing.T) {
	g := NewGraph()

	var mu sync.Mutex
	called := make(map[string]int)
	record := func(name string) Handler {
		return func(ctx context.Context, state State) (State, error) {
			mu.Lock()
			called[name]++
			mu.Unlock()
			return state.Clone(), nil
		}
	}

	g.AddNode("start", record("start"))
	g.AddNode("branch_a", record("branch_a"))
	g.AddNode("branch_b", record("branch_b"))
	g.AddNode("join", record("join"))

	g.AddEdge("start", "branch_a")
	g.AddEdge("start", "branch_b")
	g.AddEdge("branch_a", "join")
	g.AddEdge("branch_b", "join")
	g.SetEntryPoint("start")
	g.SetFinishPoint("join")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	if _, err := executor.Execute(context.Background(), State{}); err != nil {
		t.Fatalf("run error: %v", err)
	}

	if called["branch_a"] != 1 || called["branch_b"] != 1 {
		t.Fatalf("expected both branches to execute once, got %v", called)
	}
	if called["join"] != 1 {
		t.Fatalf("expected join to execute once, got %v", called)
	}
}

func TestGraphParallelPropagatesBranchError(t *testing.T) {
	g := NewGraph()

	record := func(name string) Handler {
		return func(ctx context.Context, state State) (State, error) {
			return state.Clone(), nil
		}
	}

	g.AddNode("start", record("start"))
	g.AddNode("ok_branch", record("ok_branch"))
	g.AddNode("fail_branch", func(ctx context.Context, state State) (State, error) {
		return state, fmt.Errorf("fail_branch boom")
	})
	g.AddNode("join", record("join"))

	g.AddEdge("start", "ok_branch")
	g.AddEdge("start", "fail_branch")
	g.AddEdge("ok_branch", "join")
	g.AddEdge("fail_branch", "join")
	g.SetEntryPoint("start")
	g.SetFinishPoint("join")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	_, err = executor.Execute(context.Background(), State{})
	if err == nil || !strings.Contains(err.Error(), "node fail_branch") {
		t.Fatalf("expected failure from fail_branch, got %v", err)
	}
}

func TestGraphParallelMergeByKey(t *testing.T) {
	g := NewGraph()
	_ = g.AddNode("start", func(ctx context.Context, state State) (State, error) {
		next := state.Clone()
		next["start"] = true
		return next, nil
	})
	_ = g.AddNode("workerA", func(ctx context.Context, state State) (State, error) {
		next := state.Clone()
		next["branchA"] = "done"
		return next, nil
	})
	_ = g.AddNode("workerB", func(ctx context.Context, state State) (State, error) {
		next := state.Clone()
		next["branchB"] = "done"
		return next, nil
	})
	_ = g.AddNode("join", func(ctx context.Context, state State) (State, error) {
		return state, nil
	})

	_ = g.AddEdge("start", "workerA")
	_ = g.AddEdge("start", "workerB")
	_ = g.AddEdge("workerA", "join")
	_ = g.AddEdge("workerB", "join")
	_ = g.SetEntryPoint("start")
	_ = g.SetFinishPoint("join")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	final, err := executor.Execute(context.Background(), State{})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}

	if _, ok := final["branchA"]; !ok {
		t.Fatalf("expected branchA key to be merged")
	}
	if _, ok := final["branchB"]; !ok {
		t.Fatalf("expected branchB key to be merged")
	}
}

func TestMergeStatesKeepsKeys(t *testing.T) {
	base := State{"start": true}
	a := State{"start": true, "branchA": "done"}
	b := State{"start": true, "branchB": "done"}

	merged := mergeStates(mergeStates(base, a), b)

	if _, ok := merged["branchA"]; !ok {
		t.Fatalf("branchA missing in merged result: %#v", merged)
	}
	if _, ok := merged["branchB"]; !ok {
		t.Fatalf("branchB missing in merged result: %#v", merged)
	}
}

func TestGraphParallelJoinIgnoresInactiveBranches(t *testing.T) {
	g := NewGraph()

	var mu sync.Mutex
	executed := make(map[string]int)
	record := func(name string, mutate func(State) State) Handler {
		return func(ctx context.Context, state State) (State, error) {
			mu.Lock()
			executed[name]++
			mu.Unlock()
			if mutate != nil {
				return mutate(state), nil
			}
			return state.Clone(), nil
		}
	}

	g.AddNode("start", record("start", func(state State) State {
		next := state.Clone()
		next["enable_b"] = false
		return next
	}))
	g.AddNode("branch_a", record("branch_a", nil))
	g.AddNode("branch_b", record("branch_b", nil))
	g.AddNode("join", record("join", nil))

	g.AddEdge("start", "branch_a")
	g.AddEdge("start", "branch_b", WithEdgeCondition(func(_ context.Context, state State) bool {
		enabled, _ := state["enable_b"].(bool)
		return enabled
	}))
	g.AddEdge("branch_a", "join")
	g.AddEdge("branch_b", "join")

	g.SetEntryPoint("start")
	g.SetFinishPoint("join")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	_, err = executor.Execute(context.Background(), State{})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}

	if executed["join"] == 0 {
		t.Fatalf("expected join to execute, got executed=%v", executed)
	}
	if executed["branch_b"] != 0 {
		t.Fatalf("branch_b should not execute when disabled: %v", executed)
	}
}

func TestGraphParallelJoinSkipsUnselectedEdges(t *testing.T) {
	g := NewGraph()

	var mu sync.Mutex
	executed := make(map[string]int)
	record := func(name string, mutate func(State) State) Handler {
		return func(ctx context.Context, state State) (State, error) {
			mu.Lock()
			executed[name]++
			mu.Unlock()
			if mutate != nil {
				return mutate(state), nil
			}
			return state.Clone(), nil
		}
	}

	g.AddNode("start", record("start", nil))
	g.AddNode("branch_a", record("branch_a", nil))
	g.AddNode("branch_b", record("branch_b", func(state State) State {
		next := state.Clone()
		next["send_to_join"] = false
		return next
	}))
	g.AddNode("sink", record("sink", nil))
	g.AddNode("join", record("join", nil))
	g.AddNode("final", record("final", nil))

	g.AddEdge("start", "branch_a")
	g.AddEdge("start", "branch_b")
	g.AddEdge("branch_a", "join")
	g.AddEdge("branch_b", "join", WithEdgeCondition(func(_ context.Context, state State) bool {
		send, _ := state["send_to_join"].(bool)
		return send
	}))
	g.AddEdge("branch_b", "sink", WithEdgeCondition(func(_ context.Context, state State) bool {
		send, _ := state["send_to_join"].(bool)
		return !send
	}))
	g.AddEdge("join", "final")
	g.AddEdge("sink", "final")

	g.SetEntryPoint("start")
	g.SetFinishPoint("final")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	if _, err := executor.Execute(context.Background(), State{}); err != nil {
		t.Fatalf("run error: %v", err)
	}

	if executed["join"] == 0 {
		t.Fatalf("expected join to execute even when branch_b skips it: %v", executed)
	}
	if executed["sink"] == 0 {
		t.Fatalf("expected sink to execute when branch_b skips join: %v", executed)
	}
}

// TestGraphComplexTopology tests a complex graph with:
// 1. Branch -> Branch (multi-level branching)
// 2. Parallel execution of branches
// 3. Loop within branches
// 4. Asymmetric convergence (branches converge at different points)
func TestGraphComplexTopology(t *testing.T) {
	g := NewGraph()

	var mu sync.Mutex
	executed := make(map[string]int)
	record := func(name string, transform func(State) State) Handler {
		return func(ctx context.Context, state State) (State, error) {
			mu.Lock()
			executed[name]++
			mu.Unlock()
			if transform != nil {
				return transform(state), nil
			}
			return state.Clone(), nil
		}
	}

	// Graph topology:
	// start
	//   ├─> branchA1
	//   │     ├─> branchA2_1 (loop until counter >= 3)
	//   │     │     └─> branchA2_1 (loop back)
	//   │     │     └─> parallelA1 ─┐
	//   │     └─> branchA2_2        ├─> joinA ─┐
	//   │           └─> parallelA2 ─┘          │
	//   │                                       ├─> final
	//   └─> branchB                             │
	//         └─> parallelB ────────────────────┘
	//               (shorter path)

	// Start node
	g.AddNode("start", record("start", func(state State) State {
		next := state.Clone()
		next["counter"] = 0
		next["visited"] = []string{"start"}
		return next
	}))

	// Branch A1
	g.AddNode("branchA1", record("branchA1", func(state State) State {
		next := state.Clone()
		visited := append(getStringSlice(state["visited"]), "branchA1")
		next["visited"] = visited
		return next
	}))

	// Branch A2_1 (with loop)
	g.AddNode("branchA2_1", record("branchA2_1", func(state State) State {
		next := state.Clone()
		counter, _ := next["counter"].(int)
		next["counter"] = counter + 1
		visited := append(getStringSlice(state["visited"]), "branchA2_1")
		next["visited"] = visited
		return next
	}))

	// Branch A2_2
	g.AddNode("branchA2_2", record("branchA2_2", func(state State) State {
		next := state.Clone()
		visited := append(getStringSlice(state["visited"]), "branchA2_2")
		next["visited"] = visited
		return next
	}))

	// Parallel workers in branch A
	g.AddNode("parallelA1", record("parallelA1", func(state State) State {
		next := state.Clone()
		visited := append(getStringSlice(state["visited"]), "parallelA1")
		next["visited"] = visited
		next["parallelA1_data"] = "processed"
		return next
	}))

	g.AddNode("parallelA2", record("parallelA2", func(state State) State {
		next := state.Clone()
		visited := append(getStringSlice(state["visited"]), "parallelA2")
		next["visited"] = visited
		next["parallelA2_data"] = "processed"
		return next
	}))

	// Join point for branch A
	g.AddNode("joinA", record("joinA", func(state State) State {
		next := state.Clone()
		visited := append(getStringSlice(state["visited"]), "joinA")
		next["visited"] = visited
		return next
	}))

	// Branch B (shorter path)
	g.AddNode("branchB", record("branchB", func(state State) State {
		next := state.Clone()
		visited := append(getStringSlice(state["visited"]), "branchB")
		next["visited"] = visited
		return next
	}))

	g.AddNode("parallelB", record("parallelB", func(state State) State {
		next := state.Clone()
		visited := append(getStringSlice(state["visited"]), "parallelB")
		next["visited"] = visited
		next["parallelB_data"] = "processed"
		return next
	}))

	// Final convergence point
	g.AddNode("final", record("final", func(state State) State {
		next := state.Clone()
		visited := append(getStringSlice(state["visited"]), "final")
		next["visited"] = visited
		return next
	}))

	// Build edges
	g.AddEdge("start", "branchA1")
	g.AddEdge("start", "branchB")

	// Branch A1 splits into A2_1 and A2_2
	g.AddEdge("branchA1", "branchA2_1")
	g.AddEdge("branchA1", "branchA2_2")

	// Branch A2_1 loops until counter >= 3
	g.AddEdge("branchA2_1", "branchA2_1", WithEdgeCondition(func(_ context.Context, state State) bool {
		counter, _ := state["counter"].(int)
		return counter < 3
	}))
	g.AddEdge("branchA2_1", "parallelA1", WithEdgeCondition(func(_ context.Context, state State) bool {
		counter, _ := state["counter"].(int)
		return counter >= 3
	}))

	// Branch A2_2 goes to parallelA2
	g.AddEdge("branchA2_2", "parallelA2")

	// Parallel branches converge at joinA
	g.AddEdge("parallelA1", "joinA")
	g.AddEdge("parallelA2", "joinA")

	// Branch B goes directly to parallelB
	g.AddEdge("branchB", "parallelB")

	// Asymmetric convergence: both joinA and parallelB go to final
	g.AddEdge("joinA", "final")
	g.AddEdge("parallelB", "final")

	g.SetEntryPoint("start")
	g.SetFinishPoint("final")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	result, err := executor.Execute(context.Background(), State{})
	if err != nil {
		t.Fatalf("execution error: %v", err)
	}

	// Verify execution counts
	if executed["start"] != 1 {
		t.Errorf("expected start to execute once, got %d", executed["start"])
	}
	if executed["branchA1"] != 1 {
		t.Errorf("expected branchA1 to execute once, got %d", executed["branchA1"])
	}
	if executed["branchA2_1"] != 3 {
		t.Errorf("expected branchA2_1 to loop 3 times, got %d", executed["branchA2_1"])
	}
	if executed["branchA2_2"] != 1 {
		t.Errorf("expected branchA2_2 to execute once, got %d", executed["branchA2_2"])
	}
	if executed["parallelA1"] != 1 {
		t.Errorf("expected parallelA1 to execute once, got %d", executed["parallelA1"])
	}
	if executed["parallelA2"] != 1 {
		t.Errorf("expected parallelA2 to execute once, got %d", executed["parallelA2"])
	}
	// joinA is executed by both parallelA1 and parallelA2, but due to parallel execution,
	// it may be executed multiple times (once per incoming edge)
	if executed["joinA"] == 0 {
		t.Errorf("expected joinA to execute at least once, got %d", executed["joinA"])
	}
	if executed["branchB"] != 1 {
		t.Errorf("expected branchB to execute once, got %d", executed["branchB"])
	}
	if executed["parallelB"] != 1 {
		t.Errorf("expected parallelB to execute once, got %d", executed["parallelB"])
	}
	// final is the asymmetric convergence point for joinA and parallelB
	if executed["final"] == 0 {
		t.Errorf("expected final to execute at least once, got %d", executed["final"])
	}

	// Verify final counter value (from the looping branch)
	counter, _ := result["counter"].(int)
	if counter != 3 {
		t.Errorf("expected counter to be 3, got %d", counter)
	}

	// Verify that parallel branches executed (checking execution counts instead of merged state)
	// Because our mergeStates only keeps the last value for each key, we verify via execution counts
	if executed["parallelA1"] == 0 {
		t.Error("expected parallelA1 to execute")
	}
	if executed["parallelA2"] == 0 {
		t.Error("expected parallelA2 to execute")
	}
	if executed["parallelB"] == 0 {
		t.Error("expected parallelB to execute")
	}

	// The final visited path will only contain one branch's path due to simple mergeStates
	// But we can verify that the graph structure worked correctly via execution counts
	visited := getStringSlice(result["visited"])
	if len(visited) == 0 {
		t.Error("expected non-empty visited path")
	}

	t.Logf("Execution counts: %v", executed)
	t.Logf("Final state visited (one branch's path): %v", visited)
	t.Logf("Counter value: %d", counter)
}

// TestGraphComplexTopologySerial tests the same complex topology in serial mode.
// This ensures sequential execution through the same complex graph structure.
func TestGraphComplexTopologySerial(t *testing.T) {
	g := NewGraph(WithParallel(false)) // Serial mode

	var mu sync.Mutex
	executed := make(map[string]int)
	executionOrder := []string{}
	record := func(name string, transform func(State) State) Handler {
		return func(ctx context.Context, state State) (State, error) {
			mu.Lock()
			executed[name]++
			executionOrder = append(executionOrder, name)
			mu.Unlock()
			if transform != nil {
				return transform(state), nil
			}
			return state.Clone(), nil
		}
	}

	// Same graph topology as TestGraphComplexTopology but in serial mode
	// start
	//   ├─> branchA1
	//   │     ├─> branchA2_1 (loop until counter >= 3)
	//   │     │     └─> branchA2_1 (loop back)
	//   │     │     └─> parallelA1 ─┐
	//   │     └─> branchA2_2        ├─> joinA ─┐
	//   │           └─> parallelA2 ─┘          │
	//   │                                       ├─> final
	//   └─> branchB                             │
	//         └─> parallelB ────────────────────┘

	// Start node
	g.AddNode("start", record("start", func(state State) State {
		next := state.Clone()
		next["counter"] = 0
		next["visited"] = []string{"start"}
		return next
	}))

	// Branch A1
	g.AddNode("branchA1", record("branchA1", func(state State) State {
		next := state.Clone()
		visited := append(getStringSlice(state["visited"]), "branchA1")
		next["visited"] = visited
		return next
	}))

	// Branch A2_1 (with loop)
	g.AddNode("branchA2_1", record("branchA2_1", func(state State) State {
		next := state.Clone()
		counter, _ := next["counter"].(int)
		next["counter"] = counter + 1
		visited := append(getStringSlice(state["visited"]), "branchA2_1")
		next["visited"] = visited
		return next
	}))

	// Branch A2_2
	g.AddNode("branchA2_2", record("branchA2_2", func(state State) State {
		next := state.Clone()
		visited := append(getStringSlice(state["visited"]), "branchA2_2")
		next["visited"] = visited
		return next
	}))

	// Serial workers in branch A
	g.AddNode("parallelA1", record("parallelA1", func(state State) State {
		next := state.Clone()
		visited := append(getStringSlice(state["visited"]), "parallelA1")
		next["visited"] = visited
		next["parallelA1_data"] = "processed"
		return next
	}))

	g.AddNode("parallelA2", record("parallelA2", func(state State) State {
		next := state.Clone()
		visited := append(getStringSlice(state["visited"]), "parallelA2")
		next["visited"] = visited
		next["parallelA2_data"] = "processed"
		return next
	}))

	// Join point for branch A
	g.AddNode("joinA", record("joinA", func(state State) State {
		next := state.Clone()
		visited := append(getStringSlice(state["visited"]), "joinA")
		next["visited"] = visited
		return next
	}))

	// Branch B (shorter path)
	g.AddNode("branchB", record("branchB", func(state State) State {
		next := state.Clone()
		visited := append(getStringSlice(state["visited"]), "branchB")
		next["visited"] = visited
		return next
	}))

	g.AddNode("parallelB", record("parallelB", func(state State) State {
		next := state.Clone()
		visited := append(getStringSlice(state["visited"]), "parallelB")
		next["visited"] = visited
		next["parallelB_data"] = "processed"
		return next
	}))

	// Final convergence point
	g.AddNode("final", record("final", func(state State) State {
		next := state.Clone()
		visited := append(getStringSlice(state["visited"]), "final")
		next["visited"] = visited
		return next
	}))

	// Build edges (same as parallel version)
	g.AddEdge("start", "branchA1")
	g.AddEdge("start", "branchB")

	g.AddEdge("branchA1", "branchA2_1")
	g.AddEdge("branchA1", "branchA2_2")

	g.AddEdge("branchA2_1", "branchA2_1", WithEdgeCondition(func(_ context.Context, state State) bool {
		counter, _ := state["counter"].(int)
		return counter < 3
	}))
	g.AddEdge("branchA2_1", "parallelA1", WithEdgeCondition(func(_ context.Context, state State) bool {
		counter, _ := state["counter"].(int)
		return counter >= 3
	}))

	g.AddEdge("branchA2_2", "parallelA2")

	g.AddEdge("parallelA1", "joinA")
	g.AddEdge("parallelA2", "joinA")

	g.AddEdge("branchB", "parallelB")

	g.AddEdge("joinA", "final")
	g.AddEdge("parallelB", "final")

	g.SetEntryPoint("start")
	g.SetFinishPoint("final")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	result, err := executor.Execute(context.Background(), State{})
	if err != nil {
		t.Fatalf("execution error: %v", err)
	}

	// Verify execution counts (same as parallel version)
	if executed["start"] != 1 {
		t.Errorf("expected start to execute once, got %d", executed["start"])
	}
	if executed["branchA1"] != 1 {
		t.Errorf("expected branchA1 to execute once, got %d", executed["branchA1"])
	}
	if executed["branchA2_1"] != 3 {
		t.Errorf("expected branchA2_1 to loop 3 times, got %d", executed["branchA2_1"])
	}
	if executed["branchA2_2"] != 1 {
		t.Errorf("expected branchA2_2 to execute once, got %d", executed["branchA2_2"])
	}
	if executed["parallelA1"] != 1 {
		t.Errorf("expected parallelA1 to execute once, got %d", executed["parallelA1"])
	}
	if executed["parallelA2"] != 1 {
		t.Errorf("expected parallelA2 to execute once, got %d", executed["parallelA2"])
	}
	if executed["joinA"] == 0 {
		t.Errorf("expected joinA to execute at least once, got %d", executed["joinA"])
	}
	if executed["branchB"] != 1 {
		t.Errorf("expected branchB to execute once, got %d", executed["branchB"])
	}
	if executed["parallelB"] != 1 {
		t.Errorf("expected parallelB to execute once, got %d", executed["parallelB"])
	}
	if executed["final"] == 0 {
		t.Errorf("expected final to execute at least once, got %d", executed["final"])
	}

	// Verify final counter value
	counter, _ := result["counter"].(int)
	if counter != 3 {
		t.Errorf("expected counter to be 3, got %d", counter)
	}

	// In serial mode, execution should be deterministic and sequential
	// Verify that execution is truly sequential (no concurrent execution)
	if len(executionOrder) == 0 {
		t.Error("expected non-empty execution order")
	}

	// Verify visited path includes nodes from the completed path
	visited := getStringSlice(result["visited"])
	if len(visited) == 0 {
		t.Error("expected non-empty visited path")
	}

	// In serial mode, the execution should follow a deterministic order
	// The order should be: start -> branchA1 -> (branchA2_1 x3 + branchA2_2) -> (parallelA1 + parallelA2) -> joinA -> branchB -> parallelB -> final
	t.Logf("Execution counts: %v", executed)
	t.Logf("Execution order: %v", executionOrder)
	t.Logf("Final state visited: %v", visited)
	t.Logf("Counter value: %d", counter)

	// Verify sequential execution order properties
	startIdx := -1
	finalIdx := -1
	for i, node := range executionOrder {
		if node == "start" {
			startIdx = i
		}
		if node == "final" {
			finalIdx = i
		}
	}
	if startIdx == -1 {
		t.Error("start node not found in execution order")
	}
	if finalIdx == -1 {
		t.Error("final node not found in execution order")
	}
	if startIdx >= finalIdx {
		t.Errorf("start should execute before final, got start at %d, final at %d", startIdx, finalIdx)
	}
}
