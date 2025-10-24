package main

import (
	"context"
	"log"
	"strings"

	"github.com/go-kratos/blades/flow"
)

type storyState struct {
	Draft    string
	Revision int
}

func outline(ctx context.Context, state storyState) (storyState, error) {
	if state.Draft == "" {
		state.Draft = "A traveler arrives in a mysterious town. TODO: describe the climax."
	}
	log.Printf("Outline ready: %q", state.Draft)
	return state, nil
}

func review(ctx context.Context, state storyState) (storyState, error) {
	if needsRevision(state) {
		log.Printf("Review %d: draft needs another pass.", state.Revision)
	} else {
		log.Printf("Review %d: draft looks solid.", state.Revision)
	}
	return state, nil
}

func revise(ctx context.Context, state storyState) (storyState, error) {
	state.Revision++
	if state.Revision == 1 {
		state.Draft = strings.Replace(state.Draft, "TODO", "The final showdown erupts under stormy skies", 1)
		state.Draft += "TODO: add a closing paragraph."
	} else if state.Revision == 2 {
		state.Draft = strings.Replace(state.Draft, "TODO: add a closing paragraph.", "In the aftermath, the traveler reflects on their journey.", 1)
	}
	log.Printf("Revision %d applied: %q", state.Revision, state.Draft)
	return state, nil
}

func finalize(ctx context.Context, state storyState) (storyState, error) {
	log.Printf("Final review: %q", state.Draft)
	return state, nil
}

func needsRevision(state storyState) bool {
	return strings.Contains(strings.ToUpper(state.Draft), "TODO")
}

func main() {
	const maxRevisions = 3

	g := flow.NewGraph[storyState]()
	g.AddNode("outline", outline)
	g.AddNode("review", review)
	g.AddNode("revise", revise)
	g.AddNode("final", finalize)

	// loop
	g.AddEdge("outline", "review")
	g.AddEdge("review", "revise", flow.WithEdgeCondition(func(state storyState) bool {
		return needsRevision(state) && state.Revision < maxRevisions
	}))
	g.AddEdge("review", "final", flow.WithEdgeCondition(func(state storyState) bool {
		return !needsRevision(state) || state.Revision >= maxRevisions
	}))
	g.AddEdge("revise", "review")

	g.SetEntryPoint("outline")
	g.SetFinishPoint("final")

	handler, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}

	initial := storyState{
		Draft: "A traveler arrives in a mysterious town. TODO: describe the climax.",
	}

	result, err := handler(context.Background(), initial)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Story finalized after %d revision(s).", result.Revision)
	log.Printf("Final draft: %q", result.Draft)
}
