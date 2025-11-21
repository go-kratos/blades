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
		"Instructions Agent",
		blades.WithModel(model),
		blades.WithInstruction("Respond as a {{.style}}."),
	)
	if err != nil {
		log.Fatal(err)
	}
	// Create a new session
	session := blades.NewSession(map[string]any{
		"style": "robot",
	})
	input := blades.UserMessage("Tell me a joke.")
	ctx := context.Background()
	runner := blades.NewRunner(agent)
	// Run the agent with the prompt and session context
	message, err := runner.Run(ctx, input, blades.WithSession(session))
	if err != nil {
		panic(err)
	}
	log.Println(session.State())
	log.Println(message.Text())
}
