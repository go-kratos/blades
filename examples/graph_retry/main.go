package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/go-kratos/blades/graph"
	"github.com/go-kratos/exp/backoff"
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
	retry := graph.Retry(
		graph.WithAttempts(5),
		graph.WithBackoff(backoff.New(
			backoff.WithBaseDelay(200*time.Millisecond),
			backoff.WithMaxDelay(2*time.Second),
		)),
	)

	g := graph.NewGraph(graph.WithMiddleware(retry))

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
