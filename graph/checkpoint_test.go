package graph

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestExecuteCheckpointCallbackParallel(t *testing.T) {
	g := New()

	slowStarted := make(chan struct{})
	releaseSlow := make(chan struct{})
	var startOnce sync.Once
	slowHandler := func(ctx context.Context, state State) (State, error) {
		startOnce.Do(func() { close(slowStarted) })
		select {
		case <-releaseSlow:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return stepHandler("slow")(ctx, state)
	}

	g.AddNode("start", stepHandler("start"))
	g.AddNode("fast", stepHandler("fast"))
	g.AddNode("slow", slowHandler)
	g.AddNode("finish", stepHandler("finish"))

	g.AddEdge("start", "fast")
	g.AddEdge("start", "slow")
	g.AddEdge("fast", "finish")
	g.AddEdge("slow", "finish")
	g.SetEntryPoint("start")
	g.SetFinishPoint("finish")

	executor, err := g.Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	var (
		mu          sync.Mutex
		checkpoints []Checkpoint
		runErr      error
		result      State
	)

	done := make(chan struct{})
	go func() {
		defer close(done)
		result, runErr = executor.Execute(context.Background(), State{}, WithCheckpointCallback(func(cp Checkpoint) {
			mu.Lock()
			checkpoints = append(checkpoints, cp.Clone())
			mu.Unlock()
		}))
	}()

	if err := waitForChannel(slowStarted, time.Second); err != nil {
		t.Fatalf("slow branch did not start: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if len(checkpoints) != 1 {
		mu.Unlock()
		t.Fatalf("expected single checkpoint while slow branch in-flight, got %d", len(checkpoints))
	}
	first := checkpoints[0]
	mu.Unlock()

	if !first.Visited["start"] {
		t.Fatalf("first checkpoint missing start visit: %#v", first)
	}
	if !containsAll(first.Ready, "fast", "slow") {
		t.Fatalf("first checkpoint missing ready branches: %#v", first.Ready)
	}

	close(releaseSlow)

	if err := waitForChannel(done, time.Second); err != nil {
		t.Fatalf("execution did not finish: %v", err)
	}
	if runErr != nil {
		t.Fatalf("execute error: %v", runErr)
	}

	mu.Lock()
	total := len(checkpoints)
	var second, third Checkpoint
	if total >= 2 {
		second = checkpoints[1]
	}
	if total >= 3 {
		third = checkpoints[2]
	}
	mu.Unlock()

	if total != 3 {
		t.Fatalf("expected 3 checkpoints, got %d", total)
	}
	if !second.Visited["fast"] || !second.Visited["slow"] {
		t.Fatalf("second checkpoint should include both branches visited: %#v", second.Visited)
	}
	if len(second.Ready) != 1 || second.Ready[0] != "finish" {
		t.Fatalf("finish should be ready after both branches, got %#v", second.Ready)
	}
	if !third.Finished {
		t.Fatalf("final checkpoint should be marked finished: %#v", third)
	}

	steps := getStringSlice(result[stepsKey])
	if len(steps) == 0 || steps[len(steps)-1] != "finish" {
		t.Fatalf("unexpected final steps: %v", steps)
	}
}

func TestExecuteResumeParallelCheckpoint(t *testing.T) {
	type counters struct {
		start   int
		branchA int
		branchB int
		finish  int
	}

	build := func(c *counters) *Graph {
		g := New()
		g.AddNode("start", func(ctx context.Context, state State) (State, error) {
			c.start++
			return incrementHandler(1)(ctx, state)
		})
		g.AddNode("branch_a", func(ctx context.Context, state State) (State, error) {
			c.branchA++
			return incrementHandler(10)(ctx, state)
		})
		g.AddNode("branch_b", func(ctx context.Context, state State) (State, error) {
			c.branchB++
			return incrementHandler(100)(ctx, state)
		})
		g.AddNode("finish", func(ctx context.Context, state State) (State, error) {
			c.finish++
			return incrementHandler(1000)(ctx, state)
		})

		g.AddEdge("start", "branch_a")
		g.AddEdge("start", "branch_b")
		g.AddEdge("branch_a", "finish")
		g.AddEdge("branch_b", "finish")
		g.SetEntryPoint("start")
		g.SetFinishPoint("finish")
		return g
	}

	firstCounters := &counters{}
	exec1, err := build(firstCounters).Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	var (
		captured bool
		cp       Checkpoint
	)
	fullResult, err := exec1.Execute(context.Background(), State{}, WithCheckpointCallback(func(snapshot Checkpoint) {
		if captured {
			return
		}
		captured = true
		cp = snapshot.Clone()
	}))
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if !captured {
		t.Fatal("expected a checkpoint to be captured")
	}
	if firstCounters.start != 1 || firstCounters.branchA != 1 || firstCounters.branchB != 1 || firstCounters.finish != 1 {
		t.Fatalf("unexpected execution counts in first run: %#v", firstCounters)
	}

	secondCounters := &counters{}
	exec2, err := build(secondCounters).Compile()
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	resumedResult, err := exec2.Execute(context.Background(), nil, WithCheckpointResume(cp))
	if err != nil {
		t.Fatalf("resume error: %v", err)
	}

	if secondCounters.start != 0 {
		t.Fatalf("start should not run on resume, counters=%#v", secondCounters)
	}
	if secondCounters.branchA != 1 || secondCounters.branchB != 1 || secondCounters.finish != 1 {
		t.Fatalf("branches and finish should run once on resume, counters=%#v", secondCounters)
	}

	if fullResult[valueKey] != resumedResult[valueKey] {
		t.Fatalf("expected resumed result %#v to match full run %#v", resumedResult[valueKey], fullResult[valueKey])
	}
}

func waitForChannel(ch <-chan struct{}, timeout time.Duration) error {
	select {
	case <-ch:
		return nil
	case <-time.After(timeout):
		return context.DeadlineExceeded
	}
}
