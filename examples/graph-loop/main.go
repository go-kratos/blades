package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades/graph"
)

func main() {
	// Disable parallel fan-out to keep the output ordered.
	g := graph.NewGraph(graph.WithParallel(false))

	g.AddNode("start", func(ctx context.Context, _ graph.State) (graph.State, error) {
		return graph.State{
			"attempt":      0,
			"max_attempts": 4,
			"success":      false,
		}, nil
	})

	g.AddNode("try_operation", func(ctx context.Context, state graph.State) (graph.State, error) {
		next := state.Clone()
		attempt := next["attempt"].(int) + 1
		maxAttempts := next["max_attempts"].(int)
		// Pretend the operation succeeds after two retries.
		success := attempt >= 3
		log.Printf("attempt %d/%d -> success=%v", attempt, maxAttempts, success)
		next["attempt"] = attempt
		next["success"] = success
		return next, nil
	})

	g.AddNode("finish", func(ctx context.Context, state graph.State) (graph.State, error) {
		log.Printf("finished after %d attempts (success=%v)", state["attempt"], state["success"])
		return state, nil
	})

	g.AddEdge("start", "try_operation")

	// Retry loop: re-enter try_operation while it keeps failing and attempts remain.
	g.AddEdge("try_operation", "try_operation",
		graph.WithEdgeCondition(func(_ context.Context, state graph.State) bool {
			success := state["success"].(bool)
			attempt := state["attempt"].(int)
			maxAttempts := state["max_attempts"].(int)
			return !success && attempt < maxAttempts
		}),
		graph.WithEdgeType(graph.EdgeTypeLoop),
	)

	// Exit edge: break the loop once we succeed or hit the attempt limit.
	g.AddEdge("try_operation", "finish",
		graph.WithEdgeCondition(func(_ context.Context, state graph.State) bool {
			success := state["success"].(bool)
			attempt := state["attempt"].(int)
			maxAttempts := state["max_attempts"].(int)
			return success || attempt >= maxAttempts
		}),
		graph.WithEdgeType(graph.EdgeTypeExit),
	)

	g.SetEntryPoint("start")
	g.SetFinishPoint("finish")

	executor, err := g.Compile()
	if err != nil {
		log.Fatalf("compile graph: %v", err)
	}

	state, err := executor.Execute(context.Background(), graph.State{})
	if err != nil {
		log.Fatalf("execute graph: %v", err)
	}

	log.Printf("final state: %+v", state)
}
