package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades/graph"
)

func logger(name string) graph.Handler {
	return func(ctx context.Context, state graph.State) (graph.State, error) {
		log.Println("execute node:", name)
		return state, nil
	}
}

func main() {
	g := graph.NewGraph()

	// Define node handlers using the helper function
	g.AddNode("start", logger("start"))
	g.AddNode("decision", logger("decision"))
	g.AddNode("positive", logger("positive"))
	g.AddNode("negative", logger("negative"))
	g.AddNode("finish", logger("finish"))

	g.AddEdge("start", "decision")
	g.AddEdge("decision", "positive", graph.WithEdgeCondition(func(_ context.Context, state graph.State) bool {
		return state["n"].(int) > 0
	}))
	g.AddEdge("decision", "negative", graph.WithEdgeCondition(func(_ context.Context, state graph.State) bool {
		return state["n"].(int) < 0
	}))
	g.AddEdge("positive", "finish")
	g.AddEdge("negative", "finish")

	g.SetEntryPoint("start")
	g.SetFinishPoint("finish")

	executor, err := g.Compile()
	if err != nil {
		log.Fatalf("compile error: %v", err)
	}

	state, err := executor.Execute(context.Background(), graph.State{"n": 100})
	if err != nil {
		log.Fatalf("execution error: %v", err)
	}
	log.Println(state)
}
