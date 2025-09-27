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
	branchCond := func(ctx context.Context) (bool, error) {
		state, ok := flow.FromGraphContext(ctx)
		if !ok {
			return false, flow.ErrNoGraphState
		}
		msg := state.History[len(state.History)-1]
		if msg.Role == blades.RoleAssistant {
			text := strings.ToLower(msg.AsText())
			if strings.Contains(text, "scifi") || strings.Contains(text, "sci-fi") {
				return true, nil // choose scifiWriter
			}
		}
		return false, nil // choose generalWriter
	}

	// Loop condition: run refineAgent up to 2 times.
	loopCond := func(ctx context.Context) (bool, error) {
		// In this example, we always return true to allow up to max iterations.
		return true, nil
	}

	// Build graph: outline -> checker -> branch (scifi/general) -> loop refine -> end
	a := flow.NewNode(storyOutline)
	b := flow.NewNode(storyChecker)
	c := flow.NewBranchNode(branchCond, scifiWriter, generalWriter)
	d := flow.NewLoopNode(loopCond, refineAgent, flow.WithMaxIterations(2))

	// Define edges
	a.To(b).To(c).To(d)

	prompt := blades.NewPrompt(
		blades.UserMessage("A brave knight embarks on a quest to find a hidden treasure."),
	)

	g := flow.NewGraph(a)
	result, err := g.Run(context.Background(), prompt)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(result.AsText())
}
