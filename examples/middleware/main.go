package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
)

func main() {
	agent := blades.NewAgent(
		"History Tutor",
		blades.WithModel("deepseek-chat"),
		blades.WithInstructions("You are a helpful assistant that provides detailed and accurate information."),
		blades.WithProvider(openai.NewChatProvider()),
		blades.WithMiddleware(
			NewLogging,
			NewGuardrails,
		),
	)
	prompt := blades.NewPrompt(
		blades.UserMessage("What is the capital of France?"),
	)
	// Run example
	result, err := agent.Run(context.Background(), prompt)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("run:", result.Text())
	// RunStream example
	for message, err := range agent.RunStream(context.Background(), prompt) {
		if err != nil {
			log.Fatal(err)
		}
		log.Print("runStream:", message.Text())
	}
}
