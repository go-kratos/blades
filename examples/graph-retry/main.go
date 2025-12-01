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
	return func(ctx context.Context, state graph.State) (graph.State, error) {
		attempts++
		log.Printf("[process] attempt %d", attempts)
		if attempts <= maxFailures {
			return nil, fmt.Errorf("transient failure %d/%d", attempts, maxFailures)
		}

		state["attempts"] = attempts
		state["processed_at"] = time.Now().Format(time.RFC3339Nano)
		return state, nil
	}
}

func main() {
	g := graph.New(graph.WithMiddleware(graph.Retry(3)))

	g.AddNode("start", func(ctx context.Context, state graph.State) (graph.State, error) {
		log.Println("[start] preparing work item")
		state["payload"] = "retry-demo"
		return state, nil
	})

	g.AddNode("process", flakyProcessor(2))

	g.AddNode("finish", func(ctx context.Context, state graph.State) (graph.State, error) {
		attempts, _ := state["attempts"]
		processedAt, _ := state["processed_at"]
		log.Printf("[finish] workflow complete. attempts=%v processed_at=%v", attempts, processedAt)
		return state, nil
	})

	g.AddEdge("start", "process")
	g.AddEdge("process", "finish")
	g.SetEntryPoint("start")
	g.SetFinishPoint("finish")

	executor, err := g.Compile()
	if err != nil {
		log.Fatalf("compile error: %v", err)
	}

	state, err := executor.Execute(context.Background(), graph.State{})
	if err != nil {
		log.Fatalf("execution error: %v", err)
	}
	log.Printf("task final state: %+v", state)
}
