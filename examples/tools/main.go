package main

import (
	"context"
	"encoding/json"
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
		tools.HandleFunc(func(ctx context.Context, input string) (string, error) {
			var req WeatherReq
			if err := json.Unmarshal([]byte(input), &req); err != nil {
				return "", err
			}
			b, err := json.Marshal(&WeatherRes{Forecast: "Sunny, 25°C"})
			if err != nil {
				return "", err
			}
			log.Println("Fetching weather for:", req.Location, "->", string(b))
			return string(b), nil
		}),
	)
	agent := blades.NewAgent(
		"Weather Agent",
		blades.WithModel("qwen-plus"),
		blades.WithInstructions("You are a helpful assistant that provides weather information."),
		blades.WithProvider(openai.NewChatProvider()),
		blades.WithTools(weatherTool),
	)
	// Create a prompt asking for the weather in New York City
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
