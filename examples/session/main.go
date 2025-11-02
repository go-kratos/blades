package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
)

func main() {
	agent := blades.NewAgent(
		"History Tutor",
		blades.WithModel("qwen-plus"),
		blades.WithInstructions("You are a knowledgeable history tutor. Provide detailed and accurate information on historical events."),
		blades.WithProvider(openai.NewChatProvider()),
	)
	prompt := blades.NewPrompt(
		blades.UserMessage("Can you tell me about the causes of World War II?"),
	)
	// Create a new session
	session := blades.NewSession("conversation_123")
	ctx := blades.NewSessionContext(context.Background(), session)
	// Run the agent
	result, err := agent.Run(ctx, prompt)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(result.Text())
}
