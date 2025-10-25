package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/go-kratos/blades/flow"
)

func main() {
	fmt.Println("== Loop example ==")
	runLoopExample()

	fmt.Println("\n== Parallel example ==")
	runParallelExample()
}

// --- Loop example ---------------------------------------------------------

type loopState struct {
	Revision int
	Draft    string
}

func runLoopExample() {
	const maxRevisions = 3

	g := flow.NewGraph[loopState](flow.WithParallel[loopState](false))
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
		switch state.Revision {
		case 1:
			state.Draft += " TODO: refine ending."
		case 2:
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
	g.AddEdge("revise", "review")

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

// --- Parallel example -----------------------------------------------------

type parallelState struct {
	NodeASteps    []string
	NodeBSteps    []string
	NodeJoinSteps []string
}

func runParallelExample() {
	g := flow.NewGraph[*parallelState]()

	// simple helper to log execution order
	logNode := func(name string) flow.GraphHandler[*parallelState] {

		return func(ctx context.Context, state *parallelState) (*parallelState, error) {
			fmt.Printf("node %s start executing\n	", name)
			if strings.HasPrefix(name, "branch_") {
				t := time.Millisecond * time.Duration(rand.Int63n(250))
				time.Sleep(t)
				fmt.Printf("node %s executed, sleep %d ms\n	", name, t.Milliseconds())

			}
			switch name {
			case "branch_a":
				state.NodeASteps = append(state.NodeASteps, name)
			case "branch_b":
				state.NodeBSteps = append(state.NodeBSteps, name)
			case "join":
				state.NodeJoinSteps = append(state.NodeJoinSteps, state.NodeASteps...)
				state.NodeJoinSteps = append(state.NodeJoinSteps, state.NodeBSteps...)
			}

			return state, nil
		}
	}

	g.AddNode("start", logNode("start"))
	g.AddNode("branch_a", logNode("branch_a"))
	g.AddNode("branch_b", logNode("branch_b"))
	g.AddNode("join", logNode("join"))

	g.AddEdge("start", "branch_a")
	g.AddEdge("start", "branch_b")
	g.AddEdge("branch_a", "join")
	g.AddEdge("branch_b", "join")

	g.SetEntryPoint("start")
	g.SetFinishPoint("join")

	handler, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}

	final, err := handler(context.Background(), &parallelState{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("parallel example result: %v\n", final.NodeJoinSteps)
}
