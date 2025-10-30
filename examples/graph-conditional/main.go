package main

import (
	"context"
	"fmt"
	"log"

	"github.com/go-kratos/blades/graph"
)

const valueKey = "value"
const pathKey = "path"

func main() {
	g := graph.NewGraph(graph.WithParallel(true))

	g.AddNode("start", func(ctx context.Context, state graph.State) (graph.State, error) {
		next := state.Clone()
		next[valueKey] = 42
		next[pathKey] = append(getPath(next), "start")
		return next, nil
	})

	g.AddNode("decision", func(ctx context.Context, state graph.State) (graph.State, error) {
		next := state.Clone()
		next[pathKey] = append(getPath(next), "decision")
		log.Printf("[decision] value=%v", next[valueKey])
		return next, nil
	})

	g.AddNode("positive", func(ctx context.Context, state graph.State) (graph.State, error) {
		next := state.Clone()
		next[pathKey] = append(getPath(next), "positive")
		next["result"] = fmt.Sprintf("%v is non-negative", next[valueKey])
		return next, nil
	})

	g.AddNode("negative", func(ctx context.Context, state graph.State) (graph.State, error) {
		next := state.Clone()
		next[pathKey] = append(getPath(next), "negative")
		next["result"] = fmt.Sprintf("%v is negative", next[valueKey])
		return next, nil
	})

	g.AddNode("finish", func(ctx context.Context, state graph.State) (graph.State, error) {
		next := state.Clone()
		next[pathKey] = append(getPath(next), "finish")
		return next, nil
	})

	g.AddEdge("start", "decision")
	g.AddEdge("decision", "positive", graph.WithEdgeCondition(func(_ context.Context, state graph.State) bool {
		v, _ := state[valueKey].(int)
		return v >= 0
	}))
	g.AddEdge("decision", "negative", graph.WithEdgeCondition(func(_ context.Context, state graph.State) bool {
		v, _ := state[valueKey].(int)
		return v < 0
	}))
	g.AddEdge("positive", "finish")
	g.AddEdge("negative", "finish")

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

	log.Printf("path=%v", state[pathKey])
	log.Printf("result=%v", state["result"])
}

func getPath(state graph.State) []string {
	if v, ok := state[pathKey].([]string); ok {
		return v
	}
	return nil
}
