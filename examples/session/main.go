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
	prompt := blades.NewPrompt(
		blades.UserMessage("Can you tell me about the causes of World War II?"),
	)
	session := blades.NewSession("conversation_123")
	ctx := blades.NewSessionContext(context.Background(), session)
	result, err := agent.Run(ctx, prompt)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(result.Text())
}
