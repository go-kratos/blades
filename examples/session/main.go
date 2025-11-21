package main

import (
	"context"
	"log"
	"os"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
)

func main() {
	model := openai.NewModel(os.Getenv("OPENAI_MODEL"), openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	})
	agent, err := blades.NewAgent(
		"History Tutor",
		blades.WithModel(model),
		blades.WithInstruction("You are a knowledgeable history tutor. Provide detailed and accurate information on historical events."),
	)
	if err != nil {
		log.Fatal(err)
	}
	input := blades.UserMessage("Can you tell me about the causes of World War II?")
	// Create a new session
	session := blades.NewSession()
	// Run the agent
	ctx := context.Background()
	runner := blades.NewRunner(agent)
	output, err := runner.Run(ctx, input, blades.WithSession(session))
	if err != nil {
		log.Fatal(err)
	}
	log.Println(output.Text())
}
