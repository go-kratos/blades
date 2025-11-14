package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
)

func main() {
	model := openai.NewModel("deepseek-chat")
	agent, err := blades.NewAgent(
		"History Tutor",
		blades.WithModel(model),
		blades.WithInstructions("You are a helpful assistant that provides detailed and accurate information."),
		blades.WithMiddleware(
			NewLogging,
			NewGuardrails,
		),
	)
	if err != nil {
		log.Fatal(err)
	}
	input := blades.UserMessage("What is the capital of France?")
	runner := blades.NewRunner(agent)
	output, err := runner.Run(context.Background(), input)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("runner:", output.Text())
}
