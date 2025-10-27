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

func stepHandler(name string) GraphHandler {
	return func(ctx context.Context, state State) (State, error) {
		return appendStep(state, name), nil
	}
}

func incrementHandler(delta int) GraphHandler {
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
	handlerFor := func(name string) GraphHandler {
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

	handler, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	result, err := handler(context.Background(), nil)
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

	handler, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	_, err = handler(context.Background(), nil)
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

	handler, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	result, err := handler(context.Background(), nil)
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
	parallelState, err := handlerParallel(context.Background(), State{})
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
	serialState, err := handlerSerial(context.Background(), State{})
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

	handler, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	result, err := handler(context.Background(), State{valueKey: 0})
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

	handler, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	result, err := handler(context.Background(), State{valueKey: 0})
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

	handler, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = handler(ctx, State{})
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
	record := func(name string) GraphHandler {
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

	handler, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	if _, err := handler(context.Background(), State{}); err != nil {
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

	record := func(name string) GraphHandler {
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

	handler, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	_, err = handler(context.Background(), State{})
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

	handler, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	final, err := handler(context.Background(), State{})
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
	record := func(name string, mutate func(State) State) GraphHandler {
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

	handler, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	_, err = handler(context.Background(), State{})
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
	record := func(name string, mutate func(State) State) GraphHandler {
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

	handler, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	if _, err := handler(context.Background(), State{}); err != nil {
		t.Fatalf("run error: %v", err)
	}

	if executed["join"] == 0 {
		t.Fatalf("expected join to execute even when branch_b skips it: %v", executed)
	}
	if executed["sink"] == 0 {
		t.Fatalf("expected sink to execute when branch_b skips join: %v", executed)
	}
}
