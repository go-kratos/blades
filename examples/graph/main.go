package main

import (
	"context"
	"log"
	"strings"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/flow"
)

func wrapHandle(runner blades.Runnable) flow.GraphHandler[string] {
	return func(ctx context.Context, state string) (string, error) {
		output, err := runner.Run(ctx, blades.NewPrompt(blades.UserMessage(state)))
		if err != nil {
			return "", err
		}
		return output.Text(), nil
	}
}

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
	// Define branching logic based on the outline checker output
	branchChoose := func(ctx context.Context, prompt *blades.Prompt) (string, error) {
		text := strings.ToLower(prompt.String())
		if strings.Contains(text, "scifi") || strings.Contains(text, "sci-fi") {
			return "scifi", nil // choose scifiWriter
		}
		return "general", nil // choose generalWriter
	}
	branchWriter := flow.NewBranch(branchChoose, map[string]blades.Runnable{
		"scifi":   scifiWriter,
		"general": generalWriter,
	})
	// Build graph: outline -> checker -> branch (scifi/general) -> refine -> end
	g := flow.NewGraph[string]()
	g.AddNode("outline", wrapHandle(storyOutline))
	g.AddNode("checker", wrapHandle(storyChecker))
	g.AddNode("branch", wrapHandle(branchWriter))
	g.AddNode("refine", wrapHandle(refineAgent))
	// Add edges and branches
	g.AddEdge("outline", "checker")
	g.AddEdge("checker", "branch")
	g.AddEdge("branch", "refine")
	g.SetEntryPoint("outline")
	g.SetFinishPoint("refine")
	// Compile the graph into a single runner
	handler, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}
	// Run the graph with an initial input
	result, err := handler(context.Background(), "A brave knight embarks on a quest to find a hidden treasure.")
	if err != nil {
		log.Fatal(err)
	}
	log.Println(result)
}
