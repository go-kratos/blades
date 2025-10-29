package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/go-kratos/blades/graph"
)

const maxRevisions = 3

func outline(ctx context.Context, state graph.State) (graph.State, error) {
	next := state.Clone()
	next["draft"] = "Outline TODO: add twist."
	next["revision"] = 0
	return next, nil
}

func review(ctx context.Context, state graph.State) (graph.State, error) {
	fmt.Printf("Reviewing draft: %s\n", state["draft"])
	return state.Clone(), nil
}

func revise(ctx context.Context, state graph.State) (graph.State, error) {
	draft := state["draft"].(string)
	revision := state["revision"].(int) + 1

	// Apply revision-specific updates
	draft = strings.Replace(draft, "TODO: add twist.", "A surprise reveal changes everything.", 1)
	switch revision {
	case 1:
		draft += " TODO: refine ending."
	case 2:
		draft = strings.Replace(draft, " TODO: refine ending.", " An epilogue wraps the journey.", 1)
	}

	next := state.Clone()
	next["revision"] = revision
	next["draft"] = draft
	return next, nil
}

func publish(ctx context.Context, state graph.State) (graph.State, error) {
	fmt.Printf("Final draft after %d revision(s): %s\n", state["revision"], state["draft"])
	return state.Clone(), nil
}

func main() {
	g := graph.NewGraph(graph.WithParallel(false))
	g.AddNode("outline", outline)
	g.AddNode("review", review)
	g.AddNode("revise", revise)
	g.AddNode("publish", publish)

	g.AddEdge("outline", "review")
	g.AddEdge("review", "revise", graph.WithEdgeCondition(func(ctx context.Context, state graph.State) bool {
		draft := state["draft"].(string)
		revision := state["revision"].(int)
		return strings.Contains(draft, "TODO") && revision < maxRevisions
	}))
	g.AddEdge("review", "publish", graph.WithEdgeCondition(func(ctx context.Context, state graph.State) bool {
		draft := state["draft"].(string)
		revision := state["revision"].(int)
		return !strings.Contains(draft, "TODO") || revision >= maxRevisions
	}))
	g.AddEdge("revise", "review")

	g.SetEntryPoint("outline")
	g.SetFinishPoint("publish")

	executor, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}

	state, err := executor.Execute(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Final state:", state)
}
