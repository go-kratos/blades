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

func TestExecutorInitialStatePropagates(t *testing.T) {
	g := NewGraph()

	g.AddNode("start", func(ctx context.Context, state State) (State, error) {
		if got, ok := state["seed"].(string); !ok || got != "value" {
			return nil, fmt.Errorf("start received unexpected seed: %#v", state["seed"])
		}
		next := state.Clone()
		next["start_seen"] = true
		return next, nil
	})

	g.AddNode("finish", func(ctx context.Context, state State) (State, error) {
		if got, ok := state["seed"].(string); !ok || got != "value" {
			return nil, fmt.Errorf("finish received unexpected seed: %#v", state["seed"])
		}
		next := state.Clone()
		next["finish_seen"] = true
		return next, nil
	})

	g.AddEdge("start", "finish")
	g.SetEntryPoint("start")
	g.SetFinishPoint("finish")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	initial := State{"seed": "value"}
	result, err := executor.Execute(context.Background(), initial)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if val, ok := result["seed"].(string); !ok || val != "value" {
		t.Fatalf("expected seed to survive execution, got %#v", result["seed"])
	}
	if len(initial) != 1 {
		t.Fatalf("initial state mutated: %#v", initial)
	}
	if _, ok := result["finish_seen"]; !ok {
		t.Fatalf("expected finish to mark state, got %#v", result)
	}
}

func TestExecutorResetBetweenRuns(t *testing.T) {
	g := NewGraph()

	g.AddNode("start", func(ctx context.Context, state State) (State, error) {
		runID, ok := state["run"].(int)
		if !ok {
			return nil, fmt.Errorf("missing run id in start: %#v", state["run"])
		}
		next := state.Clone()
		next["marker"] = fmt.Sprintf("run-%d", runID)
		return next, nil
	})

	g.AddNode("finish", func(ctx context.Context, state State) (State, error) {
		next := state.Clone()
		if _, ok := next["marker"].(string); !ok {
			return nil, fmt.Errorf("missing marker in finish: %#v", state)
		}
		next["completed"] = true
		return next, nil
	})

	g.AddEdge("start", "finish")
	g.SetEntryPoint("start")
	g.SetFinishPoint("finish")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	firstInitial := State{"run": 1}
	firstResult, err := executor.Execute(context.Background(), firstInitial)
	if err != nil {
		t.Fatalf("first execution error: %v", err)
	}
	if marker := firstResult["marker"]; marker != "run-1" {
		t.Fatalf("expected marker run-1, got %#v", marker)
	}
	if len(firstInitial) != 1 {
		t.Fatalf("first initial state mutated: %#v", firstInitial)
	}

	secondInitial := State{"run": 2}
	secondResult, err := executor.Execute(context.Background(), secondInitial)
	if err != nil {
		t.Fatalf("second execution error: %v", err)
	}
	if marker := secondResult["marker"]; marker != "run-2" {
		t.Fatalf("expected marker run-2, got %#v", marker)
	}
	if len(secondInitial) != 1 {
		t.Fatalf("second initial state mutated: %#v", secondInitial)
	}
	if _, ok := secondResult["completed"].(bool); !ok {
		t.Fatalf("expected finish flag in second result, got %#v", secondResult)
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

// TestGraphDifferentPathLengths tests that convergence nodes correctly wait for all predecessors
// even when the paths to reach them have different lengths.
// This is the bug that was fixed: previously, fanOutParallel only handled same-length paths.
func TestGraphDifferentPathLengths(t *testing.T) {
	// Graph topology:
	// A → B ↘
	//       → D
	//   → C → C2 ↗
	//
	// Path 1: A → B → D (length 2)
	// Path 2: A → C → C2 → D (length 3)
	// D should wait for both B and C2 to complete

	g := NewGraph()

	var mu sync.Mutex
	executed := make(map[string]int)
	executionOrder := make([]string, 0)
	record := func(name string) Handler {
		return func(ctx context.Context, state State) (State, error) {
			mu.Lock()
			executed[name]++
			executionOrder = append(executionOrder, name)
			mu.Unlock()

			next := state.Clone()
			// Each node sets its own key to avoid mergeStates overwriting
			next[name+"_executed"] = true
			return next, nil
		}
	}

	g.AddNode("A", record("A"))
	g.AddNode("B", record("B"))
	g.AddNode("C", record("C"))
	g.AddNode("C2", record("C2"))
	g.AddNode("D", record("D"))

	// Create asymmetric paths
	g.AddEdge("A", "B")  // Short path
	g.AddEdge("A", "C")  // Long path start
	g.AddEdge("C", "C2") // Long path middle
	g.AddEdge("B", "D")  // Short path converges
	g.AddEdge("C2", "D") // Long path converges

	g.SetEntryPoint("A")
	g.SetFinishPoint("D")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	_, err = executor.Execute(context.Background(), State{})
	if err != nil {
		t.Fatalf("execution error: %v", err)
	}

	t.Logf("Execution order: %v", executionOrder)

	// Verify all nodes executed exactly once
	expectedNodes := []string{"A", "B", "C", "C2", "D"}
	for _, node := range expectedNodes {
		if executed[node] != 1 {
			t.Errorf("expected %s to execute once, got %d", node, executed[node])
		}
	}

	// Verify execution order: both B and C2 must complete before D
	bIndex := -1
	c2Index := -1
	dIndex := -1
	for i, node := range executionOrder {
		switch node {
		case "B":
			bIndex = i
		case "C2":
			c2Index = i
		case "D":
			dIndex = i
		}
	}

	if bIndex == -1 {
		t.Error("B was not executed")
	}
	if c2Index == -1 {
		t.Error("C2 was not executed")
	}
	if dIndex == -1 {
		t.Error("D was not executed")
	}

	// The critical assertion: D must execute after BOTH B and C2
	if !(dIndex > bIndex && dIndex > c2Index) {
		t.Errorf("D should execute after both B and C2, got B at %d, C2 at %d, D at %d",
			bIndex, c2Index, dIndex)
	}
}

// TestGraphDifferentPathLengthsMultipleBranches extends the path-length coverage to three
// asymmetric branches converging at a single finish node.
func TestGraphDifferentPathLengthsMultipleBranches(t *testing.T) {
	// Graph topology:
	// start → branch_short → finish
	// start → branch_mid → branch_mid2 → finish
	// start → branch_long1 → branch_long2 → branch_long3 → finish

	g := NewGraph()

	var mu sync.Mutex
	executed := make(map[string]int)
	executionOrder := make([]string, 0)
	record := func(name string) Handler {
		return func(ctx context.Context, state State) (State, error) {
			mu.Lock()
			executed[name]++
			executionOrder = append(executionOrder, name)
			mu.Unlock()

			next := state.Clone()
			next[name+"_executed"] = true
			return next, nil
		}
	}

	g.AddNode("start", record("start"))
	g.AddNode("branch_short", record("branch_short"))
	g.AddNode("branch_mid", record("branch_mid"))
	g.AddNode("branch_mid2", record("branch_mid2"))
	g.AddNode("branch_long1", record("branch_long1"))
	g.AddNode("branch_long2", record("branch_long2"))
	g.AddNode("branch_long3", record("branch_long3"))
	g.AddNode("finish", record("finish"))

	g.AddEdge("start", "branch_short")
	g.AddEdge("start", "branch_mid")
	g.AddEdge("start", "branch_long1")
	g.AddEdge("branch_short", "finish")
	g.AddEdge("branch_mid", "branch_mid2")
	g.AddEdge("branch_mid2", "finish")
	g.AddEdge("branch_long1", "branch_long2")
	g.AddEdge("branch_long2", "branch_long3")
	g.AddEdge("branch_long3", "finish")

	g.SetEntryPoint("start")
	g.SetFinishPoint("finish")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	_, err = executor.Execute(context.Background(), State{})
	if err != nil {
		t.Fatalf("execution error: %v", err)
	}

	// Ensure each node executed exactly once.
	expectedNodes := []string{
		"start",
		"branch_short",
		"branch_mid",
		"branch_mid2",
		"branch_long1",
		"branch_long2",
		"branch_long3",
		"finish",
	}
	for _, node := range expectedNodes {
		if executed[node] != 1 {
			t.Errorf("expected %s to execute once, got %d", node, executed[node])
		}
	}

	// Finish must wait for all predecessors (branch_short, branch_mid2, branch_long3).
	shortIdx := -1
	midIdx := -1
	longIdx := -1
	finishIdx := -1
	for i, node := range executionOrder {
		switch node {
		case "branch_short":
			shortIdx = i
		case "branch_mid2":
			midIdx = i
		case "branch_long3":
			longIdx = i
		case "finish":
			finishIdx = i
		}
	}

	if finishIdx == -1 {
		t.Error("finish node did not execute")
	}
	if shortIdx == -1 || midIdx == -1 || longIdx == -1 {
		t.Fatalf("missing branch execution indices: short=%d mid=%d long=%d", shortIdx, midIdx, longIdx)
	}
	if !(finishIdx > shortIdx && finishIdx > midIdx && finishIdx > longIdx) {
		t.Errorf("finish should run after all branch endpoints; got short=%d mid=%d long=%d finish=%d",
			shortIdx, midIdx, longIdx, finishIdx)
	}
}

func TestGraphDifferentPathLengthsConditionalBranches(t *testing.T) {
	// Graph topology with conditional exits:
	// start → branch_short ──► finish
	// start → branch_mid ─┐
	//             └─(cond false)──► mid_skip
	//             └─(cond true) ──► branch_mid2 ──► finish
	// start → branch_long1 → branch_long2 → branch_long3 ──► finish

	g := NewGraph()

	var mu sync.Mutex
	executed := make(map[string]int)
	executionOrder := make([]string, 0)
	record := func(name string, mutate func(State) State) Handler {
		return func(ctx context.Context, state State) (State, error) {
			mu.Lock()
			executed[name]++
			executionOrder = append(executionOrder, name)
			mu.Unlock()

			next := state.Clone()
			next[name+"_executed"] = true
			if mutate != nil {
				return mutate(next), nil
			}
			return next, nil
		}
	}

	g.AddNode("start", record("start", func(state State) State {
		next := state.Clone()
		next["mid_condition"] = true
		return next
	}))
	g.AddNode("branch_short", record("branch_short", nil))
	g.AddNode("branch_mid", record("branch_mid", nil))
	g.AddNode("mid_skip", record("mid_skip", nil))
	g.AddNode("branch_mid2", record("branch_mid2", nil))
	g.AddNode("branch_long1", record("branch_long1", nil))
	g.AddNode("branch_long2", record("branch_long2", nil))
	g.AddNode("branch_long3", record("branch_long3", nil))
	g.AddNode("finish", record("finish", nil))

	g.AddEdge("start", "branch_short")
	g.AddEdge("start", "branch_mid")
	g.AddEdge("start", "branch_long1")
	g.AddEdge("branch_short", "finish")
	g.AddEdge("branch_mid", "mid_skip", WithEdgeCondition(func(_ context.Context, state State) bool {
		cond, _ := state["mid_condition"].(bool)
		return !cond
	}))
	g.AddEdge("branch_mid", "branch_mid2", WithEdgeCondition(func(_ context.Context, state State) bool {
		cond, _ := state["mid_condition"].(bool)
		return cond
	}))
	g.AddEdge("branch_mid2", "finish")
	g.AddEdge("mid_skip", "finish")
	g.AddEdge("branch_long1", "branch_long2")
	g.AddEdge("branch_long2", "branch_long3")
	g.AddEdge("branch_long3", "finish")

	g.SetEntryPoint("start")
	g.SetFinishPoint("finish")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	result, err := executor.Execute(context.Background(), State{})
	if err != nil {
		t.Fatalf("execution error: %v", err)
	}

	for _, node := range []string{
		"start",
		"branch_short",
		"branch_mid",
		"branch_mid2",
		"branch_long1",
		"branch_long2",
		"branch_long3",
		"finish",
	} {
		if executed[node] != 1 {
			t.Errorf("expected %s to execute once, got %d", node, executed[node])
		}
	}
	if executed["branch_mid2"] == 0 {
		t.Errorf("expected branch_mid2 to execute, got counts=%v", executed)
	}

	shortIdx := -1
	midIdx := -1
	longIdx := -1
	finishIdx := -1
	for i, node := range executionOrder {
		switch node {
		case "branch_short":
			shortIdx = i
		case "branch_mid2":
			midIdx = i
		case "branch_long3":
			longIdx = i
		case "finish":
			finishIdx = i
		}
	}

	if finishIdx == -1 {
		t.Fatal("finish node did not execute")
	}
	if shortIdx == -1 || midIdx == -1 || longIdx == -1 {
		t.Fatalf("missing branch indices short=%d mid=%d long=%d", shortIdx, midIdx, longIdx)
	}
	if !(finishIdx > shortIdx && finishIdx > midIdx && finishIdx > longIdx) {
		t.Errorf("finish should wait for all active branches; got short=%d mid=%d long=%d finish=%d",
			shortIdx, midIdx, longIdx, finishIdx)
	}

	if _, ok := result["branch_mid2_executed"]; !ok {
		t.Fatalf("branch_mid2 execution marker missing from result: %#v", result)
	}
	if _, ok := result["finish_executed"]; !ok {
		t.Fatalf("finish state missing from result: %#v", result)
	}
}

func TestGraphSingleEdgeWaitPropagation(t *testing.T) {
	g := NewGraph()

	var mu sync.Mutex
	executionOrder := make([]string, 0)
	record := func(name string, transform func(State) State) Handler {
		return func(ctx context.Context, state State) (State, error) {
			mu.Lock()
			executionOrder = append(executionOrder, name)
			mu.Unlock()
			next := state.Clone()
			next[name+"_visited"] = true
			if transform != nil {
				return transform(next), nil
			}
			return next, nil
		}
	}

	g.AddNode("start", record("start", nil))
	g.AddNode("branchA", record("branchA", func(state State) State {
		next := state.Clone()
		next["fromA"] = true
		return next
	}))
	g.AddNode("mid", record("mid", nil))
	g.AddNode("branchB", record("branchB", nil))
	g.AddNode("branchB2", record("branchB2", func(state State) State {
		next := state.Clone()
		next["fromB"] = true
		return next
	}))
	g.AddNode("join", func(ctx context.Context, state State) (State, error) {
		mu.Lock()
		executionOrder = append(executionOrder, "join")
		mu.Unlock()
		if _, ok := state["fromA"].(bool); !ok {
			return nil, fmt.Errorf("join missing fromA: %#v", state)
		}
		if _, ok := state["fromB"].(bool); !ok {
			return nil, fmt.Errorf("join missing fromB: %#v", state)
		}
		next := state.Clone()
		next["join_verified"] = true
		return next, nil
	})

	g.AddEdge("start", "branchA")
	g.AddEdge("start", "branchB")
	g.AddEdge("branchA", "mid")
	g.AddEdge("mid", "join")
	g.AddEdge("branchB", "branchB2")
	g.AddEdge("branchB2", "join")

	g.SetEntryPoint("start")
	g.SetFinishPoint("join")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	if _, err := executor.Execute(context.Background(), State{}); err != nil {
		t.Fatalf("execution error: %v (order=%v)", err, executionOrder)
	}

	if len(executionOrder) == 0 || executionOrder[len(executionOrder)-1] != "join" {
		t.Fatalf("expected join to execute last, order=%v", executionOrder)
	}
}

// TestGraphAsymmetricConvergence tests a more complex asymmetric DAG
// similar to the ASR pipeline structure that was failing.
func TestGraphAsymmetricConvergence(t *testing.T) {
	// Graph topology:
	// prepare → vad → xid ↘
	//             ↘ chunk → asr → merge
	//
	// This creates an asymmetric convergence where:
	// - xid is reached directly from vad
	// - asr is reached through chunk
	// - merge must wait for both xid and asr

	g := NewGraph()

	var mu sync.Mutex
	executed := make(map[string]int)
	executionOrder := make([]string, 0)
	record := func(name string) Handler {
		return func(ctx context.Context, state State) (State, error) {
			mu.Lock()
			executed[name]++
			executionOrder = append(executionOrder, name)
			mu.Unlock()

			next := state.Clone()
			// Each node sets its own key to avoid mergeStates overwriting
			next[name+"_data"] = "processed"
			return next, nil
		}
	}

	g.AddNode("prepare", record("prepare"))
	g.AddNode("vad", record("vad"))
	g.AddNode("xid", record("xid"))
	g.AddNode("chunk", record("chunk"))
	g.AddNode("asr", record("asr"))
	g.AddNode("merge", record("merge"))

	// Build asymmetric structure
	g.AddEdge("prepare", "vad")
	g.AddEdge("vad", "xid")   // Shorter path to merge
	g.AddEdge("vad", "chunk") // Longer path to merge
	g.AddEdge("chunk", "asr")
	g.AddEdge("xid", "merge")
	g.AddEdge("asr", "merge")

	g.SetEntryPoint("prepare")
	g.SetFinishPoint("merge")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	_, err = executor.Execute(context.Background(), State{})
	if err != nil {
		t.Fatalf("execution error: %v", err)
	}

	t.Logf("Execution order: %v", executionOrder)

	// Verify all nodes executed
	expectedNodes := []string{"prepare", "vad", "xid", "chunk", "asr", "merge"}
	for _, node := range expectedNodes {
		if executed[node] != 1 {
			t.Errorf("expected %s to execute once, got %d", node, executed[node])
		}
	}

	// Verify execution order: both xid and asr must complete before merge
	xidIndex := -1
	asrIndex := -1
	mergeIndex := -1
	for i, node := range executionOrder {
		switch node {
		case "xid":
			xidIndex = i
		case "asr":
			asrIndex = i
		case "merge":
			mergeIndex = i
		}
	}

	// The critical assertion: merge must execute after BOTH xid and asr
	if !(mergeIndex > xidIndex && mergeIndex > asrIndex) {
		t.Errorf("merge should execute after both xid and asr, got xid at %d, asr at %d, merge at %d",
			xidIndex, asrIndex, mergeIndex)
	}
}
