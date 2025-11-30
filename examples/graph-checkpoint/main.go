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
	g.AddNode("start", func(ctx context.Context, state graph.State) (graph.State, error) {
		state["start"] = true
		return state, nil
	})
	g.AddNode("process", func(ctx context.Context, state graph.State) (graph.State, error) {
		state["process"] = true
		approved, ok := state["approved"].(bool)
		if !ok || !approved {
			return nil, ErrProcessApproval
		}
		return state, nil
	})
	g.AddNode("finish", func(ctx context.Context, state graph.State) (graph.State, error) {
		state["finish"] = true
		return state, nil
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
	state, err := executor.Execute(context.Background(), graph.State{}, graph.WithCheckpointID(checkpointID))
	if err != nil {
		if !errors.Is(err, ErrProcessApproval) {
			log.Fatalf("execute error: %v", err)
		}
		log.Printf("execution paused waiting for approval: %v", err)
	} else {
		log.Println("task completed without approval, no resume needed")
		return
	}
	log.Println("task paused, waiting for approval...", state)

	// Simulate approval and resume execution
	resumeState := graph.State{
		"approved": true,
	}
	finalState, err := executor.Resume(context.Background(), resumeState, graph.WithCheckpointID(checkpointID))
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("resumed from checkpoint %s, final state: %+v", checkpointID, finalState)
}
