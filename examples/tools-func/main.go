package main

import (
	"context"
	"log"
	"os"

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

// weatherHandle is the function that handles weather requests.
func weatherHandle(ctx context.Context, req WeatherReq) (WeatherRes, error) {
	log.Println("Fetching weather for:", req.Location)
	session, ok := blades.FromSessionContext(ctx)
	if !ok {
		return WeatherRes{}, blades.ErrNoSessionContext
	}
	session.SetState("location", req.Location)
	return WeatherRes{Forecast: "Sunny, 25°C"}, nil
}

func main() {
	// Define a tool to get the weather
	weatherTool, err := tools.NewFunc(
		"get_weather",
		"Get the current weather for a given city",
		weatherHandle,
	)
	if err != nil {
		log.Fatal(err)
	}
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
	ctx := context.Background()
	session := blades.NewSession()
	runner := blades.NewRunner(agent)
	output, err := runner.Run(ctx, input, blades.WithSession(session))
	if err != nil {
		log.Fatal(err)
	}
	log.Println("state:", session.State())
	log.Println("output:", output.Text())
}
