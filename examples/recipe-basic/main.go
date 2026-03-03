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

	// 2. Load recipe from YAML
	spec, err := recipe.LoadFromFile("recipe.yaml")
	if err != nil {
		log.Fatal(err)
	}

	// 3. Build agent with parameters
	agent, err := recipe.Build(spec,
		recipe.WithModelRegistry(registry),
		recipe.WithParams(map[string]any{"language": "go"}),
	)
	if err != nil {
		log.Fatal(err)
	}

	// 4. Run the agent
	runner := blades.NewRunner(agent)
	output, err := runner.Run(context.Background(), blades.UserMessage(`
		Review this code:
		func add(a, b int) int {
			return a - b
		}
	`))
	if err != nil {
		log.Fatal(err)
	}
	log.Println(output.Text())
}
