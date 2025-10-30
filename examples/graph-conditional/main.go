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
	g := graph.NewGraph()

	// Define node handlers using the helper function
	g.AddNode("start", nodeHandler("start", func(ctx context.Context, state graph.State) (graph.State, error) {
		state[valueKey] = 42
		return state, nil
	}))

	g.AddNode("decision", nodeHandler("decision", func(ctx context.Context, state graph.State) (graph.State, error) {
		log.Printf("[decision] value=%v", state[valueKey])
		return state, nil
	}))

	g.AddNode("positive", nodeHandler("positive", func(ctx context.Context, state graph.State) (graph.State, error) {
		state["result"] = fmt.Sprintf("%v is non-negative", state[valueKey])
		return state, nil
	}))

	g.AddNode("negative", nodeHandler("negative", func(ctx context.Context, state graph.State) (graph.State, error) {
		state["result"] = fmt.Sprintf("%v is negative", state[valueKey])
		return state, nil
	}))

	g.AddNode("finish", nodeHandler("finish", nil))

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

// nodeHandler wraps a handler function to automatically track the execution path.
// It adds the node name to the path and then executes the custom logic if provided.
func nodeHandler(nodeName string, customLogic graph.Handler) graph.Handler {
	return func(ctx context.Context, state graph.State) (graph.State, error) {
		// Track execution path
		state[pathKey] = append(getPath(state), nodeName)

		// Execute custom logic if provided
		if customLogic != nil {
			return customLogic(ctx, state)
		}

		return state, nil
	}
}

func getPath(state graph.State) []string {
	if v, ok := state[pathKey].([]string); ok {
		return v
	}
	return nil
}
