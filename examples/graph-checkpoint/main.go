package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/go-kratos/blades/graph"
)

var ErrProcessApproval = errors.New("approval is required")

type checkpointStore struct {
	checkpoints map[string]*graph.Checkpoint
}

func NewCheckpointStore() *checkpointStore {
	return &checkpointStore{
		checkpoints: make(map[string]*graph.Checkpoint),
	}
}

func (s *checkpointStore) Save(ctx context.Context, checkpoint *graph.Checkpoint) error {
	s.checkpoints[checkpoint.ID] = checkpoint.Clone()
	return nil
}

func (s *checkpointStore) Resume(ctx context.Context, checkpointID string) (*graph.Checkpoint, error) {
	if cp, ok := s.checkpoints[checkpointID]; ok {
		return cp.Clone(), nil
	}
	return nil, fmt.Errorf("checkpoint %s not found", checkpointID)
}

func main() {
	g := graph.New(graph.WithMiddleware(graph.Retry(3)))
	// Define nodes
	g.AddNode("start", func(ctx context.Context, state graph.State) error {
		state.Store("start", true)
		return nil
	})
	g.AddNode("process", func(ctx context.Context, state graph.State) error {
		state.Store("process", true)
		approved, ok := state.Load("approved")
		if !ok || !approved.(bool) {
			return ErrProcessApproval
		}
		return nil
	})
	g.AddNode("finish", func(ctx context.Context, state graph.State) error {
		state.Store("finish", true)
		return nil
	})
	// Define edges
	g.AddEdge("start", "process")
	g.AddEdge("process", "finish")
	g.SetEntryPoint("start")
	g.SetFinishPoint("finish")
	// Compile and execute the graph
	checkpointID := "checkpoint_1"
	checkpointer := NewCheckpointStore()
	executor, err := g.Compile(graph.WithCheckpointer(checkpointer))
	if err != nil {
		log.Fatalf("compile error: %v", err)
	}

	// Execute the graph with checkpointing
	initState := graph.NewState()
	if err := executor.Execute(context.Background(), initState, graph.WithCheckpointID(checkpointID)); err != nil {
		if !errors.Is(err, ErrProcessApproval) {
			log.Fatalf("execute error: %v", err)
		}
		log.Printf("execution paused waiting for approval: %v", err)
	} else {
		log.Println("task completed without approval, no resume needed")
		return
	}

	// Simulate approval and resume execution
	resumeState := graph.NewState(map[string]any{
		"approved": true,
	})
	if err := executor.Resume(context.Background(), resumeState, graph.WithCheckpointID(checkpointID)); err != nil {
		log.Fatal(err)
	}

	log.Printf("resumed from checkpoint %s, final state: %+v", checkpointID, resumeState.Snapshot())
}
