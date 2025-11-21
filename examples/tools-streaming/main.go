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

// timeHandle provides a mock current time response.
func timeHandle(ctx context.Context, args string) (string, error) {
	log.Println("Time tool called with args:", args)
	return "The current time is 3:04 PM", nil
}

// weatherHandle provides a mock weather forecast for a given location.
func weatherHandle(ctx context.Context, args string) (string, error) {
	log.Println("Weather tool called with args:", args)
	return "Sunny, 25°C", nil
}

func main() {
	timeTool := tools.NewTool(
		"get_current_time",
		"Get the current time",
		tools.HandleFunc(timeHandle),
		tools.WithInputSchema(&jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"timezone": {
					Type:        "string",
					Description: "The timezone to get the current time for",
				},
			},
		}),
	)
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
				"current_time": {
					Type:        "string",
					Description: "The current time in the location",
				},
			},
		}),
	)
	// Create an agent with the weather tool
	model := openai.NewModel(os.Getenv("OPENAI_MODEL"), openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	})
	agent, err := blades.NewAgent(
		"Weather Agent",
		blades.WithModel(model),
		blades.WithInstruction("You are a helpful assistant that provides weather information."),
		blades.WithTools(timeTool, weatherTool),
	)
	if err != nil {
		log.Fatal(err)
	}
	// Create a prompt asking for the weather in New York City
	input := blades.UserMessage("What is the weather in New York City?")
	runner := blades.NewRunner(agent)
	for output, err := range runner.RunStream(context.Background(), input) {
		if err != nil {
			log.Fatal(err)
		}
		log.Println(output.Role, output.Status, output.String())
	}
}
