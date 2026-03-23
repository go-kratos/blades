package main

import (
	"context"
	"log"
	"os"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/middleware"
	"github.com/go-kratos/blades/recipe"
)

func main() {
	// 1. Register models
	registry := recipe.NewModelRegistry()
	registry.Register("gpt-4o", openai.NewModel("gpt-4o", openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	}))

	// 2. Register middlewares
	// The "retry" middleware is declared in agent.yaml and resolved here at build time.
	mwRegistry := recipe.NewMiddlewareRegistry()
	mwRegistry.Register("retry", func(opts map[string]any) (blades.Middleware, error) {
		attempts := 3
		if v, ok := opts["attempts"].(int); ok && v > 0 {
			attempts = v
		}
		return middleware.Retry(attempts), nil
	})

	// 3. Load recipe from YAML
	spec, err := recipe.LoadFromFile("agent.yaml")
	if err != nil {
		log.Fatal(err)
	}

	// 4. Build agent — the YAML "context: window" and "middlewares: retry" are
	//    applied automatically; no manual wiring needed.
	agent, err := recipe.Build(spec,
		recipe.WithModelRegistry(registry),
		recipe.WithMiddlewareRegistry(mwRegistry),
		recipe.WithParams(map[string]any{"language": "go"}),
	)
	if err != nil {
		log.Fatal(err)
	}

	// 5. Run the agent
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
