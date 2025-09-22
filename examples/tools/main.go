package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/google/jsonschema-go/jsonschema"
)

func main() {
	tools := []*blades.Tool{
		&blades.Tool{
			Name:        "get_weather",
			Description: "Get the current weather for a given city",
			InputSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"location": {Type: "string"},
				},
				Required: []string{"location"},
			},
			Handle: func(ctx context.Context, input string) (string, error) {
				log.Println("Fetching weather for:", input)
				return "Sunny, 25°C", nil
			},
		},
	}
	agent := blades.NewAgent(
		"Weather Agent",
		blades.WithModel("gpt-5"),
		blades.WithInstructions("You are a helpful assistant that provides weather information."),
		blades.WithProvider(openai.NewProvider()),
		blades.WithTools(tools...),
	)
	prompt := blades.NewPrompt(
		blades.UserMessage("What is the weather in New York City?"),
	)
	result, err := agent.Run(context.Background(), prompt)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(result.AsText())
}
