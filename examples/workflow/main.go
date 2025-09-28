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
		state, ok := flow.FromContext(ctx)
		if !ok {
			return "", flow.ErrNoFlowState
		}
		text := strings.ToLower(state.Prompt.String())
		if strings.Contains(text, "scifi") || strings.Contains(text, "sci-fi") {
			return "scifi", nil // choose scifiWriter
		}
		return "general", nil // choose generalWriter
	}

	// Loop condition: run refineAgent up to 2 times.
	loopCond := func(ctx context.Context) (bool, error) {
		// In this example, we always return true to allow up to max iterations.
		return true, nil
	}

	// Build graph: outline -> checker -> branch (scifi/general) -> loop refine -> end
	a := flow.NewNode(storyOutline)
	b := flow.NewNode(storyChecker)
	c := flow.NewNode(scifiWriter)
	d := flow.NewNode(generalWriter)
	e := flow.NewLoop(loopCond, refineAgent, flow.LoopMaxIterations(2))
	branch := flow.NewBranch(branchCond)
	branch.Add("scifi", c)
	branch.Add("general", d)

	// Define edges
	a.To(b)
	b.To(branch) // -> branch to choose between c and d
	c.To(e)
	d.To(e)

	prompt := blades.NewPrompt(
		blades.UserMessage("A brave knight embarks on a quest to find a hidden treasure."),
	)

	g := flow.New(a)
	result, err := g.Run(context.Background(), prompt)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(result.AsText())
}
