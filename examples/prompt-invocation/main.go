package main

import (
	"context"
	"log"
	"os"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
)

func main() {
	model := openai.NewModel(os.Getenv("OPENAI_MODEL"), openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	})
	// Create a new agent with OpenAI provider
	agent, err := blades.NewAgent(
		"Invocation Agent",
		blades.WithModel(model),
		blades.WithInstruction("You are a helpful assistant that provides detailed and accurate information."),
	)
	if err != nil {
		log.Fatal(err)
	}
	// Create a user message input
	input := blades.UserMessage("What is the capital of France?")
	// Run the agent with the input message
	for output, err := range agent.Run(context.Background(), &blades.Invocation{Message: input}) {
		if err != nil {
			log.Fatal(err)
		}
		log.Println(output.Text())
	}
}
