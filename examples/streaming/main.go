package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
)

func main() {
	agent := blades.NewAgent(
		"Stream Agent",
		blades.WithModel("gpt-5"),
		blades.WithProvider(openai.NewChatProvider()),
		blades.WithInstructions("You are a helpful assistant that provides detailed answers."),
	)
	prompt := blades.NewPrompt(
		blades.UserMessage("What is the capital of France?"),
	)
	stream, err := agent.RunStream(context.Background(), prompt)
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()
	for stream.Next() {
		chunk, err := stream.Current()
		if err != nil {
			log.Fatalf("stream recv error: %v", err)
		}
		log.Print(chunk.Text())
	}
}
