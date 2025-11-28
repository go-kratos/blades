package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/go-kratos/blades/graph"
)

func flakyProcessor(maxFailures int) graph.Handler {
	attempts := 0
	return func(ctx context.Context, state graph.State) error {
		attempts++
		log.Printf("[process] attempt %d", attempts)
		if attempts <= maxFailures {
			return fmt.Errorf("transient failure %d/%d", attempts, maxFailures)
		}

		state.Store("attempts", attempts)
		state.Store("processed_at", time.Now().Format(time.RFC3339Nano))
		return nil
	}
}

func main() {
	g := graph.New(graph.WithMiddleware(graph.Retry(3)))

	g.AddNode("start", func(ctx context.Context, state graph.State) error {
		log.Println("[start] preparing work item")
		state.Store("payload", "retry-demo")
		return nil
	})

	g.AddNode("process", flakyProcessor(2))

	g.AddNode("finish", func(ctx context.Context, state graph.State) error {
		attempts, _ := state.Load("attempts")
		processedAt, _ := state.Load("processed_at")
		log.Printf("[finish] workflow complete. attempts=%v processed_at=%v", attempts, processedAt)
		return nil
	})

	g.AddEdge("start", "process")
	g.AddEdge("process", "finish")
	g.SetEntryPoint("start")
	g.SetFinishPoint("finish")

	executor, err := g.Compile()
	if err != nil {
		log.Fatalf("compile error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	state := graph.NewState()
	taskID, err := executor.Execute(ctx, state)
	if err != nil {
		log.Fatalf("execution error: %v", err)
	}

	log.Printf("task %s final state: %+v", taskID, state.Snapshot())
}
