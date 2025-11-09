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
		blades.WithModel("deepseek-chat"),
		blades.WithProvider(openai.NewChatProvider()),
		blades.WithInstructions("You are a helpful assistant that provides detailed answers."),
	)
	input := blades.UserMessage("What is the capital of France?")
	runner := blades.NewRunner(agent)
	stream := runner.RunStream(context.Background(), input)
	for m, err := range stream {
		if err != nil {
			log.Fatal(err)
		}
		log.Println(m.Status, m.Text())
	}
}
