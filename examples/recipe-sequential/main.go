package main

import (
	"context"
	"log"
	"os"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/recipe"
)

func main() {
	// 1. Register models
	registry := recipe.NewModelRegistry()
	registry.Register("gpt-4o", openai.NewModel("gpt-4o", openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	}))

	// 2. Load recipe
	spec, err := recipe.LoadFromFile("agent.yaml")
	if err != nil {
		log.Fatal(err)
	}

	// 3. Build sequential pipeline
	agent, err := recipe.Build(spec,
		recipe.WithModelRegistry(registry),
		recipe.WithParams(map[string]any{"language": "go"}),
	)
	if err != nil {
		log.Fatal(err)
	}

	// 4. Run with streaming to see each step
	runner := blades.NewRunner(agent)
	stream := runner.RunStream(context.Background(), blades.UserMessage(`
		Review this code:
		func divide(a, b int) int {
			return a / b
		}
	`))
	for message, err := range stream {
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("[%s] %s\n", message.Author, message.Text())
	}
}
