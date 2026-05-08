package main

import (
	"context"
	"fmt"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/ollama"
)

func main() {
	// Configure Ollama provider
	model := ollama.NewModel("llama3.2", ollama.Config{
		BaseURL:     "http://localhost:11434", // Default Ollama URL
		Temperature: 0.7,
		TopP:        0.9,
	})

	// Create agent with Ollama model
	agent, err := blades.NewAgent(
		"Ollama Agent",
		blades.WithModel(model),
		blades.WithInstructions("You are a helpful assistant that provides concise and accurate information."),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Create input message
	input := blades.UserMessage("What is the capital of France?")

	// Run the agent
	runner := blades.NewRunner(agent)
	output, err := runner.Run(context.Background(), input)
	if err != nil {
		log.Fatal(err)
	}

	// Print response
	fmt.Println("Response:", output.Text())

	// Example of streaming response
	fmt.Println("\n=== Streaming Example ===")
	streamInput := blades.UserMessage("Tell me a short story about a robot who discovers music.")

	streamCtx := context.Background()
	stream := runner.RunStream(streamCtx, streamInput)

	fmt.Print("Assistant: ")
	for msg, err := range stream {
		if err != nil {
			log.Printf("Streaming error: %v", err)
			break
		}
		fmt.Print(msg.Text())
	}
	fmt.Println()
}