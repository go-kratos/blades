package main

import (
	"context"
	"log"
	"os"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/planner"
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
	session, ok := blades.FromSessionContext(ctx)
	if !ok {
		return WeatherRes{}, blades.ErrNoSessionContext
	}
	session.SetState("location", req.Location)
	resp := WeatherRes{Forecast: "Sunny, 20°C"}
	log.Printf("Fetching weather for:%s, resp:%+v\n", req.Location, resp)
	return resp, nil
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
	model := openai.NewModel(os.Getenv("OPENAI_MODEL"), openai.Config{
		BaseURL: os.Getenv("OPENAI_API_BASE_URL"),
		APIKey:  os.Getenv("OPENAI_API_KEY"),
	})
	opts := []blades.AgentOption{
		blades.WithModel(model),
		blades.WithInstruction("You are a helpful assistant that provides detailed and accurate information."),
		blades.WithTools(weatherTool),
		blades.WithPlanner(planner.NewReactPlanner()),
	}
	agent, err := blades.NewAgent("Planner Agent", opts...)
	if err != nil {
		log.Fatal(err)
	}
	input := blades.UserMessage("What is the weather in Beijing?")
	runner := blades.NewRunner(agent)
	output, err := runner.Run(context.Background(), input)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(output.Text())
}
