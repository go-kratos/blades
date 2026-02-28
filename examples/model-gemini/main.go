package main

import (
	"context"
	"log"
	"os"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/gemini"
	"github.com/go-kratos/blades/tools"
	"google.golang.org/genai"
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
	apiKey := os.Getenv("GOOGLE_API_KEY")
	modelName := os.Getenv("GEMINI_MODEL")

	weatherTool, err := tools.NewFunc(
		"get_weather",
		"Get the current weather for a given city",
		weatherHandle,
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	model, err := gemini.NewModel(ctx, modelName, gemini.Config{
		ClientConfig: genai.ClientConfig{
			APIKey:  apiKey,
			Backend: genai.BackendGeminiAPI,
		},
		MaxOutputTokens: 1024,
		Temperature:     0.7,
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

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
	session := blades.NewSession()
	runner := blades.NewRunner(agent)
	output, err := runner.Run(ctx, input, blades.WithSession(session))
	if err != nil {
		log.Fatal(err)
	}

	log.Println("state:", session.State())
	log.Println("output:", output.Text())
}
