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
	// Define branching logic based on the outline checker output
	branchChoose := func(ctx context.Context, prompt *blades.Prompt) (string, error) {
		text := strings.ToLower(prompt.String())
		if strings.Contains(text, "scifi") || strings.Contains(text, "sci-fi") {
			return "scifi", nil // choose scifiWriter
		}
		return "general", nil // choose generalWriter
	}
	branchWriter := flow.NewBranch("branch", branchChoose, scifiWriter, generalWriter)
	// Build graph: outline -> checker -> branch (scifi/general) -> refine -> end
	g := flow.NewGraph("story")
	g.AddNode(storyOutline)
	g.AddNode(storyChecker)
	g.AddNode(branchWriter)
	g.AddNode(refineAgent)
	// Add edges and branches
	g.AddStart(storyOutline)
	g.AddEdge(storyOutline, storyChecker)
	g.AddEdge(storyChecker, branchWriter)
	g.AddEdge(branchWriter, refineAgent)
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
