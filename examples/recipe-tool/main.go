package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/recipe"
	"github.com/go-kratos/blades/tools"
)

// --- Function Tool 1: extract-emails (regex-based) ---

type ExtractEmailsReq struct {
	Text string `json:"text" jsonschema:"The text to extract email addresses from"`
}

type ExtractEmailsRes struct {
	Matches []string `json:"matches" jsonschema:"The extracted email addresses"`
}

var emailPattern = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)

func extractEmails(_ context.Context, req ExtractEmailsReq) (ExtractEmailsRes, error) {
	matches := emailPattern.FindAllString(req.Text, -1)
	if matches == nil {
		matches = []string{}
	}
	return ExtractEmailsRes{Matches: matches}, nil
}

// --- Function Tool 2: get-weather (simple lookup) ---

type GetWeatherReq struct {
	City string `json:"city" jsonschema:"The city name to get weather for"`
}

type GetWeatherRes struct {
	City     string `json:"city" jsonschema:"The city name"`
	Forecast string `json:"forecast" jsonschema:"The weather forecast"`
}

func getWeather(_ context.Context, req GetWeatherReq) (GetWeatherRes, error) {
	// Simulated weather data
	forecasts := map[string]string{
		"new york":  "Partly cloudy, 18°C",
		"london":    "Rainy, 12°C",
		"tokyo":     "Sunny, 22°C",
		"beijing":   "Hazy, 15°C",
		"singapore": "Thunderstorms, 30°C",
	}
	forecast, ok := forecasts[strings.ToLower(req.City)]
	if !ok {
		forecast = fmt.Sprintf("No data available for %s", req.City)
	}
	return GetWeatherRes{City: req.City, Forecast: forecast}, nil
}

func main() {
	// 1. Create function tools in Go
	emailTool, err := tools.NewFunc("extract-emails", "Extract email addresses from text using regex", extractEmails)
	if err != nil {
		log.Fatal(err)
	}
	weatherTool, err := tools.NewFunc("get-weather", "Get the current weather for a city", getWeather)
	if err != nil {
		log.Fatal(err)
	}

	// 2. Register tools in a ToolRegistry
	toolRegistry := recipe.NewStaticToolRegistry()
	toolRegistry.Register("extract-emails", emailTool)
	toolRegistry.Register("get-weather", weatherTool)

	// 3. Register models
	modelRegistry := recipe.NewRegistry()
	modelRegistry.Register("gpt-4o", openai.NewModel("gpt-4o", openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	}))

	// 4. Load recipe
	spec, err := recipe.LoadFromFile("agent.yaml")
	if err != nil {
		log.Fatal(err)
	}

	// 5. Build tool-mode agent (sub-recipes + function tools)
	agent, err := recipe.Build(spec,
		recipe.WithModelRegistry(modelRegistry),
		recipe.WithToolRegistry(toolRegistry),
		recipe.WithParams(map[string]any{"topic": "climate change impact on agriculture"}),
	)
	if err != nil {
		log.Fatal(err)
	}

	// 6. Run with streaming
	runner := blades.NewRunner(agent)
	stream := runner.RunStream(context.Background(), blades.UserMessage(
		"Research the topic and provide a summary with verified facts and data analysis.",
	))
	for message, err := range stream {
		if err != nil {
			log.Fatal(err)
		}
		if text := message.Text(); text != "" {
			log.Printf("[%s] %s\n", message.Author, text)
		}
	}
}
