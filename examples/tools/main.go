package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/tools"
)

// WeatherReq represents a request for weather information.
type WeatherReq struct {
	Location string `json:"location" jsonschema:"Get the current weather for a given city"`
}

// WeatherRes represents a response containing weather information.
type WeatherRes struct {
	Forecast string `json:"forecast" jsonschema:"The weather forecast"`
}

func main() {
	weatherTool, err := tools.NewTool[WeatherReq, WeatherRes](
		"get_weather",
		"Get the current weather for a given city",
		tools.HandleFunc[WeatherReq, WeatherRes](func(ctx context.Context, req WeatherReq) (WeatherRes, error) {
			log.Println("Fetching weather for:", req.Location)
			return WeatherRes{Forecast: "Sunny, 25°C"}, nil
		}),
	)
	agent := blades.NewAgent(
		"Weather Agent",
		blades.WithModel("gpt-5"),
		blades.WithInstructions("You are a helpful assistant that provides weather information."),
		blades.WithProvider(openai.NewChatProvider()),
		blades.WithTools(weatherTool),
	)
	// Create a prompt asking for the weather in New York City
	input := blades.UserMessage("What is the weather in New York City?")
	// Run the agent with the prompt
	runner := blades.NewRunner(agent)
	output, err := runner.Run(context.Background(), input)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(output.Text())
}
