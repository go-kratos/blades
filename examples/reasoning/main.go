package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
)

func main() {
	agent := blades.NewAgent(
		"Chat Agent",
		blades.WithModel("deepseek-r1"),
		blades.WithProvider(openai.NewChatProvider()),
		blades.WithInstructions("You are a helpful assistant that provides detailed and accurate information."),
	)
	prompt := blades.NewPrompt(
		blades.UserMessage("What is the capital of France?"),
	)
	stream, err := agent.RunStream(context.Background(), prompt, blades.ReasoningEffort("medium"))
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()
	for stream.Next() {
		res, err := stream.Current()
		if err != nil {
			log.Fatal(err)
		}
		log.Println(res.Text())
	}
}
