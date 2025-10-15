package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/memory"
)

func main() {
	ctx := context.Background()
	memoryStore := memory.NewInMemoryStore()
	memoryTool, err := memory.NewMemoryTool(memoryStore)
	if err != nil {
		log.Fatal(err)
	}
	memoryStore.AddMemory(ctx, &memory.Memory{Content: blades.AssistantMessage("My favorite project is the Blades Agent kit.")})
	memoryStore.AddMemory(ctx, &memory.Memory{Content: blades.AssistantMessage("My favorite programming language is Go.")})
	// Create an agent with memory tool
	agent := blades.NewAgent(
		"MemoryRecallAgent",
		blades.WithModel("gpt-5"),
		blades.WithInstructions("Answer the user's question. Use the 'Memory' tool if the answer might be in past conversations."),
		blades.WithProvider(openai.NewChatProvider()),
		blades.WithTools(memoryTool),
	)
	// Example conversation in memory
	prompt := blades.NewPrompt(
		blades.UserMessage("What is my favorite project?"),
	)
	result, err := agent.Run(ctx, prompt)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(result.Text())
}
