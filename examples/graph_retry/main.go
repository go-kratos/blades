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

		next := state.Clone()
		next["attempts"] = attempts
		next["processed_at"] = time.Now().Format(time.RFC3339Nano)
		return next, nil
	}
}

func main() {
	g := graph.New(graph.WithMiddleware(graph.Retry(3)))

	g.AddNode("start", func(ctx context.Context, state graph.State) (graph.State, error) {
		log.Println("[start] preparing work item")
		next := state.Clone()
		next["payload"] = "retry-demo"
		return next, nil
	})

	g.AddNode("process", flakyProcessor(2))

	g.AddNode("finish", func(ctx context.Context, state graph.State) (graph.State, error) {
		log.Printf("[finish] workflow complete. attempts=%v processed_at=%v", state["attempts"], state["processed_at"])
		return state.Clone(), nil
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

	state, err := executor.Execute(ctx, graph.State{})
	if err != nil {
		log.Fatalf("execution error: %v", err)
	}

	log.Printf("final state: %+v", state)
}
