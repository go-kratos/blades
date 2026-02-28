package main

import (
	"context"
	"log"
	"os"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/anthropic"
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
	baseURL := os.Getenv("ANTHROPIC_BASE_URL")
	authToken := os.Getenv("ANTHROPIC_AUTH_TOKEN")
	modelName := os.Getenv("ANTHROPIC_DEFAULT_HAIKU_MODEL")

	weatherTool, err := tools.NewFunc(
		"get_weather",
		"Get the current weather for a given city",
		weatherHandle,
	)
	if err != nil {
		log.Fatal(err)
	}

	model := anthropic.NewModel(modelName, anthropic.Config{
		BaseURL:         baseURL,
		APIKey:          authToken,
		MaxOutputTokens: 1024,
		Temperature:     0.7,
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
