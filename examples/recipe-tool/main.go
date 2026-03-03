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
	registry := recipe.NewRegistry()
	registry.Register("gpt-4o", openai.NewModel("gpt-4o", openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	}))

	// 2. Load recipe
	spec, err := recipe.LoadFromFile("recipe.yaml")
	if err != nil {
		log.Fatal(err)
	}

	// 3. Build tool-mode agent (sub-recipes become callable tools)
	agent, err := recipe.Build(spec,
		recipe.WithModelRegistry(registry),
		recipe.WithParams(map[string]any{"topic": "climate change impact on agriculture"}),
	)
	if err != nil {
		log.Fatal(err)
	}

	// 4. Run with streaming, skip empty chunks
	runner := blades.NewRunner(agent)
	stream := runner.RunStream(context.Background(), blades.UserMessage(
		"Research the topic and provide a summary with verified facts and data analysis.",
	))
	for message, err := range stream {
		if err != nil {
			log.Fatal(err)
		}
		if text := message.Text(); text != "" {
			log.Printf("[%s] %s\n", message.Author, text)
		}
	}
}
