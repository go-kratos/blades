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
	model := openai.NewModel("gpt-5", openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	})
	mathTutorAgent, err := blades.NewAgent(
		"MathTutor",
		blades.WithDescription("An agent that helps with math questions"),
		blades.WithInstructions("You are a helpful math tutor. Answer questions related to mathematics."),
		blades.WithModel(model),
	)
	if err != nil {
		log.Fatal(err)
	}
	historyTutorAgent, err := blades.NewAgent(
		"HistoryTutor",
		blades.WithDescription("An agent that helps with history questions"),
		blades.WithInstructions("You are a helpful history tutor. Answer questions related to history."),
		blades.WithModel(model),
	)
	if err != nil {
		log.Fatal(err)
	}
	agent, err := flow.NewHandoffAgent(flow.HandoffConfig{
		Name:        "TriageAgent",
		Description: "You determine which agent to use based on the user's homework question",
		Model:       model,
		SubAgents: []blades.Agent{
			mathTutorAgent,
			historyTutorAgent,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	input := blades.UserMessage("What is the capital of France?")
	runner := blades.NewRunner(agent)
	output, err := runner.Run(context.Background(), input)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(output.Text())
}
