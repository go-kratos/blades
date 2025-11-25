package main

import (
	"context"
	"log"
	"os"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/flow"
)

func main() {
	model := openai.NewModel(os.Getenv("OPENAI_MODEL"), openai.Config{
		BaseURL: os.Getenv("OPENAI_BASE_URL"),
		APIKey:  os.Getenv("OPENAI_API_KEY"),
	})
	config := flow.DeepConfig{
		Name:          "DeepAgent",
		Model:         model,
		Description:   "An intelligent agent that can decompose complex tasks into manageable subtasks and execute them effectively.",
		MaxIterations: 100,
	}
	agent, err := flow.NewDeepAgent(config)
	if err != nil {
		log.Fatal(err)
	}
	// input := blades.UserMessage("What preparations should a beginner make before going to the gym? Please help me make a to-do list use write_todos")
	input := blades.UserMessage("I want to conduct research on the accomplishments of Lebron James, Michael Jordan, and Kobe Bryant, and then compare them (You can use the to-do tool to decompose the task).")
	runner := blades.NewRunner(agent)
	output, err := runner.Run(context.Background(), input)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(output.Text())
}
