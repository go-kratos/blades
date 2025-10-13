package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/flow"
)

func main() {
	provider := openai.NewChatProvider()
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
		blades.WithInstructions("Read the given story outline, and judge the quality. Also, determine if it is a scifi story."),
	)
	storyAgent := blades.NewAgent(
		"story_agent",
		blades.WithModel("gpt-5"),
		blades.WithProvider(provider),
		blades.WithInstructions("Write a short story based on the given outline."),
	)
	transitionHandler := func(ctx context.Context, transition flow.Transition, output *blades.Message) (*blades.Prompt, error) {
		return blades.NewPrompt(output), nil
	}
	seq := flow.NewSequential("story", transitionHandler, storyOutline, storyChecker, storyAgent)
	// Input prompt
	prompt := blades.NewPrompt(
		blades.UserMessage("A brave knight embarks on a quest to find a hidden treasure."),
	)
	result, err := seq.Run(context.Background(), prompt)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(result.Text())
}
