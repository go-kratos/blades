package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/go-kratos/blades/graph"
)

const maxRevisions = 3

const (
	stateKeyRevision = "revision"
	stateKeyDraft    = "draft"
)

func outline(ctx context.Context, state graph.State) (graph.State, error) {
	next := state.Clone()
	if _, ok := next[stateKeyDraft]; !ok {
		next[stateKeyDraft] = "Outline TODO: add twist."
	}
	return next, nil
}

func review(ctx context.Context, state graph.State) (graph.State, error) {
	return state.Clone(), nil
}

func revise(ctx context.Context, state graph.State) (graph.State, error) {
	draft := state[stateKeyDraft].(string)
	revision := state[stateKeyRevision].(int) + 1

	// Apply revision-specific updates
	draft = strings.Replace(draft, "TODO: add twist.", "A surprise reveal changes everything.", 1)
	switch revision {
	case 1:
		draft += " TODO: refine ending."
	case 2:
		draft = strings.Replace(draft, " TODO: refine ending.", " An epilogue wraps the journey.", 1)
	}

	state[stateKeyRevision] = revision
	state[stateKeyDraft] = draft
	return state, nil
}

func publish(ctx context.Context, state graph.State) (graph.State, error) {
	fmt.Printf(
		"Final draft after %d revision(s): %s\n",
		state[stateKeyRevision].(int),
		state[stateKeyDraft].(string),
	)
	return state.Clone(), nil
}

func needsRevision(state graph.State, max int) bool {
	draft := state[stateKeyDraft].(string)
	revision := state[stateKeyRevision].(int)
	return strings.Contains(draft, "TODO") && revision < max
}

func publishReady(state graph.State, max int) bool {
	draft := state[stateKeyDraft].(string)
	revision := state[stateKeyRevision].(int)
	return !strings.Contains(draft, "TODO") || revision >= max
}

func main() {
	rand.Seed(time.Now().UnixNano())

	g := graph.NewGraph(graph.WithParallel(false))
	g.AddNode("outline", outline)
	g.AddNode("review", review)
	g.AddNode("revise", revise)
	g.AddNode("publish", publish)

	g.AddEdge("outline", "review")
	g.AddEdge("review", "revise", graph.WithEdgeCondition(func(ctx context.Context, state graph.State) bool {
		return needsRevision(state, maxRevisions)
	}))
	g.AddEdge("review", "publish", graph.WithEdgeCondition(func(ctx context.Context, state graph.State) bool {
		return publishReady(state, maxRevisions)
	}))
	g.AddEdge("revise", "review")

	g.SetEntryPoint("outline")
	g.SetFinishPoint("publish")

	executor, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}

	state, err := executor.Execute(context.Background(), graph.State{stateKeyRevision: 0})
	if err != nil {
		log.Fatal(err)
	}

	log.Println("final state:", state)
}
