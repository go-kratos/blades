package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
)

func main() {
	agent := blades.NewAgent(
		"Instructions Agent",
		blades.WithModel("gpt-5"),
		blades.WithProvider(openai.NewChatProvider()),
		blades.WithInstructions("Respond as a {{.style}}."),
	)
	prompt := blades.NewPrompt(
		blades.UserMessage("Tell me a joke."),
	)
	// Create a new session
	session := blades.NewSession(map[string]any{
		"style": "robot",
	})
	// Run the agent with the prompt and session context
	runner := blades.NewRunner(agent, blades.WithSession(session))
	message, err := runner.Run(context.Background(), prompt)
	if err != nil {
		panic(err)
	}
	log.Println(message.Text())
}
