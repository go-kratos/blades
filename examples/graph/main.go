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

func main() {
	fmt.Println("== Loop example ==")
	runLoopExample()

	fmt.Println("\n== Parallel example ==")
	runParallelExample()
}

// --- Loop example ---------------------------------------------------------

const (
	stateKeyRevision = "revision"
	stateKeyDraft    = "draft"
)

func runLoopExample() {
	const maxRevisions = 3

	g := graph.NewGraph(graph.WithParallel(false))
	g.AddNode("outline", func(ctx context.Context, state graph.State) (graph.State, error) {
		next := state.Clone()
		if _, ok := next[stateKeyDraft]; !ok {
			next[stateKeyDraft] = "Outline TODO: add twist."
		}
		return next, nil
	})
	g.AddNode("review", func(ctx context.Context, state graph.State) (graph.State, error) {
		return state.Clone(), nil
	})
	g.AddNode("revise", func(ctx context.Context, state graph.State) (graph.State, error) {
		next := state.Clone()
		revision, _ := next[stateKeyRevision].(int)
		draft, _ := next[stateKeyDraft].(string)

		revision++
		draft = strings.Replace(draft, "TODO: add twist.", "A surprise reveal changes everything.", 1)
		switch revision {
		case 1:
			draft += " TODO: refine ending."
		case 2:
			draft = strings.Replace(draft, " TODO: refine ending.", " An epilogue wraps the journey.", 1)
		}

		next[stateKeyRevision] = revision
		next[stateKeyDraft] = draft
		return next, nil
	})
	g.AddNode("publish", func(ctx context.Context, state graph.State) (graph.State, error) {
		revision, _ := state[stateKeyRevision].(int)
		draft, _ := state[stateKeyDraft].(string)
		fmt.Printf("Final draft after %d revision(s): %s\n", revision, draft)
		return state.Clone(), nil
	})

	g.AddEdge("outline", "review")
	g.AddEdge("review", "revise", graph.WithEdgeCondition(func(_ context.Context, state graph.State) bool {
		draft, _ := state[stateKeyDraft].(string)
		revision, _ := state[stateKeyRevision].(int)
		return strings.Contains(draft, "TODO") && revision < maxRevisions
	}))
	g.AddEdge("review", "publish", graph.WithEdgeCondition(func(_ context.Context, state graph.State) bool {
		draft, _ := state[stateKeyDraft].(string)
		revision, _ := state[stateKeyRevision].(int)
		return !strings.Contains(draft, "TODO") || revision >= maxRevisions
	}))
	g.AddEdge("revise", "review")

	g.SetEntryPoint("outline")
	g.SetFinishPoint("publish")

	executor, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}

	_, err = executor.Execute(context.Background(), graph.State{})
	if err != nil {
		log.Fatal(err)
	}
}

// --- Parallel example -----------------------------------------------------

const (
	stateKeyNodeA    = "node_a_steps"
	stateKeyNodeB    = "node_b_steps"
	stateKeyNodeJoin = "node_join_steps"
)

func runParallelExample() {
	g := graph.NewGraph()

	// simple helper to log execution order
	logNode := func(name string) graph.GraphHandler {
		return func(ctx context.Context, state graph.State) (graph.State, error) {
			next := state.Clone()
			fmt.Printf("node %s start executing\n	", name)
			if strings.HasPrefix(name, "branch_") {
				t := time.Millisecond * time.Duration(rand.Int63n(250))
				time.Sleep(t)
				fmt.Printf("node %s executed, sleep %d ms\n	", name, t.Milliseconds())
			}
			switch name {
			case "branch_a":
				next[stateKeyNodeA] = appendString(next[stateKeyNodeA], name)
			case "branch_b":
				next[stateKeyNodeB] = appendString(next[stateKeyNodeB], name)
			case "join":
				var joined []string
				joined = append(joined, getStringSlice(next[stateKeyNodeA])...)
				joined = append(joined, getStringSlice(next[stateKeyNodeB])...)
				next[stateKeyNodeJoin] = joined
			}

			return next, nil
		}
	}

	g.AddNode("start", logNode("start"))
	g.AddNode("branch_a", logNode("branch_a"))
	g.AddNode("branch_b", logNode("branch_b"))
	g.AddNode("branch_c", logNode("branch_c"))
	g.AddNode("branch_d", logNode("branch_d"))

	g.AddNode("join", logNode("join"))

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

	final, err := executor.Execute(context.Background(), graph.State{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("parallel example result: %v\n", getStringSlice(final[stateKeyNodeJoin]))
}

func appendString(value any, item string) []string {
	slice := getStringSlice(value)
	return append(slice, item)
}

func getStringSlice(value any) []string {
	if slice, ok := value.([]string); ok {
		return slice
	}
	return []string{}
}
