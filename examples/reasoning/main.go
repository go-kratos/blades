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
	stream := agent.RunStream(context.Background(), prompt)
	for m, err := range stream {
		if err != nil {
			log.Fatal(err)
		}
		log.Println(m.Text())
	}
}
