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

func getString(state graph.State, key string) (string, bool) {
	if v, ok := state[key]; ok {
		if s, ok := v.(string); ok {
			return s, true
		}
	}
	return "", false
}

func getInt(state graph.State, key string) (int, bool) {
	if v, ok := state[key]; ok {
		if i, ok := v.(int); ok {
			return i, true
		}
	}
	return 0, false
}

func outline(ctx context.Context, state graph.State) (graph.State, error) {
	next := state.Clone()
	if draft, ok := getString(next, stateKeyDraft); !ok || draft == "" {
		next[stateKeyDraft] = "Outline TODO: add twist."
	}
	if _, ok := getInt(next, stateKeyRevision); !ok {
		next[stateKeyRevision] = 0
	}
	return next, nil
}

func review(ctx context.Context, state graph.State) (graph.State, error) {
	return state.Clone(), nil
}

func revise(ctx context.Context, state graph.State) (graph.State, error) {
	draft, _ := getString(state, stateKeyDraft)
	revision, _ := getInt(state, stateKeyRevision)
	revision++

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
		func() int {
			if revision, ok := getInt(state, stateKeyRevision); ok {
				return revision
			}
			return 0
		}(),
		func() string {
			if draft, ok := getString(state, stateKeyDraft); ok {
				return draft
			}
			return ""
		}(),
	)
	return state.Clone(), nil
}

func needsRevision(state graph.State, max int) bool {
	draft, _ := getString(state, stateKeyDraft)
	revision, _ := getInt(state, stateKeyRevision)
	return strings.Contains(draft, "TODO") && revision < max
}

func publishReady(state graph.State, max int) bool {
	draft, _ := getString(state, stateKeyDraft)
	revision, _ := getInt(state, stateKeyRevision)
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
