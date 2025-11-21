package main

import (
	"context"
	"log"
	"os"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)

// weatherHandle provides a mock weather forecast for a given location.
func weatherHandle(ctx context.Context, args string) (string, error) {
	return args + ": Sunny, 25°C", nil
}

// toolLogging is a middleware that logs incoming requests to the tool.
func toolLogging() tools.Middleware {
	return func(next tools.Handler) tools.Handler {
		return tools.HandleFunc(func(ctx context.Context, req string) (string, error) {
			log.Println("Request received:", req)
			return next.Handle(ctx, req)
		})
	}
}

func main() {
	// Define a tool to get the weather
	weatherTool := tools.NewTool(
		"get_weather",
		"Get the current weather for a given city",
		tools.HandleFunc(weatherHandle),
		tools.WithInputSchema(&jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"country": {
					Type:        "string",
					Description: "The country",
				},
			},
		}),
		tools.WithMiddleware(toolLogging()),
	)
	// Create an agent with the weather tool
	model := openai.NewModel(os.Getenv("OPENAI_MODEL"), openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	})
	agent, err := blades.NewAgent(
		"Weather Agent",
		blades.WithModel(model),
		blades.WithInstruction("You are a helpful assistant that provides weather information."),
		blades.WithTools(weatherTool),
	)
	if err != nil {
		log.Fatal(err)
	}
	// Create a prompt asking for the weather in New York City
	input := blades.UserMessage("What is the weather in New York City?")
	runner := blades.NewRunner(agent)
	output, err := runner.Run(context.Background(), input)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("output:", output.Text())
}
