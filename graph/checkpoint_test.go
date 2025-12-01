package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

const (
	stepsKey = "steps"
	valueKey = "value"
)

// memoryCheckpointer is a test helper that stores checkpoints in memory keyed by checkpointID.
type memoryCheckpointer struct {
	mu      sync.Mutex
	last    map[string]*Checkpoint
	history map[string][]*Checkpoint
}

func newMemoryCheckpointer() *memoryCheckpointer {
	return &memoryCheckpointer{
		last:    make(map[string]*Checkpoint),
		history: make(map[string][]*Checkpoint),
	}
}

func (m *memoryCheckpointer) Save(ctx context.Context, cp *Checkpoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cloned := cp.Clone()
	m.last[cp.ID] = cloned
	m.history[cp.ID] = append(m.history[cp.ID], cloned)
	return nil
}

func (m *memoryCheckpointer) Resume(ctx context.Context, checkpointID string) (*Checkpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp, ok := m.last[checkpointID]
	if !ok {
		return nil, fmt.Errorf("checkpoint not found: %s", checkpointID)
	}
	return cp.Clone(), nil
}

func (m *memoryCheckpointer) seed(checkpointID string, cp *Checkpoint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.last[checkpointID] = cp.Clone()
}

func (m *memoryCheckpointer) snapshots(checkpointID string) []*Checkpoint {
	m.mu.Lock()
	defer m.mu.Unlock()
	checkpoints := m.history[checkpointID]
	out := make([]*Checkpoint, len(checkpoints))
	for i, cp := range checkpoints {
		out[i] = cp.Clone()
	}
	return out
}

func TestSequentialExecutionSharedState(t *testing.T) {
	g := New(WithParallel(false))
	g.AddNode("start", func(ctx context.Context, state State) (State, error) {
		state[stepsKey] = []string{"start"}
		return state, nil
	})
	g.AddNode("finish", func(ctx context.Context, state State) (State, error) {
		raw, _ := state[stepsKey]
		steps := getStringSlice(raw)
		steps = append(steps, "finish")
		state[stepsKey] = steps
		return state, nil
	})
	g.AddEdge("start", "finish")
	g.SetEntryPoint("start")
	g.SetFinishPoint("finish")

	exec, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	state, err := exec.Execute(context.Background(), State{})
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	steps := getStringSliceFromState(state, stepsKey)
	if len(steps) != 2 || steps[0] != "start" || steps[1] != "finish" {
		t.Fatalf("unexpected steps: %v", steps)
	}
}

func TestCheckpointResumeSharedState(t *testing.T) {
	g := New(WithParallel(false))
	var counters struct {
		start  int32
		mid    int32
		finish int32
	}
	g.AddNode("start", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&counters.start, 1)
		state[valueKey] = 1
		return state, nil
	})
	g.AddNode("mid", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&counters.mid, 1)
		raw, _ := state[valueKey]
		v, _ := raw.(int)
		state[valueKey] = v + 1
		return state, nil
	})
	g.AddNode("finish", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&counters.finish, 1)
		raw, _ := state[valueKey]
		v, _ := raw.(int)
		state[valueKey] = v + 1
		return state, nil
	})
	g.AddEdge("start", "mid")
	g.AddEdge("mid", "finish")
	g.SetEntryPoint("start")
	g.SetFinishPoint("finish")

	store := newMemoryCheckpointer()
	exec1, err := g.Compile(WithCheckpointer(store))
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	state, err := exec1.Execute(context.Background(), State{}, WithCheckpointID("task"))
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	checkpoints := store.snapshots("task")
	if len(checkpoints) == 0 {
		t.Fatal("expected checkpoint to capture state")
	}
	captured := checkpoints[0]
	store.seed("task", captured)

	if atomic.LoadInt32(&counters.start) != 1 || atomic.LoadInt32(&counters.mid) != 1 || atomic.LoadInt32(&counters.finish) != 1 {
		t.Fatalf("unexpected counters after first run: %#v", counters)
	}

	exec2, err := g.Compile(WithCheckpointer(store))
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	state, err = exec2.Resume(context.Background(), State{}, WithCheckpointID("task"))
	if err != nil {
		t.Fatalf("resume error: %v", err)
	}

	if atomic.LoadInt32(&counters.start) != 1 {
		t.Fatalf("start should not rerun on resume, got %d", counters.start)
	}
	if atomic.LoadInt32(&counters.mid) != 2 || atomic.LoadInt32(&counters.finish) != 2 {
		t.Fatalf("mid/finish should run again on resume, counters=%#v", counters)
	}

	val := getIntFromState(state, valueKey)
	if val != 3 {
		t.Fatalf("expected value to be 3 after resume, got %d", val)
	}
}

func TestCheckpointMarshalUnmarshal(t *testing.T) {
	original := Checkpoint{
		Received: map[string]int{"a": 1, "b": 2},
		Visited:  map[string]bool{"start": true},
		State:    map[string]any{"k": "v", "n": 1},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded Checkpoint
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if !jsonEqual(t, original, decoded) {
		t.Fatalf("decoded checkpoint mismatch: %#v vs %#v", decoded, original)
	}
}

func TestCheckpointResumeWithSkippedBranch(t *testing.T) {
	g := New(WithParallel(false))
	var counts struct {
		start   int32
		branchA int32
		branchB int32
		join    int32
	}

	g.AddNode("start", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&counts.start, 1)
		state["start"] = true
		return state, nil
	})
	g.AddNode("branch_a", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&counts.branchA, 1)
		state["a"] = true
		return state, nil
	})
	g.AddNode("branch_b", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&counts.branchB, 1)
		state["b"] = true
		return state, nil
	})
	g.AddNode("join", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&counts.join, 1)
		if _, ok := state["a"]; !ok {
			return nil, fmt.Errorf("missing branch_a state")
		}
		if _, ok := state["b"]; ok {
			return nil, fmt.Errorf("unexpected branch_b state")
		}
		return state, nil
	})

	g.AddEdge("start", "branch_a", WithEdgeCondition(func(_ context.Context, _ State) bool { return true }))
	g.AddEdge("start", "branch_b", WithEdgeCondition(func(_ context.Context, _ State) bool { return false }))
	g.AddEdge("branch_a", "join")
	g.AddEdge("branch_b", "join")
	g.SetEntryPoint("start")
	g.SetFinishPoint("join")

	store := newMemoryCheckpointer()
	const checkpointID = "skipped-branch"
	exec1, err := g.Compile(WithCheckpointer(store))
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	_, err = exec1.Execute(context.Background(), State{}, WithCheckpointID(checkpointID))
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	checkpoints := store.snapshots(checkpointID)
	if len(checkpoints) == 0 {
		t.Fatal("expected checkpoint to be captured")
	}
	cp := checkpoints[0]
	store.seed(checkpointID, cp)

	// Resume on a fresh executor with fresh counters; start/branch_b should not rerun.
	var counts2 struct {
		start   int32
		branchA int32
		branchB int32
		join    int32
	}
	g2 := New(WithParallel(false))
	g2.AddNode("start", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&counts2.start, 1)
		return state, nil
	})
	g2.AddNode("branch_a", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&counts2.branchA, 1)
		state["a"] = true
		return state, nil
	})
	g2.AddNode("branch_b", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&counts2.branchB, 1)
		return state, nil
	})
	g2.AddNode("join", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&counts2.join, 1)
		return state, nil
	})
	g2.AddEdge("start", "branch_a", WithEdgeCondition(func(_ context.Context, _ State) bool { return true }))
	g2.AddEdge("start", "branch_b", WithEdgeCondition(func(_ context.Context, _ State) bool { return false }))
	g2.AddEdge("branch_a", "join")
	g2.AddEdge("branch_b", "join")
	g2.SetEntryPoint("start")
	g2.SetFinishPoint("join")

	exec2, err := g2.Compile(WithCheckpointer(store))
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	_, err = exec2.Resume(context.Background(), State{}, WithCheckpointID(checkpointID))
	if err != nil {
		t.Fatalf("resume error: %v", err)
	}

	if counts2.start != 0 {
		t.Fatalf("start should not rerun on resume, got %d", counts2.start)
	}
	if counts2.branchB != 0 {
		t.Fatalf("branch_b should remain skipped, got %d", counts2.branchB)
	}
	if counts2.branchA != 1 || counts2.join != 1 {
		t.Fatalf("branch_a/join should run once, got a=%d join=%d", counts2.branchA, counts2.join)
	}
}

func TestCheckpointResumeRebuildReadyQueue(t *testing.T) {
	var firstCounts struct {
		start  int32
		mid    int32
		finish int32
	}
	g := New(WithParallel(false))
	g.AddNode("start", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&firstCounts.start, 1)
		state[valueKey] = 1
		return state, nil
	})
	g.AddNode("mid", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&firstCounts.mid, 1)
		val := getIntFromState(state, valueKey)
		state[valueKey] = val + 1
		return state, nil
	})
	g.AddNode("finish", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&firstCounts.finish, 1)
		val := getIntFromState(state, valueKey)
		state[valueKey] = val + 1
		return state, nil
	})
	g.AddEdge("start", "mid")
	g.AddEdge("mid", "finish")
	g.SetEntryPoint("start")
	g.SetFinishPoint("finish")

	store := newMemoryCheckpointer()
	const checkpointID = "ready-queue"
	exec1, err := g.Compile(WithCheckpointer(store))
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	_, err = exec1.Execute(context.Background(), State{}, WithCheckpointID(checkpointID))
	if err != nil {
		t.Fatalf("first execution error: %v", err)
	}
	checkpoints := store.snapshots(checkpointID)
	if len(checkpoints) == 0 {
		t.Fatal("expected checkpoint to capture state")
	}
	cp := checkpoints[0]
	store.seed(checkpointID, cp)
	if _, ok := cp.Received["mid"]; !ok {
		t.Fatalf("expected checkpoint to mark mid as activated, got %#v", cp.Received)
	}

	// Resume on fresh executor; start should not rerun, ready queue must be rebuilt for mid.
	var secondCounts struct {
		start  int32
		mid    int32
		finish int32
	}
	g2 := New(WithParallel(false))
	g2.AddNode("start", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&secondCounts.start, 1)
		return state, nil
	})
	g2.AddNode("mid", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&secondCounts.mid, 1)
		val := getIntFromState(state, valueKey)
		state[valueKey] = val + 1
		return state, nil
	})
	g2.AddNode("finish", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&secondCounts.finish, 1)
		val := getIntFromState(state, valueKey)
		state[valueKey] = val + 1
		return state, nil
	})
	g2.AddEdge("start", "mid")
	g2.AddEdge("mid", "finish")
	g2.SetEntryPoint("start")
	g2.SetFinishPoint("finish")

	exec2, err := g2.Compile(WithCheckpointer(store))
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	state, err := exec2.Resume(context.Background(), State{}, WithCheckpointID(checkpointID))
	if err != nil {
		t.Fatalf("resume error: %v", err)
	}

	if secondCounts.start != 0 {
		t.Fatalf("start should not rerun on resume, got %d", secondCounts.start)
	}
	if secondCounts.mid != 1 || secondCounts.finish != 1 {
		t.Fatalf("mid/finish should run once on resume, got mid=%d finish=%d", secondCounts.mid, secondCounts.finish)
	}
	if val := getIntFromState(state, valueKey); val != 3 {
		t.Fatalf("expected final value 3 after resume, got %d", val)
	}
}

func getIntFromState(state State, key string) int {
	raw, _ := state[key]
	if v, ok := raw.(int); ok {
		return v
	}
	return 0
}

func jsonEqual(t *testing.T, a, b any) bool {
	ta, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("jsonEqual marshal a: %v", err)
	}
	tb, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("jsonEqual marshal b: %v", err)
	}
	return string(ta) == string(tb)
}

func getStringSlice(value any) []string {
	if v, ok := value.([]string); ok {
		return v
	}
	return []string{}
}

func getStringSliceFromState(state State, key string) []string {
	raw, ok := state[key]
	if !ok {
		return []string{}
	}
	return getStringSlice(raw)
}

func TestCheckpointResumeParallelBranches(t *testing.T) {
	var counters struct {
		start   int32
		branchA int32
		branchB int32
		join    int32
	}

	g := New()
	g.AddNode("start", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&counters.start, 1)
		state[valueKey] = 1
		return state, nil
	})
	g.AddNode("branch_a", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&counters.branchA, 1)
		return state, nil
	})
	g.AddNode("branch_b", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&counters.branchB, 1)
		return state, nil
	})
	g.AddNode("join", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&counters.join, 1)
		return state, nil
	})
	g.AddEdge("start", "branch_a")
	g.AddEdge("start", "branch_b")
	g.AddEdge("branch_a", "join")
	g.AddEdge("branch_b", "join")
	g.SetEntryPoint("start")
	g.SetFinishPoint("join")

	store := newMemoryCheckpointer()
	const checkpointID = "parallel-branches"
	exec, err := g.Compile(WithCheckpointer(store))
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	// Capture checkpoint after start completes (branches ready but not executed)
	_, err = exec.Execute(context.Background(), State{}, WithCheckpointID(checkpointID))
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	checkpoints := store.snapshots(checkpointID)
	if len(checkpoints) == 0 {
		t.Fatal("expected checkpoint to capture state")
	}
	store.seed(checkpointID, checkpoints[0])

	// Reset counters and resume
	atomic.StoreInt32(&counters.start, 0)
	atomic.StoreInt32(&counters.branchA, 0)
	atomic.StoreInt32(&counters.branchB, 0)
	atomic.StoreInt32(&counters.join, 0)

	_, err = exec.Resume(context.Background(), State{}, WithCheckpointID(checkpointID))
	if err != nil {
		t.Fatalf("resume error: %v", err)
	}

	// Start was visited, should not rerun
	if atomic.LoadInt32(&counters.start) != 0 {
		t.Fatalf("start should not rerun, got %d", counters.start)
	}
	// Branches and join should run
	if atomic.LoadInt32(&counters.branchA) != 1 || atomic.LoadInt32(&counters.branchB) != 1 {
		t.Fatalf("branches should run once, got a=%d b=%d", counters.branchA, counters.branchB)
	}
	if atomic.LoadInt32(&counters.join) != 1 {
		t.Fatalf("join should run once, got %d", counters.join)
	}
}

func TestCheckpointResumeConditionalEdge(t *testing.T) {
	var counters struct {
		start   int32
		branchA int32
		branchB int32
		finish  int32
	}

	g := New(WithParallel(false))
	g.AddNode("start", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&counters.start, 1)
		state["route"] = "A"
		return state, nil
	})
	g.AddNode("branch_a", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&counters.branchA, 1)
		return state, nil
	})
	g.AddNode("branch_b", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&counters.branchB, 1)
		return state, nil
	})
	g.AddNode("finish", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&counters.finish, 1)
		return state, nil
	})
	// Both branches have conditions, but both lead to finish
	g.AddEdge("start", "branch_a", WithEdgeCondition(func(ctx context.Context, state State) bool {
		v, _ := state["route"]
		return v == "A"
	}))
	g.AddEdge("start", "branch_b", WithEdgeCondition(func(ctx context.Context, state State) bool {
		v, _ := state["route"]
		return v == "B"
	}))
	g.AddEdge("branch_a", "finish")
	g.AddEdge("branch_b", "finish")
	g.SetEntryPoint("start")
	g.SetFinishPoint("finish")

	store := newMemoryCheckpointer()
	exec, err := g.Compile(WithCheckpointer(store))
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	// Run full execution first
	_, err = exec.Execute(context.Background(), State{})
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	// branch_b should have been skipped (route=A, so only branch_a runs)
	if atomic.LoadInt32(&counters.branchA) != 1 {
		t.Fatalf("branch_a should run, got %d", counters.branchA)
	}
	if atomic.LoadInt32(&counters.branchB) != 0 {
		t.Fatalf("branch_b should be skipped, got %d", counters.branchB)
	}

	// Now test resume: create checkpoint after start with branch_b already skipped
	checkpoint := &Checkpoint{
		Received: map[string]int{"start": 1, "branch_a": 1},
		Visited:  map[string]bool{"start": true, "branch_b": true}, // branch_b marked as skipped
		State:    map[string]any{"route": "A"},
	}

	// Reset counters
	atomic.StoreInt32(&counters.start, 0)
	atomic.StoreInt32(&counters.branchA, 0)
	atomic.StoreInt32(&counters.branchB, 0)
	atomic.StoreInt32(&counters.finish, 0)

	store.seed("conditional-edge", checkpoint)

	_, err = exec.Resume(context.Background(), State{}, WithCheckpointID("conditional-edge"))
	if err != nil {
		t.Fatalf("resume error: %v", err)
	}

	// start should not rerun
	if atomic.LoadInt32(&counters.start) != 0 {
		t.Fatalf("start should not rerun, got %d", counters.start)
	}
	// branch_a should run, branch_b should stay skipped
	if atomic.LoadInt32(&counters.branchA) != 1 {
		t.Fatalf("branch_a should run on resume, got %d", counters.branchA)
	}
	if atomic.LoadInt32(&counters.branchB) != 0 {
		t.Fatalf("branch_b should remain skipped on resume, got %d", counters.branchB)
	}
}

func TestCheckpointResumeFromFinished(t *testing.T) {
	var runCount int32

	g := New(WithParallel(false))
	g.AddNode("start", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&runCount, 1)
		return state, nil
	})
	g.AddNode("finish", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&runCount, 1)
		return state, nil
	})
	g.AddEdge("start", "finish")
	g.SetEntryPoint("start")
	g.SetFinishPoint("finish")

	store := newMemoryCheckpointer()
	const checkpointID = "finished"
	exec, err := g.Compile(WithCheckpointer(store))
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	// Capture final checkpoint
	_, err = exec.Execute(context.Background(), State{}, WithCheckpointID(checkpointID))
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	checkpoints := store.snapshots(checkpointID)
	if len(checkpoints) == 0 {
		t.Fatal("expected checkpoints to be recorded")
	}
	finalCheckpoint := checkpoints[len(checkpoints)-1]
	if !finalCheckpoint.Visited["finish"] {
		t.Fatal("finish should be visited in final checkpoint")
	}
	store.seed(checkpointID, finalCheckpoint)

	// Reset and resume from finished state
	atomic.StoreInt32(&runCount, 0)

	_, err = exec.Resume(context.Background(), State{}, WithCheckpointID(checkpointID))
	if err != nil {
		t.Fatalf("resume error: %v", err)
	}

	// Nothing should run - already finished
	if atomic.LoadInt32(&runCount) != 0 {
		t.Fatalf("nothing should run when resuming finished graph, got %d", runCount)
	}
}

func TestCheckpointResumeEmptyCheckpoint(t *testing.T) {
	var runCount int32

	g := New(WithParallel(false))
	g.AddNode("start", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&runCount, 1)
		state[valueKey] = 42
		return state, nil
	})
	g.AddNode("finish", func(ctx context.Context, state State) (State, error) {
		atomic.AddInt32(&runCount, 1)
		return state, nil
	})
	g.AddEdge("start", "finish")
	g.SetEntryPoint("start")
	g.SetFinishPoint("finish")

	store := newMemoryCheckpointer()
	const checkpointID = "empty"
	exec, err := g.Compile(WithCheckpointer(store))
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	// Resume with empty checkpoint (nothing visited)
	emptyCheckpoint := &Checkpoint{
		Received: map[string]int{"start": 1}, // entry activated
		Visited:  map[string]bool{},
		State:    map[string]any{},
	}

	store.seed(checkpointID, emptyCheckpoint)

	state, err := exec.Resume(context.Background(), State{}, WithCheckpointID(checkpointID))
	if err != nil {
		t.Fatalf("resume error: %v", err)
	}

	// Both nodes should run
	if atomic.LoadInt32(&runCount) != 2 {
		t.Fatalf("expected 2 nodes to run, got %d", runCount)
	}

	val := getIntFromState(state, valueKey)
	if val != 42 {
		t.Fatalf("expected value 42, got %d", val)
	}
}

func TestCheckpointResumeMultiLevelFanOut(t *testing.T) {
	// Graph: start -> (a, b) -> mid -> (c, d) -> finish
	var order []string
	var mu sync.Mutex
	record := func(name string) func(context.Context, State) (State, error) {
		return func(ctx context.Context, state State) (State, error) {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			return state, nil
		}
	}

	g := New(WithParallel(false))
	g.AddNode("start", record("start"))
	g.AddNode("a", record("a"))
	g.AddNode("b", record("b"))
	g.AddNode("mid", record("mid"))
	g.AddNode("c", record("c"))
	g.AddNode("d", record("d"))
	g.AddNode("finish", record("finish"))

	g.AddEdge("start", "a")
	g.AddEdge("start", "b")
	g.AddEdge("a", "mid")
	g.AddEdge("b", "mid")
	g.AddEdge("mid", "c")
	g.AddEdge("mid", "d")
	g.AddEdge("c", "finish")
	g.AddEdge("d", "finish")
	g.SetEntryPoint("start")
	g.SetFinishPoint("finish")

	store := newMemoryCheckpointer()
	const checkpointID = "multi-fanout"
	exec, err := g.Compile(WithCheckpointer(store))
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	// Capture checkpoint after mid completes
	_, err = exec.Execute(context.Background(), State{}, WithCheckpointID(checkpointID))
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	checkpoints := store.snapshots(checkpointID)

	// Find checkpoint where mid is visited but finish is not
	var midCheckpoint *Checkpoint
	for _, cp := range checkpoints {
		if cp.Visited["mid"] && !cp.Visited["finish"] {
			midCheckpoint = cp
			break
		}
	}
	if midCheckpoint.Visited == nil {
		t.Fatal("could not find checkpoint after mid")
	}
	store.seed(checkpointID, midCheckpoint)

	// Reset and resume
	mu.Lock()
	order = nil
	mu.Unlock()

	_, err = exec.Resume(context.Background(), State{}, WithCheckpointID(checkpointID))
	if err != nil {
		t.Fatalf("resume error: %v", err)
	}

	mu.Lock()
	resumed := order
	mu.Unlock()

	// start, a, b, mid should not run; c, d, finish should run
	for _, node := range []string{"start", "a", "b", "mid"} {
		for _, ran := range resumed {
			if ran == node {
				t.Fatalf("node %s should not run on resume", node)
			}
		}
	}
	expectedToRun := map[string]bool{"c": false, "d": false, "finish": false}
	for _, ran := range resumed {
		expectedToRun[ran] = true
	}
	for node, ran := range expectedToRun {
		if !ran {
			t.Fatalf("node %s should have run on resume", node)
		}
	}
}
