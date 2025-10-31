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

func TestGraphCompileRejectsCycles(t *testing.T) {
	g := NewGraph()
	_ = g.AddNode("A", stepHandler("A"))
	_ = g.AddNode("B", stepHandler("B"))
	_ = g.AddEdge("A", "B")
	_ = g.AddEdge("B", "A")
	_ = g.SetEntryPoint("A")
	_ = g.SetFinishPoint("B")

	if _, err := g.Compile(); err == nil || !strings.Contains(err.Error(), "cycles are not supported") {
		t.Fatalf("expected cycle detection error, got %v", err)
	}
}

func TestGraphCompileRejectsCyclesInDisconnectedComponent(t *testing.T) {
	g := NewGraph()

	_ = g.AddNode("start", stepHandler("start"))
	_ = g.AddNode("end", stepHandler("end"))
	_ = g.AddEdge("start", "end")
	_ = g.SetEntryPoint("start")
	_ = g.SetFinishPoint("end")

	// Add a disconnected cyclic component: X -> Y -> Z -> X
	_ = g.AddNode("X", stepHandler("X"))
	_ = g.AddNode("Y", stepHandler("Y"))
	_ = g.AddNode("Z", stepHandler("Z"))
	_ = g.AddEdge("X", "Y")
	_ = g.AddEdge("Y", "Z")
	_ = g.AddEdge("Z", "X")

	if _, err := g.Compile(); err == nil || !strings.Contains(err.Error(), "cycles are not supported") {
		t.Fatalf("expected cycle detection error from disconnected component, got %v", err)
	}
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

func TestGraphConditionalMixedPrecedence(t *testing.T) {
	g := NewGraph()

	visited := make(map[string]int)
	record := func(name string, allow bool) Handler {
		return func(ctx context.Context, state State) (State, error) {
			visited[name]++
			next := state.Clone()
			next["path"] = append(getStringSlice(state["path"]), name)
			next["allow"] = allow
			return next, nil
		}
	}

	_ = g.AddNode("start", func(ctx context.Context, state State) (State, error) {
		next := state.Clone()
		next["path"] = []string{"start"}
		return next, nil
	})
	_ = g.AddNode("decision", func(ctx context.Context, state State) (State, error) {
		return state.Clone(), nil
	})
	_ = g.AddNode("first", record("first", false))
	_ = g.AddNode("second", record("second", true))
	_ = g.AddNode("fallback", record("fallback", false))
	_ = g.AddNode("finish", func(ctx context.Context, state State) (State, error) {
		visited["finish"]++
		return state.Clone(), nil
	})

	_ = g.AddEdge("start", "decision")
	_ = g.AddEdge("decision", "first", WithEdgeCondition(func(_ context.Context, state State) bool {
		return false
	}))
	_ = g.AddEdge("decision", "second", WithEdgeCondition(func(_ context.Context, state State) bool {
		return true
	}))
	_ = g.AddEdge("decision", "fallback")
	_ = g.AddEdge("first", "finish")
	_ = g.AddEdge("second", "finish")
	_ = g.AddEdge("fallback", "finish")

	_ = g.SetEntryPoint("start")
	_ = g.SetFinishPoint("finish")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	if _, err := executor.Execute(context.Background(), State{}); err != nil {
		t.Fatalf("execution error: %v", err)
	}

	if visited["second"] != 1 {
		t.Fatalf("expected second branch to execute once, got %d (visited=%v)", visited["second"], visited)
	}
	if visited["first"] != 0 || visited["fallback"] != 0 {
		t.Fatalf("unexpected branches executed, counts=%v", visited)
	}
	if visited["finish"] != 1 {
		t.Fatalf("expected finish to execute once, got %d", visited["finish"])
	}
}

func TestGraphConditionalUnconditionalOrder(t *testing.T) {
	g := NewGraph(WithParallel(false))

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
		next["allow_conditional"] = true
		return next
	}))
	g.AddNode("always", record("always", nil))
	g.AddNode("conditional", record("conditional", nil))
	g.AddNode("join", record("join", nil))

	g.AddEdge("start", "always")
	g.AddEdge("start", "conditional", WithEdgeCondition(func(_ context.Context, state State) bool {
		allow, _ := state["allow_conditional"].(bool)
		return allow
	}))
	g.AddEdge("always", "join")
	g.AddEdge("conditional", "join")

	g.SetEntryPoint("start")
	g.SetFinishPoint("join")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	if _, err := executor.Execute(context.Background(), State{}); err != nil {
		t.Fatalf("execution error: %v", err)
	}

	if executed["conditional"] == 0 {
		t.Fatalf("expected conditional branch to execute when condition true, executed=%v", executed)
	}
}

func TestMiddlewareReceivesNodeName(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	mw := func(next Handler) Handler {
		return func(ctx context.Context, state State) (State, error) {
			if node, ok := FromNodeContext(ctx); ok {
				mu.Lock()
				seen = append(seen, node.Name)
				mu.Unlock()
			} else {
				t.Fatalf("node name missing from context")
			}
			return next(ctx, state)
		}
	}

	g := NewGraph(WithMiddleware(mw))
	g.AddNode("start", func(ctx context.Context, state State) (State, error) {
		next := state.Clone()
		next[stepsKey] = append(getStringSlice(state[stepsKey]), "start")
		return next, nil
	})
	g.AddNode("finish", stepHandler("finish"))
	g.AddEdge("start", "finish")
	g.SetEntryPoint("start")
	g.SetFinishPoint("finish")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	if _, err := executor.Execute(context.Background(), State{}); err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if len(seen) != 2 {
		t.Fatalf("expected to see two node names, got %v", seen)
	}
	if seen[0] != "start" || seen[1] != "finish" {
		t.Fatalf("unexpected node names order: %v", seen)
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

	if serialState[valueKey].(int) != parallelState[valueKey].(int) {
		t.Fatalf("expected serial and parallel execution to yield same result, got serial=%d parallel=%d",
			serialState[valueKey].(int), parallelState[valueKey].(int))
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

func TestGraphParallelNestedFanOutConcurrency(t *testing.T) {
	g := NewGraph(WithParallel(true))

	var mu sync.Mutex
	started := make(map[string]bool, 2)
	ready := make(chan struct{})
	release := make(chan struct{})
	var readyOnce sync.Once
	var releaseOnce sync.Once

	blockingBranch := func(name string) Handler {
		return func(ctx context.Context, state State) (State, error) {
			next := state.Clone()
			next[name] = true

			mu.Lock()
			started[name] = true
			if len(started) == 2 {
				readyOnce.Do(func() {
					close(ready)
				})
			}
			mu.Unlock()

			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}

			return next, nil
		}
	}

	g.AddNode("start", func(ctx context.Context, state State) (State, error) {
		next := state.Clone()
		next["start"] = true
		return next, nil
	})
	g.AddNode("branch_b", func(ctx context.Context, state State) (State, error) {
		next := state.Clone()
		next["branch_b"] = true
		return next, nil
	})
	g.AddNode("branch_c", blockingBranch("branch_c"))
	g.AddNode("branch_d", blockingBranch("branch_d"))
	g.AddNode("join", func(ctx context.Context, state State) (State, error) {
		if _, ok := state["branch_c"].(bool); !ok {
			return nil, fmt.Errorf("join missing branch_c contribution: %#v", state)
		}
		if _, ok := state["branch_d"].(bool); !ok {
			return nil, fmt.Errorf("join missing branch_d contribution: %#v", state)
		}
		next := state.Clone()
		next["joined"] = true
		return next, nil
	})

	g.AddEdge("start", "branch_b")
	g.AddEdge("branch_b", "branch_c")
	g.AddEdge("branch_b", "branch_d")
	g.AddEdge("branch_c", "join")
	g.AddEdge("branch_d", "join")

	g.SetEntryPoint("start")
	g.SetFinishPoint("join")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	type result struct {
		state State
		err   error
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resCh := make(chan result, 1)
	go func() {
		state, err := executor.Execute(ctx, State{})
		resCh <- result{state: state, err: err}
	}()

	select {
	case <-ready:
	case <-time.After(200 * time.Millisecond):
		releaseOnce.Do(func() { close(release) })
		t.Fatalf("expected branch_c and branch_d to start concurrently")
	}

	releaseOnce.Do(func() { close(release) })

	select {
	case res := <-resCh:
		if res.err != nil {
			t.Fatalf("execution error: %v", res.err)
		}
		if _, ok := res.state["joined"].(bool); !ok {
			t.Fatalf("expected join to run, result=%#v", res.state)
		}
		if _, ok := res.state["branch_c"].(bool); !ok {
			t.Fatalf("expected branch_c contribution, result=%#v", res.state)
		}
		if _, ok := res.state["branch_d"].(bool); !ok {
			t.Fatalf("expected branch_d contribution, result=%#v", res.state)
		}
	case <-time.After(time.Second):
		t.Fatalf("execution did not complete after releasing branch workers")
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

func TestGraphJoinRequiresAllInputs(t *testing.T) {
	g := NewGraph(WithParallel(false))

	var mu sync.Mutex
	executed := make(map[string]int)
	record := func(name string, mutate func(State) State) Handler {
		return func(ctx context.Context, state State) (State, error) {
			mu.Lock()
			executed[name]++
			mu.Unlock()
			if mutate != nil {
				return mutate(state.Clone()), nil
			}
			return state.Clone(), nil
		}
	}

	g.AddNode("start", record("start", func(state State) State {
		next := state.Clone()
		next["seed"] = 1
		return next
	}))
	g.AddNode("branch_a", record("branch_a", func(state State) State {
		next := state.Clone()
		next["a"] = true
		return next
	}))
	g.AddNode("branch_b", record("branch_b", func(state State) State {
		next := state.Clone()
		next["b"] = true
		return next
	}))
	g.AddNode("control", record("control", func(state State) State {
		next := state.Clone()
		next["control"] = true
		return next
	}))
	var joinExecuted bool
	g.AddNode("join", func(ctx context.Context, state State) (State, error) {
		joinExecuted = true
		if _, ok := state["a"].(bool); !ok {
			return nil, fmt.Errorf("missing branch_a contribution: %#v", state)
		}
		if _, ok := state["b"].(bool); !ok {
			return nil, fmt.Errorf("missing branch_b contribution: %#v", state)
		}
		if _, ok := state["control"].(bool); !ok {
			return nil, fmt.Errorf("missing control contribution: %#v", state)
		}
		next := state.Clone()
		next["joined"] = true
		return next, nil
	})

	g.AddEdge("start", "branch_a")
	g.AddEdge("start", "branch_b")
	g.AddEdge("start", "control")
	g.AddEdge("branch_a", "join")
	g.AddEdge("branch_b", "join")
	g.AddEdge("control", "join")

	g.SetEntryPoint("start")
	g.SetFinishPoint("join")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	result, err := executor.Execute(context.Background(), State{})
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if !joinExecuted {
		t.Fatalf("expected join to execute, got executed=%v", executed)
	}
	if _, ok := result["joined"].(bool); !ok {
		t.Fatalf("expected join result flag, got %#v", result)
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

func TestGraphSerialFanOutOrder(t *testing.T) {
	g := NewGraph(WithParallel(false))

	var mu sync.Mutex
	executed := make([]string, 0, 6)

	record := func(name string) Handler {
		return func(ctx context.Context, state State) (State, error) {
			mu.Lock()
			executed = append(executed, name)
			mu.Unlock()
			return state.Clone(), nil
		}
	}

	g.AddNode("start", record("start"))
	g.AddNode("branch_a", record("branch_a"))
	g.AddNode("branch_b", record("branch_b"))
	g.AddNode("branch_c", record("branch_c"))
	g.AddNode("branch_d", record("branch_d"))
	g.AddNode("join", record("join"))

	g.AddEdge("start", "branch_a")
	g.AddEdge("start", "branch_b")
	g.AddEdge("branch_b", "branch_c")
	g.AddEdge("branch_b", "branch_d")
	g.AddEdge("branch_c", "join")
	g.AddEdge("branch_d", "join")
	g.AddEdge("branch_a", "join")

	g.SetEntryPoint("start")
	g.SetFinishPoint("join")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	if _, err := executor.Execute(context.Background(), State{"a": 1}); err != nil {
		t.Fatalf("execution error: %v", err)
	}

	expected := []string{"start", "branch_a", "branch_b", "branch_c", "branch_d", "join"}
	if len(executed) != len(expected) {
		t.Fatalf("unexpected execution count: got %v want %v", executed, expected)
	}
	for i, name := range expected {
		if executed[i] != name {
			t.Fatalf("execution order mismatch at %d: got %s want %s (full=%v)", i, executed[i], name, executed)
		}
	}
}

func TestGraphLoopFlow(t *testing.T) {
	g := NewGraph()

	g.AddNode("outline", stepHandler("outline"))
	g.AddNode("review", stepHandler("review"))
	g.AddNode("revise", stepHandler("revise"))
	g.AddNode("publish", stepHandler("publish"))

	g.AddEdge("outline", "review")
	g.AddEdge("review", "revise")
	g.AddEdge("revise", "review") // introduces a cycle
	g.AddEdge("revise", "publish")

	g.SetEntryPoint("outline")
	g.SetFinishPoint("publish")

	if _, err := g.Compile(); err == nil || !strings.Contains(err.Error(), "cycles are not supported") {
		t.Fatalf("expected compile error for cyclic graph, got %v", err)
	}
}
func TestGraphSerialFanOutStateIsolation(t *testing.T) {
	g := NewGraph(WithParallel(false))

	var (
		mu               sync.Mutex
		branchBSawFromA  bool
		joinVerifiedBoth bool
	)

	g.AddNode("start", func(ctx context.Context, state State) (State, error) {
		next := State{}
		next["path"] = []string{"start"}
		return next, nil
	})

	g.AddNode("branchA", func(ctx context.Context, state State) (State, error) {
		path := getStringSlice(state["path"])
		if len(path) != 1 || path[0] != "start" {
			t.Fatalf("branchA received unexpected path: %v", path)
		}
		next := state.Clone()
		next["path"] = append(path, "branchA")
		next["fromA"] = true
		return next, nil
	})

	g.AddNode("branchB", func(ctx context.Context, state State) (State, error) {
		path := getStringSlice(state["path"])
		if len(path) != 1 || path[0] != "start" {
			t.Fatalf("branchB received unexpected path: %v", path)
		}
		if _, ok := state["fromA"]; ok {
			mu.Lock()
			branchBSawFromA = true
			mu.Unlock()
		}
		next := state.Clone()
		next["path"] = append(path, "branchB")
		next["fromB"] = true
		return next, nil
	})

	g.AddNode("join", func(ctx context.Context, state State) (State, error) {
		mu.Lock()
		defer mu.Unlock()
		if _, ok := state["fromA"].(bool); !ok {
			return nil, fmt.Errorf("join missing fromA flag: %#v", state)
		}
		if _, ok := state["fromB"].(bool); !ok {
			return nil, fmt.Errorf("join missing fromB flag: %#v", state)
		}
		joinVerifiedBoth = true
		return state.Clone(), nil
	})

	g.SetEntryPoint("start")
	g.SetFinishPoint("join")

	g.AddEdge("start", "branchA")
	g.AddEdge("start", "branchB")
	g.AddEdge("branchA", "join")
	g.AddEdge("branchB", "join")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	if _, err := executor.Execute(context.Background(), State{}); err != nil {
		t.Fatalf("execution error: %v", err)
	}

	if branchBSawFromA {
		t.Fatalf("expected branchB to see an isolated state, but fromA was present")
	}
	if !joinVerifiedBoth {
		t.Fatalf("join did not observe contributions from both branches")
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

func TestExecutorConcurrentRuns(t *testing.T) {
	g := NewGraph()

	g.AddNode("start", func(ctx context.Context, state State) (State, error) {
		next := state.Clone()
		if _, ok := next["value"].(int); !ok {
			next["value"] = 0
		}
		return next, nil
	})
	g.AddNode("worker", func(ctx context.Context, state State) (State, error) {
		next := state.Clone()
		val, _ := next["value"].(int)
		next["value"] = val + 1
		return next, nil
	})
	g.AddNode("finish", func(ctx context.Context, state State) (State, error) {
		return state.Clone(), nil
	})

	g.AddEdge("start", "worker")
	g.AddEdge("worker", "finish")
	g.SetEntryPoint("start")
	g.SetFinishPoint("finish")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	const runs = 8
	type result struct {
		input  int
		output int
		err    error
	}

	results := make([]result, runs)
	var wg sync.WaitGroup
	wg.Add(runs)

	for i := 0; i < runs; i++ {
		go func() {
			defer wg.Done()
			initial := State{"value": i}
			out, err := executor.Execute(context.Background(), initial)
			results[i] = result{input: i, err: err}
			if err == nil {
				val, _ := out["value"].(int)
				results[i].output = val
			}
			if initial["value"] != i {
				t.Errorf("initial state mutated for run %d: %#v", i, initial)
			}
		}()
	}

	wg.Wait()

	for _, r := range results {
		if r.err != nil {
			t.Fatalf("execution error for run %d: %v", r.input, r.err)
		}
		if r.output != r.input+1 {
			t.Fatalf("unexpected output for run %d: got %d want %d", r.input, r.output, r.input+1)
		}
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

// TestGraphComplexMixedOrchestration tests a complex scenario mixing:
// - Parallel branches
// - Conditional routing
// - Serial chains
// - Multiple convergence points
func TestGraphComplexMixedOrchestration(t *testing.T) {
	// Graph topology:
	//                   → process_a → transform_a ↘
	// start → split ───→ process_b ────────────────→ join1 → validate_check → (cond) → skip/validate → final
	//                   → process_c ─────────────────↗
	//
	// Scenarios tested:
	// - Parallel split (split → process_a/b/c)
	// - Serial chain (process_a → transform_a)
	// - Multiple convergence (join1 waits for transform_a, process_b, process_c)
	// - Conditional branch after convergence

	g := NewGraph()

	var mu sync.Mutex
	executed := make(map[string]int)
	executionOrder := make([]string, 0)
	record := func(name string, stateMutator func(State) State) Handler {
		return func(ctx context.Context, state State) (State, error) {
			mu.Lock()
			executed[name]++
			executionOrder = append(executionOrder, name)
			mu.Unlock()

			next := state.Clone()
			next[name+"_visited"] = true
			if stateMutator != nil {
				return stateMutator(next), nil
			}
			return next, nil
		}
	}

	// Setup nodes
	g.AddNode("start", record("start", func(s State) State {
		s["enable_validate"] = true
		return s
	}))
	g.AddNode("split", record("split", nil))
	g.AddNode("process_a", record("process_a", nil))
	g.AddNode("transform_a", record("transform_a", func(s State) State {
		s["data_a"] = "transformed"
		return s
	}))
	g.AddNode("process_b", record("process_b", func(s State) State {
		s["data_b"] = "processed"
		return s
	}))
	g.AddNode("process_c", record("process_c", nil))
	g.AddNode("join1", record("join1", nil))
	g.AddNode("validate_check", record("validate_check", nil))
	g.AddNode("skip", record("skip", nil))
	g.AddNode("validate", record("validate", func(s State) State {
		s["validated"] = true
		return s
	}))
	g.AddNode("final", record("final", nil))

	// Build edges
	g.AddEdge("start", "split")
	g.AddEdge("split", "process_a")
	g.AddEdge("split", "process_b")
	g.AddEdge("split", "process_c")
	g.AddEdge("process_a", "transform_a")
	g.AddEdge("transform_a", "join1")
	g.AddEdge("process_b", "join1")
	g.AddEdge("process_c", "join1")
	g.AddEdge("join1", "validate_check")

	// Conditional routing after join
	g.AddEdge("validate_check", "skip", WithEdgeCondition(func(_ context.Context, state State) bool {
		enable, _ := state["enable_validate"].(bool)
		return !enable
	}))
	g.AddEdge("validate_check", "validate", WithEdgeCondition(func(_ context.Context, state State) bool {
		enable, _ := state["enable_validate"].(bool)
		return enable
	}))
	g.AddEdge("skip", "final")
	g.AddEdge("validate", "final")

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

	t.Logf("Execution order: %v", executionOrder)

	// Verify all expected nodes executed
	expectedNodes := []string{"start", "split", "process_a", "transform_a", "process_b", "process_c", "join1", "validate_check", "validate", "final"}
	for _, node := range expectedNodes {
		if executed[node] != 1 {
			t.Errorf("expected %s to execute once, got %d", node, executed[node])
		}
	}

	// Verify skip was not executed (because enable_validate=true)
	if executed["skip"] != 0 {
		t.Errorf("expected skip not to execute, got %d", executed["skip"])
	}

	// Verify state merging
	if _, ok := result["validated"].(bool); !ok {
		t.Errorf("expected validated flag in result")
	}
	if _, ok := result["data_a"].(string); !ok {
		t.Errorf("expected data_a in result")
	}
	if _, ok := result["data_b"].(string); !ok {
		t.Errorf("expected data_b in result")
	}

	// Verify join1 executed after all its predecessors
	findIndex := func(name string) int {
		for i, n := range executionOrder {
			if n == name {
				return i
			}
		}
		return -1
	}

	join1Idx := findIndex("join1")
	if join1Idx == -1 {
		t.Fatal("join1 not executed")
	}

	for _, pred := range []string{"transform_a", "process_b", "process_c"} {
		predIdx := findIndex(pred)
		if predIdx == -1 || predIdx >= join1Idx {
			t.Errorf("expected %s to execute before join1, got %s=%d join1=%d", pred, pred, predIdx, join1Idx)
		}
	}
}

// TestGraphNestedParallelBranches tests parallel branches that contain parallel sub-branches
func TestGraphNestedParallelBranches(t *testing.T) {
	// Graph topology:
	//                   → worker1a ↘
	// start → fanout1 ──→ worker1b ─→ join1 ↘
	//                   → worker1c ↗          ↘
	//                                           → final_merge
	//                   → worker2a ↘          ↗
	//        → fanout2 ──→ worker2b ─→ join2 ↗
	//                   → worker2c ↗

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

			// Simulate some work
			time.Sleep(time.Millisecond)

			next := state.Clone()
			next[name+"_result"] = name + "_done"
			return next, nil
		}
	}

	// Build nested parallel structure
	g.AddNode("start", record("start"))
	g.AddNode("fanout1", record("fanout1"))
	g.AddNode("worker1a", record("worker1a"))
	g.AddNode("worker1b", record("worker1b"))
	g.AddNode("worker1c", record("worker1c"))
	g.AddNode("join1", record("join1"))
	g.AddNode("fanout2", record("fanout2"))
	g.AddNode("worker2a", record("worker2a"))
	g.AddNode("worker2b", record("worker2b"))
	g.AddNode("worker2c", record("worker2c"))
	g.AddNode("join2", record("join2"))
	g.AddNode("final_merge", record("final_merge"))

	g.AddEdge("start", "fanout1")
	g.AddEdge("start", "fanout2")
	g.AddEdge("fanout1", "worker1a")
	g.AddEdge("fanout1", "worker1b")
	g.AddEdge("fanout1", "worker1c")
	g.AddEdge("worker1a", "join1")
	g.AddEdge("worker1b", "join1")
	g.AddEdge("worker1c", "join1")
	g.AddEdge("fanout2", "worker2a")
	g.AddEdge("fanout2", "worker2b")
	g.AddEdge("fanout2", "worker2c")
	g.AddEdge("worker2a", "join2")
	g.AddEdge("worker2b", "join2")
	g.AddEdge("worker2c", "join2")
	g.AddEdge("join1", "final_merge")
	g.AddEdge("join2", "final_merge")

	g.SetEntryPoint("start")
	g.SetFinishPoint("final_merge")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	result, err := executor.Execute(context.Background(), State{})
	if err != nil {
		t.Fatalf("execution error: %v", err)
	}

	t.Logf("Execution order: %v", executionOrder)

	// Verify all nodes executed exactly once
	allNodes := []string{
		"start", "fanout1", "worker1a", "worker1b", "worker1c", "join1",
		"fanout2", "worker2a", "worker2b", "worker2c", "join2", "final_merge",
	}
	for _, node := range allNodes {
		if executed[node] != 1 {
			t.Errorf("expected %s to execute once, got %d", node, executed[node])
		}
	}

	// Verify all worker results are present
	for _, worker := range []string{"worker1a", "worker1b", "worker1c", "worker2a", "worker2b", "worker2c"} {
		key := worker + "_result"
		if _, ok := result[key]; !ok {
			t.Errorf("expected %s in result", key)
		}
	}

	// Verify convergence order
	findIndex := func(name string) int {
		for i, n := range executionOrder {
			if n == name {
				return i
			}
		}
		return -1
	}

	join1Idx := findIndex("join1")
	join2Idx := findIndex("join2")
	finalIdx := findIndex("final_merge")

	// join1 must wait for all worker1x
	for _, worker := range []string{"worker1a", "worker1b", "worker1c"} {
		workerIdx := findIndex(worker)
		if workerIdx >= join1Idx {
			t.Errorf("%s should execute before join1", worker)
		}
	}

	// join2 must wait for all worker2x
	for _, worker := range []string{"worker2a", "worker2b", "worker2c"} {
		workerIdx := findIndex(worker)
		if workerIdx >= join2Idx {
			t.Errorf("%s should execute before join2", worker)
		}
	}

	// final_merge must wait for both joins
	if join1Idx >= finalIdx || join2Idx >= finalIdx {
		t.Errorf("both joins should execute before final_merge")
	}
}

// TestGraphComplexConditionalRouting tests multiple conditional branches with cross-convergence
func TestGraphComplexConditionalRouting(t *testing.T) {
	// Graph topology:
	//              → process_high → priority_handler ↘
	// start → classify ─→ process_med → standard_handler ─→ aggregate → final
	//              → process_low → (skip or retry) ────────↗
	//
	// Routes based on priority level in state

	g := NewGraph()

	var mu sync.Mutex
	executed := make(map[string]int)

	record := func(name string, mutator func(State) State) Handler {
		return func(ctx context.Context, state State) (State, error) {
			mu.Lock()
			executed[name]++
			mu.Unlock()

			next := state.Clone()
			next[name+"_visited"] = true
			if mutator != nil {
				return mutator(next), nil
			}
			return next, nil
		}
	}

	g.AddNode("start", record("start", func(s State) State {
		s["priority"] = "medium"
		s["retry_enabled"] = true
		return s
	}))
	g.AddNode("classify", record("classify", nil))
	g.AddNode("process_high", record("process_high", nil))
	g.AddNode("process_med", record("process_med", nil))
	g.AddNode("process_low", record("process_low", nil))
	g.AddNode("priority_handler", record("priority_handler", func(s State) State {
		s["handled_priority"] = true
		return s
	}))
	g.AddNode("standard_handler", record("standard_handler", func(s State) State {
		s["handled_standard"] = true
		return s
	}))
	g.AddNode("skip", record("skip", nil))
	g.AddNode("retry", record("retry", func(s State) State {
		s["retried"] = true
		return s
	}))
	g.AddNode("aggregate", record("aggregate", nil))
	g.AddNode("final", record("final", nil))

	// Conditional edges based on priority
	g.AddEdge("start", "classify")
	g.AddEdge("classify", "process_high", WithEdgeCondition(func(_ context.Context, state State) bool {
		return state["priority"] == "high"
	}))
	g.AddEdge("classify", "process_med", WithEdgeCondition(func(_ context.Context, state State) bool {
		return state["priority"] == "medium"
	}))
	g.AddEdge("classify", "process_low", WithEdgeCondition(func(_ context.Context, state State) bool {
		return state["priority"] == "low"
	}))

	// Different paths for different priorities
	g.AddEdge("process_high", "priority_handler")
	g.AddEdge("priority_handler", "aggregate")

	g.AddEdge("process_med", "standard_handler")
	g.AddEdge("standard_handler", "aggregate")

	g.AddEdge("process_low", "skip", WithEdgeCondition(func(_ context.Context, state State) bool {
		retry, _ := state["retry_enabled"].(bool)
		return !retry
	}))
	g.AddEdge("process_low", "retry", WithEdgeCondition(func(_ context.Context, state State) bool {
		retry, _ := state["retry_enabled"].(bool)
		return retry
	}))
	g.AddEdge("skip", "aggregate")
	g.AddEdge("retry", "aggregate")

	g.AddEdge("aggregate", "final")

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

	// Verify correct path was taken (medium priority → standard handler)
	if executed["process_med"] != 1 {
		t.Errorf("expected process_med to execute once, got %d", executed["process_med"])
	}
	if executed["standard_handler"] != 1 {
		t.Errorf("expected standard_handler to execute once, got %d", executed["standard_handler"])
	}

	// Verify other paths were not taken
	if executed["process_high"] != 0 || executed["priority_handler"] != 0 {
		t.Errorf("high priority path should not execute")
	}
	if executed["process_low"] != 0 || executed["skip"] != 0 || executed["retry"] != 0 {
		t.Errorf("low priority path should not execute")
	}

	// Verify state
	if _, ok := result["handled_standard"].(bool); !ok {
		t.Errorf("expected handled_standard flag in result")
	}

	// Test with different priority
	t.Run("low priority with retry", func(t *testing.T) {
		mu.Lock()
		executed = make(map[string]int)
		mu.Unlock()

		// Create a new graph for low priority test
		g2 := NewGraph()
		g2.AddNode("start", record("start", func(s State) State {
			s["priority"] = "low"
			s["retry_enabled"] = true
			return s
		}))
		g2.AddNode("classify", record("classify", nil))
		g2.AddNode("process_high", record("process_high", nil))
		g2.AddNode("process_med", record("process_med", nil))
		g2.AddNode("process_low", record("process_low", nil))
		g2.AddNode("priority_handler", record("priority_handler", func(s State) State {
			s["handled_priority"] = true
			return s
		}))
		g2.AddNode("standard_handler", record("standard_handler", func(s State) State {
			s["handled_standard"] = true
			return s
		}))
		g2.AddNode("skip", record("skip", nil))
		g2.AddNode("retry", record("retry", func(s State) State {
			s["retried"] = true
			return s
		}))
		g2.AddNode("aggregate", record("aggregate", nil))
		g2.AddNode("final", record("final", nil))

		g2.AddEdge("start", "classify")
		g2.AddEdge("classify", "process_high", WithEdgeCondition(func(_ context.Context, state State) bool {
			return state["priority"] == "high"
		}))
		g2.AddEdge("classify", "process_med", WithEdgeCondition(func(_ context.Context, state State) bool {
			return state["priority"] == "medium"
		}))
		g2.AddEdge("classify", "process_low", WithEdgeCondition(func(_ context.Context, state State) bool {
			return state["priority"] == "low"
		}))

		g2.AddEdge("process_high", "priority_handler")
		g2.AddEdge("priority_handler", "aggregate")
		g2.AddEdge("process_med", "standard_handler")
		g2.AddEdge("standard_handler", "aggregate")

		g2.AddEdge("process_low", "skip", WithEdgeCondition(func(_ context.Context, state State) bool {
			retry, _ := state["retry_enabled"].(bool)
			return !retry
		}))
		g2.AddEdge("process_low", "retry", WithEdgeCondition(func(_ context.Context, state State) bool {
			retry, _ := state["retry_enabled"].(bool)
			return retry
		}))
		g2.AddEdge("skip", "aggregate")
		g2.AddEdge("retry", "aggregate")
		g2.AddEdge("aggregate", "final")

		g2.SetEntryPoint("start")
		g2.SetFinishPoint("final")

		executor2, err := g2.Compile()
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}

		result2, err := executor2.Execute(context.Background(), State{})
		if err != nil {
			t.Fatalf("execution error: %v", err)
		}

		if executed["process_low"] != 1 || executed["retry"] != 1 {
			t.Errorf("expected low priority retry path, got process_low=%d retry=%d",
				executed["process_low"], executed["retry"])
		}

		if _, ok := result2["retried"].(bool); !ok {
			t.Errorf("expected retried flag in result")
		}
	})
}

// TestGraphLargeScaleDAG tests a larger, more complex DAG scenario
func TestGraphLargeScaleDAG(t *testing.T) {
	// Simulates a complex data pipeline with multiple stages:
	// Stage 1: Data ingestion (3 parallel sources)
	// Stage 2: Data transformation (per source)
	// Stage 3: Feature extraction (parallel)
	// Stage 4: Model inference (conditional routing)
	// Stage 5: Post-processing and aggregation

	g := NewGraph()

	var mu sync.Mutex
	executed := make(map[string]int)

	record := func(name string) Handler {
		return func(ctx context.Context, state State) (State, error) {
			mu.Lock()
			executed[name]++
			mu.Unlock()

			next := state.Clone()
			next[name+"_done"] = true
			return next, nil
		}
	}

	// Stage 1: Ingestion
	g.AddNode("start", record("start"))
	g.AddNode("ingest_db", record("ingest_db"))
	g.AddNode("ingest_api", record("ingest_api"))
	g.AddNode("ingest_file", record("ingest_file"))

	// Stage 2: Transformation
	g.AddNode("transform_db", record("transform_db"))
	g.AddNode("transform_api", record("transform_api"))
	g.AddNode("transform_file", record("transform_file"))

	// Stage 3: Feature extraction
	g.AddNode("extract_features", record("extract_features"))
	g.AddNode("feature_normalize", record("feature_normalize"))
	g.AddNode("feature_select", record("feature_select"))

	// Stage 4: Model inference
	g.AddNode("model_router", record("model_router"))
	g.AddNode("model_fast", record("model_fast"))
	g.AddNode("model_accurate", record("model_accurate"))

	// Stage 5: Post-processing
	g.AddNode("post_process", record("post_process"))
	g.AddNode("aggregate", record("aggregate"))
	g.AddNode("final", record("final"))

	// Build the DAG
	g.AddEdge("start", "ingest_db")
	g.AddEdge("start", "ingest_api")
	g.AddEdge("start", "ingest_file")

	g.AddEdge("ingest_db", "transform_db")
	g.AddEdge("ingest_api", "transform_api")
	g.AddEdge("ingest_file", "transform_file")

	g.AddEdge("transform_db", "extract_features")
	g.AddEdge("transform_api", "extract_features")
	g.AddEdge("transform_file", "extract_features")

	g.AddEdge("extract_features", "feature_normalize")
	g.AddEdge("extract_features", "feature_select")

	g.AddEdge("feature_normalize", "model_router")
	g.AddEdge("feature_select", "model_router")

	g.AddEdge("model_router", "model_fast")
	g.AddEdge("model_router", "model_accurate")

	g.AddEdge("model_fast", "post_process")
	g.AddEdge("model_accurate", "post_process")

	g.AddEdge("post_process", "aggregate")
	g.AddEdge("aggregate", "final")

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

	// Verify all nodes executed
	expectedNodes := []string{
		"start",
		"ingest_db", "ingest_api", "ingest_file",
		"transform_db", "transform_api", "transform_file",
		"extract_features", "feature_normalize", "feature_select",
		"model_router", "model_fast", "model_accurate",
		"post_process", "aggregate", "final",
	}

	for _, node := range expectedNodes {
		if executed[node] != 1 {
			t.Errorf("expected %s to execute once, got %d", node, executed[node])
		}
	}

	// Verify all results are merged
	for _, node := range expectedNodes {
		key := node + "_done"
		if _, ok := result[key].(bool); !ok {
			t.Errorf("expected %s in result", key)
		}
	}

	t.Logf("Successfully executed DAG with %d nodes", len(expectedNodes))
}

// TestGraphSerialWithConditionalRouting tests serial execution with conditional routing
func TestGraphSerialWithConditionalRouting(t *testing.T) {
	// Tests serial mode with conditional branches that converge
	// Graph topology:
	// start → check → [priority_high (cond) OR priority_low (fallback)] → process → final
	g := NewGraph(WithParallel(false))

	var mu sync.Mutex
	executionOrder := make([]string, 0)

	record := func(name string) Handler {
		return func(ctx context.Context, state State) (State, error) {
			mu.Lock()
			executionOrder = append(executionOrder, name)
			t.Logf("Executing node: %s", name)
			mu.Unlock()

			next := state.Clone()
			next[name] = true
			return next, nil
		}
	}

	g.AddNode("start", record("start"))
	g.AddNode("check", record("check"))
	g.AddNode("priority_high", record("priority_high"))
	g.AddNode("priority_low", record("priority_low"))
	g.AddNode("process", record("process"))
	g.AddNode("final", record("final"))

	g.AddEdge("start", "check")
	// Conditional edge first, then fallback
	g.AddEdge("check", "priority_high", WithEdgeCondition(func(_ context.Context, state State) bool {
		useHigh, _ := state["use_high"].(bool)
		t.Logf("Condition check: use_high=%v", useHigh)
		return useHigh
	}))
	g.AddEdge("check", "priority_low") // unconditional fallback

	g.AddEdge("priority_high", "process")
	g.AddEdge("priority_low", "process")
	g.AddEdge("process", "final")

	g.SetEntryPoint("start")
	g.SetFinishPoint("final")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	// Test high priority path
	t.Run("high priority path", func(t *testing.T) {
		mu.Lock()
		executionOrder = make([]string, 0)
		mu.Unlock()

		result, err := executor.Execute(context.Background(), State{"use_high": true})
		if err != nil {
			t.Fatalf("execution error: %v", err)
		}

		t.Logf("Execution order: %v", executionOrder)

		if !result["priority_high"].(bool) {
			t.Errorf("expected priority_high to execute")
		}

		expected := []string{"start", "check", "priority_high", "process", "final"}
		if !reflect.DeepEqual(executionOrder, expected) {
			t.Errorf("expected execution order %v, got %v", expected, executionOrder)
		}
	})

	// Test low priority path (fallback)
	t.Run("low priority path", func(t *testing.T) {
		mu.Lock()
		executionOrder = make([]string, 0)
		mu.Unlock()

		result, err := executor.Execute(context.Background(), State{})
		if err != nil {
			t.Fatalf("execution error: %v", err)
		}

		t.Logf("Execution order: %v", executionOrder)

		if !result["priority_low"].(bool) {
			t.Errorf("expected priority_low to execute")
		}

		expected := []string{"start", "check", "priority_low", "process", "final"}
		if !reflect.DeepEqual(executionOrder, expected) {
			t.Errorf("expected execution order %v, got %v", expected, executionOrder)
		}
	})
}

// TestNodeNameInContext tests that the node name is correctly propagated via context
func TestNodeNameInContext(t *testing.T) {
	g := NewGraph()

	var mu sync.Mutex
	nodeNames := make(map[string]string) // maps expected node name to actual node name from context

	// Handler that extracts node name from context
	handlerWithNodeName := func(expectedName string) Handler {
		return func(ctx context.Context, state State) (State, error) {
			node, ok := FromNodeContext(ctx)
			if !ok {
				return nil, fmt.Errorf("node name not found in context for node %s", expectedName)
			}

			mu.Lock()
			nodeNames[expectedName] = node.Name
			mu.Unlock()

			return state.Clone(), nil
		}
	}

	g.AddNode("start", handlerWithNodeName("start"))
	g.AddNode("process", handlerWithNodeName("process"))
	g.AddNode("finish", handlerWithNodeName("finish"))

	g.AddEdge("start", "process")
	g.AddEdge("process", "finish")
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

	// Verify each node received its correct name
	expectedNodes := []string{"start", "process", "finish"}
	for _, nodeName := range expectedNodes {
		actualName, ok := nodeNames[nodeName]
		if !ok {
			t.Errorf("node %s did not execute or did not extract node name", nodeName)
			continue
		}
		if actualName != nodeName {
			t.Errorf("expected node name %s, got %s", nodeName, actualName)
		}
	}
}

// TestNodeNameInMiddleware tests that middleware can access the node name via context
func TestNodeNameInMiddleware(t *testing.T) {
	var mu sync.Mutex
	middlewareNodeNames := make([]string, 0)
	handlerNodeNames := make([]string, 0)

	// Middleware that logs node names
	loggingMiddleware := func(next Handler) Handler {
		return func(ctx context.Context, state State) (State, error) {
			node, ok := FromNodeContext(ctx)
			if ok {
				mu.Lock()
				middlewareNodeNames = append(middlewareNodeNames, node.Name)
				mu.Unlock()
			}
			return next(ctx, state)
		}
	}

	// Handler that also checks node name
	recordHandler := func(name string) Handler {
		return func(ctx context.Context, state State) (State, error) {
			node, ok := FromNodeContext(ctx)
			if ok && node.Name == name {
				mu.Lock()
				handlerNodeNames = append(handlerNodeNames, node.Name)
				mu.Unlock()
			}
			return state.Clone(), nil
		}
	}

	g := NewGraph(WithMiddleware(loggingMiddleware))

	g.AddNode("node_a", recordHandler("node_a"))
	g.AddNode("node_b", recordHandler("node_b"))
	g.AddNode("node_c", recordHandler("node_c"))

	g.AddEdge("node_a", "node_b")
	g.AddEdge("node_b", "node_c")
	g.SetEntryPoint("node_a")
	g.SetFinishPoint("node_c")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	_, err = executor.Execute(context.Background(), State{})
	if err != nil {
		t.Fatalf("execution error: %v", err)
	}

	// Verify middleware saw all node names
	expectedNodes := []string{"node_a", "node_b", "node_c"}
	if len(middlewareNodeNames) != len(expectedNodes) {
		t.Errorf("middleware expected to see %d nodes, got %d", len(expectedNodes), len(middlewareNodeNames))
	}

	for i, expected := range expectedNodes {
		if i >= len(middlewareNodeNames) {
			t.Errorf("middleware missing node name at index %d", i)
			continue
		}
		if middlewareNodeNames[i] != expected {
			t.Errorf("middleware at index %d: expected %s, got %s", i, expected, middlewareNodeNames[i])
		}
	}

	// Verify handlers also saw correct node names
	if len(handlerNodeNames) != len(expectedNodes) {
		t.Errorf("handlers expected to see %d nodes, got %d", len(expectedNodes), len(handlerNodeNames))
	}

	for i, expected := range expectedNodes {
		if i >= len(handlerNodeNames) {
			t.Errorf("handler missing node name at index %d", i)
			continue
		}
		if handlerNodeNames[i] != expected {
			t.Errorf("handler at index %d: expected %s, got %s", i, expected, handlerNodeNames[i])
		}
	}
}

// TestNodeNameInParallelExecution tests node name propagation in parallel execution mode
func TestNodeNameInParallelExecution(t *testing.T) {
	g := NewGraph(WithParallel(true))

	var mu sync.Mutex
	nodeNames := make(map[string]bool)

	handlerWithNodeName := func(expectedName string) Handler {
		return func(ctx context.Context, state State) (State, error) {
			node, ok := FromNodeContext(ctx)
			if !ok {
				return nil, fmt.Errorf("node name not found in context")
			}
			if node.Name != expectedName {
				return nil, fmt.Errorf("expected node name %s, got %s", expectedName, node.Name)
			}

			mu.Lock()
			nodeNames[node.Name] = true
			mu.Unlock()

			// Simulate some work
			time.Sleep(time.Millisecond)

			return state.Clone(), nil
		}
	}

	g.AddNode("start", handlerWithNodeName("start"))
	g.AddNode("branch_a", handlerWithNodeName("branch_a"))
	g.AddNode("branch_b", handlerWithNodeName("branch_b"))
	g.AddNode("branch_c", handlerWithNodeName("branch_c"))
	g.AddNode("join", handlerWithNodeName("join"))

	g.AddEdge("start", "branch_a")
	g.AddEdge("start", "branch_b")
	g.AddEdge("start", "branch_c")
	g.AddEdge("branch_a", "join")
	g.AddEdge("branch_b", "join")
	g.AddEdge("branch_c", "join")

	g.SetEntryPoint("start")
	g.SetFinishPoint("join")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	_, err = executor.Execute(context.Background(), State{})
	if err != nil {
		t.Fatalf("execution error: %v", err)
	}

	// Verify all nodes executed with correct names
	expectedNodes := []string{"start", "branch_a", "branch_b", "branch_c", "join"}
	for _, nodeName := range expectedNodes {
		if !nodeNames[nodeName] {
			t.Errorf("node %s did not execute or had incorrect name", nodeName)
		}
	}

	if len(nodeNames) != len(expectedNodes) {
		t.Errorf("expected %d nodes to execute, got %d", len(expectedNodes), len(nodeNames))
	}
}

// TestNodeNameWithMultipleMiddlewares tests node name with multiple middleware layers
func TestNodeNameWithMultipleMiddlewares(t *testing.T) {
	var mu sync.Mutex
	middleware1Calls := make(map[string]int)
	middleware2Calls := make(map[string]int)

	middleware1 := func(next Handler) Handler {
		return func(ctx context.Context, state State) (State, error) {
			node, ok := FromNodeContext(ctx)
			if ok {
				mu.Lock()
				middleware1Calls[node.Name]++
				mu.Unlock()
			}
			return next(ctx, state)
		}
	}

	middleware2 := func(next Handler) Handler {
		return func(ctx context.Context, state State) (State, error) {
			node, ok := FromNodeContext(ctx)
			if ok {
				mu.Lock()
				middleware2Calls[node.Name]++
				mu.Unlock()
			}
			return next(ctx, state)
		}
	}

	g := NewGraph(WithMiddleware(middleware1, middleware2))

	g.AddNode("alpha", stepHandler("alpha"))
	g.AddNode("beta", stepHandler("beta"))
	g.AddNode("gamma", stepHandler("gamma"))

	g.AddEdge("alpha", "beta")
	g.AddEdge("beta", "gamma")
	g.SetEntryPoint("alpha")
	g.SetFinishPoint("gamma")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	_, err = executor.Execute(context.Background(), State{})
	if err != nil {
		t.Fatalf("execution error: %v", err)
	}

	// Verify both middlewares saw all nodes
	expectedNodes := []string{"alpha", "beta", "gamma"}
	for _, nodeName := range expectedNodes {
		if middleware1Calls[nodeName] != 1 {
			t.Errorf("middleware1 expected to see node %s once, got %d", nodeName, middleware1Calls[nodeName])
		}
		if middleware2Calls[nodeName] != 1 {
			t.Errorf("middleware2 expected to see node %s once, got %d", nodeName, middleware2Calls[nodeName])
		}
	}
}
