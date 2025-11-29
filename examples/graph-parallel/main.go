package main

import (
	"context"
	"log"
	"time"

	"github.com/go-kratos/blades/graph"
)

func logger(name string) graph.Handler {
	return func(ctx context.Context, state graph.State) error {
		time.Sleep(time.Millisecond * 500)
		log.Println("execute node:", name)
		state.Store(name, "visited")
		return nil
	}
}

func main() {
	g := graph.New()

	g.AddNode("start", logger("start"))
	g.AddNode("branch_a", logger("branch_a"))
	g.AddNode("branch_b", logger("branch_b"))
	g.AddNode("branch_c", logger("branch_c"))
	g.AddNode("branch_d", logger("branch_d"))
	g.AddNode("join", logger("join"))

	g.AddEdge("start", "branch_a")
	g.AddEdge("start", "branch_b")
	g.AddEdge("branch_b", "branch_c")
	g.AddEdge("branch_b", "branch_d")
	g.AddEdge("branch_c", "join")
	g.AddEdge("branch_d", "join")
	g.AddEdge("branch_a", "join")

	g.SetEntryPoint("start")
	g.SetFinishPoint("join")

	executor, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}

	state := graph.NewState()
	if err := executor.Execute(context.Background(), state); err != nil {
		log.Fatal(err)
	}
	log.Printf("task final state: %+v", state.Snapshot())
}
