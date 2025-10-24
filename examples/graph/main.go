package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/go-kratos/blades/flow"
)

func main() {
	fmt.Println("== Loop example ==")
	runLoopExample()

	fmt.Println("\n== Activation group example ==")
	runActivationGroupExample()
}

// --- Loop example ---------------------------------------------------------

type loopState struct {
	Revision int
	Draft    string
}

func runLoopExample() {
	const maxRevisions = 3

	g := flow.NewGraph[loopState]()
	g.AddNode("outline", func(ctx context.Context, state loopState) (loopState, error) {
		if state.Draft == "" {
			state.Draft = "Outline TODO: add twist."
		}
		return state, nil
	})
	g.AddNode("review", func(ctx context.Context, state loopState) (loopState, error) {
		return state, nil
	})
	g.AddNode("revise", func(ctx context.Context, state loopState) (loopState, error) {
		state.Revision++
		state.Draft = strings.Replace(state.Draft, "TODO: add twist.", "A surprise reveal changes everything.", 1)
		if state.Revision == 1 {
			state.Draft += " TODO: refine ending."
		} else if state.Revision == 2 {
			state.Draft = strings.Replace(state.Draft, " TODO: refine ending.", " An epilogue wraps the journey.", 1)
		}
		return state, nil
	})
	g.AddNode("publish", func(ctx context.Context, state loopState) (loopState, error) {
		fmt.Printf("Final draft after %d revision(s): %s\n", state.Revision, state.Draft)
		return state, nil
	})

	g.AddEdge("outline", "review")
	g.AddEdge("review", "revise", flow.WithEdgeCondition(func(_ context.Context, state loopState) bool {
		return strings.Contains(state.Draft, "TODO") && state.Revision < maxRevisions
	}))
	g.AddEdge("review", "publish", flow.WithEdgeCondition(func(_ context.Context, state loopState) bool {
		return !strings.Contains(state.Draft, "TODO") || state.Revision >= maxRevisions
	}))
	g.AddEdge("revise", "review", flow.WithActivationGroup[loopState]("feedback", flow.ActivationAll))

	g.SetEntryPoint("outline")
	g.SetFinishPoint("publish")

	handler, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}

	_, err = handler(context.Background(), loopState{})
	if err != nil {
		log.Fatal(err)
	}
}

// --- Activation group example --------------------------------------------

type groupState struct {
	AllowFast bool
	Steps     []string
}

func step(name string, mutate func(*groupState)) flow.GraphHandler[groupState] {
	return func(ctx context.Context, state groupState) (groupState, error) {
		if mutate != nil {
			mutate(&state)
		}
		state.Steps = append(state.Steps, name)
		fmt.Printf(" -> %s (steps: %v)\n", name, state.Steps)
		return state, nil
	}
}

func runActivationGroupExample() {
	g := flow.NewGraph[groupState]()

	g.AddNode("source", step("source", nil))
	g.AddNode("analysis_a", step("analysis_a", nil))
	g.AddNode("analysis_b", step("analysis_b", nil))
	g.AddNode("fast_check", step("fast_check", func(state *groupState) {
		state.AllowFast = true
	}))
	g.AddNode("merge", step("merge", nil))
	g.AddNode("sink", step("sink", nil))

	// Source fans out.
	g.AddEdge("source", "analysis_a")
	g.AddEdge("source", "analysis_b")
	g.AddEdge("source", "fast_check")

	// Critical path: default activation waits for ALL incoming edges.
	g.AddEdge("analysis_a", "merge")
	g.AddEdge("analysis_b", "merge")

	// Decision group: any member can trigger sink. Merge represents the fully-gathered result.
	g.AddEdge("merge", "sink",
		flow.WithActivationGroup[groupState]("decision", flow.ActivationAny),
	)

	// Fast path: ANY result in the fast group may trigger the sink early.
	g.AddEdge("fast_check", "sink",
		flow.WithEdgeCondition(func(_ context.Context, state groupState) bool {
			return state.AllowFast
		}),
		flow.WithActivationGroup[groupState]("decision", flow.ActivationAny),
	)

	g.SetEntryPoint("source")
	g.SetFinishPoint("sink")

	handler, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}

	_, err = handler(context.Background(), groupState{})
	if err != nil {
		log.Fatal(err)
	}
}
