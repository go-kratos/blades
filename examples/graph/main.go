package main

import (
	"context"
	"log"
	"strings"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/flow"
)

func main() {
	provider := openai.NewChatProvider()

	// Define agents for the graph nodes.
	storyOutline := blades.NewAgent(
		"story_outline_agent",
		blades.WithModel("gpt-5"),
		blades.WithProvider(provider),
		blades.WithInstructions("Generate a very short story outline based on the user's input."),
	)
	storyChecker := blades.NewAgent(
		"outline_checker_agent",
		blades.WithModel("gpt-5"),
		blades.WithProvider(provider),
		blades.WithInstructions("Read the given outline, judge the quality, and state if it is a scifi story using the word 'scifi' if applicable."),
	)
	scifiWriter := blades.NewAgent(
		"scifi_writer_agent",
		blades.WithModel("gpt-5"),
		blades.WithProvider(provider),
		blades.WithInstructions("Write a short scifi story based on the given outline."),
	)
	generalWriter := blades.NewAgent(
		"general_writer_agent",
		blades.WithModel("gpt-5"),
		blades.WithProvider(provider),
		blades.WithInstructions("Write a short non-scifi story based on the given outline."),
	)
	refineAgent := blades.NewAgent(
		"refine_agent",
		blades.WithModel("gpt-5"),
		blades.WithProvider(provider),
		blades.WithInstructions("Refine the story to improve clarity and flow."),
	)

	// Branch condition: choose scifiWriter if recent assistant output mentions "scifi".
	branchCond := func(ctx context.Context) (string, error) {
		state, ok := flow.FromGraphContext(ctx)
		if !ok {
			return "", flow.ErrNoFlowState
		}
		text := strings.ToLower(state.Prompt.String())
		if strings.Contains(text, "scifi") || strings.Contains(text, "sci-fi") {
			return "scifi", nil // choose scifiWriter
		}
		return "general", nil // choose generalWriter
	}

	// Build graph: outline -> checker -> branch (scifi/general) -> refine -> end
	g := flow.NewGraph()
	g.AddNode(storyOutline)
	g.AddNode(storyChecker)
	g.AddNode(scifiWriter)
	g.AddNode(generalWriter)
	g.AddNode(refineAgent)
	// Add edges and branches
	g.AddEdgeStart(storyOutline)
	g.AddEdge(storyOutline, storyChecker)
	g.AddBranch(storyChecker, branchCond, map[string]blades.Runner{
		"scifi":   scifiWriter,
		"general": generalWriter,
	})
	g.AddEdge(scifiWriter, refineAgent)
	g.AddEdge(generalWriter, refineAgent)
	g.AddEdgeEnd(refineAgent)
	// Compile the graph into a single runner
	runner, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}
	// Run the graph with an initial prompt
	prompt := blades.NewPrompt(
		blades.UserMessage("A brave knight embarks on a quest to find a hidden treasure."),
	)
	result, err := runner.Run(context.Background(), prompt)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(result.Text())
}
