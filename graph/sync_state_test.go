package graph

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
)

const (
	stepsKey = "steps"
	valueKey = "value"
)

func TestSequentialExecutionSharedState(t *testing.T) {
	g := New(WithParallel(false))
	g.AddNode("start", func(ctx context.Context, state State) error {
		state.Store(stepsKey, []string{"start"})
		return nil
	})
	g.AddNode("finish", func(ctx context.Context, state State) error {
		raw, _ := state.Load(stepsKey)
		steps := getStringSlice(raw)
		steps = append(steps, "finish")
		state.Store(stepsKey, steps)
		return nil
	})
	g.AddEdge("start", "finish")
	g.SetEntryPoint("start")
	g.SetFinishPoint("finish")

	exec, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	final, err := exec.Execute(context.Background(), NewState())
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	steps := getStringSliceFromState(final, stepsKey)
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

	g.AddNode("start", func(ctx context.Context, state State) error {
		atomic.AddInt32(&counters.start, 1)
		state.Store(valueKey, 1)
		return nil
	})
	g.AddNode("mid", func(ctx context.Context, state State) error {
		atomic.AddInt32(&counters.mid, 1)
		raw, _ := state.Load(valueKey)
		v, _ := raw.(int)
		state.Store(valueKey, v+1)
		return nil
	})
	g.AddNode("finish", func(ctx context.Context, state State) error {
		atomic.AddInt32(&counters.finish, 1)
		raw, _ := state.Load(valueKey)
		v, _ := raw.(int)
		state.Store(valueKey, v+1)
		return nil
	})
	g.AddEdge("start", "mid")
	g.AddEdge("mid", "finish")
	g.SetEntryPoint("start")
	g.SetFinishPoint("finish")

	exec1, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	var captured Checkpoint
	_, err = exec1.Execute(context.Background(), NewState(), WithCheckpointSaver(CheckpointSaverFunc(func(cp Checkpoint) {
		if captured.State != nil {
			return
		}
		captured = cp.Clone()
	})))
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if captured.State == nil {
		t.Fatal("expected checkpoint to capture state")
	}
	if atomic.LoadInt32(&counters.start) != 1 || atomic.LoadInt32(&counters.mid) != 1 || atomic.LoadInt32(&counters.finish) != 1 {
		t.Fatalf("unexpected counters after first run: %#v", counters)
	}

	exec2, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	final, err := exec2.Execute(context.Background(), NewState(), WithCheckpointResume(captured))
	if err != nil {
		t.Fatalf("resume error: %v", err)
	}

	if atomic.LoadInt32(&counters.start) != 1 {
		t.Fatalf("start should not rerun on resume, got %d", counters.start)
	}
	if atomic.LoadInt32(&counters.mid) != 2 || atomic.LoadInt32(&counters.finish) != 2 {
		t.Fatalf("mid/finish should run again on resume, counters=%#v", counters)
	}

	val := getIntFromState(final, valueKey)
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

	data, err := original.Marshal()
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded Checkpoint
	if err := decoded.Unmarshal(data); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if !jsonEqual(t, original, decoded) {
		t.Fatalf("decoded checkpoint mismatch: %#v vs %#v", decoded, original)
	}
}

func getIntFromState(state State, key string) int {
	raw, _ := state.Load(key)
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
	raw, ok := state.Load(key)
	if !ok {
		return []string{}
	}
	return getStringSlice(raw)
}
