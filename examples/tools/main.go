package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/google/jsonschema-go/jsonschema"
)

func main() {
	weatherHandler := blades.NewFuncTool(
		"get_weather",
		"Get the current weather for a given city",
		&jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"location": {Type: "string"},
			},
			Required: []string{"location"},
		},
		func(ctx context.Context, input string) (string, error) {
			log.Println("Fetching weather for:", input)
			return "Sunny, 25°C", nil
		},
	)
	tools := []*blades.Tool{
		weatherHandler,
	}
	agent := blades.NewAgent(
		"Weather Agent",
		blades.WithModel("qwen-plus"),
		blades.WithInstructions("You are a helpful assistant that provides weather information."),
		blades.WithProvider(openai.NewChatProvider()),
		blades.WithTools(tools...),
	)
	prompt := blades.NewPrompt(
		blades.UserMessage("What is the weather in New York City?"),
	)
	// Run the agent with the prompt
	result, err := agent.Run(context.Background(), prompt)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(result.Text())
	// Run the agent in streaming mode
	stream, err := agent.RunStream(context.Background(), prompt)
	if err != nil {
		log.Fatal(err)
	}
	for stream.Next() {
		res, err := stream.Current()
		if err != nil {
			log.Fatal(err)
		}
		log.Print(res.Text())
	}
}
