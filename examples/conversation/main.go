package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/memory"
)

func main() {
	agent := blades.NewAgent(
		"History Tutor",
		blades.WithModel("qwen-plus"),
		blades.WithInstructions("You are a knowledgeable history tutor. Provide detailed and accurate information on historical events."),
		blades.WithProvider(openai.NewChatProvider()),
		blades.WithMemory(memory.NewInMemory(10)),
	)
	// Example conversation in memory
	prompt := blades.NewConversation(
		"conversation_123",
		blades.UserMessage("Can you tell me about the causes of World War II?"),
	)
	result, err := agent.Run(context.Background(), prompt)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(result.Text())
}
