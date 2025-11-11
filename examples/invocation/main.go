package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
)

func main() {
	// Create a new agent with OpenAI provider
	agent, err := blades.NewAgent(
		"Invocation Agent",
		blades.WithModel("deepseek-chat"),
		blades.WithProvider(openai.NewChatProvider()),
		blades.WithInstructions("You are a helpful assistant that provides detailed and accurate information."),
	)
	if err != nil {
		log.Fatal(err)
	}
	// Create a user message input
	input := blades.UserMessage("What is the capital of France?")
	// Run the agent with the input message
	for output, err := range agent.Run(context.Background(), blades.NewInvocation(input)) {
		if err != nil {
			log.Fatal(err)
		}
		log.Println(output.Text())
	}
}
