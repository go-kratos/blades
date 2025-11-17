package main

import (
	"context"
	"log"
	"os"

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
	model := openai.NewModel("gpt-5", openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	})
	agent, err := blades.NewAgent(
		"MemoryRecallAgent",
		blades.WithModel(model),
		blades.WithInstructions("Answer the user's question. Use the 'Memory' tool if the answer might be in past conversations."),
		blades.WithTools(memoryTool),
	)
	if err != nil {
		log.Fatal(err)
	}
	// Example conversation in memory
	input := blades.UserMessage("What is my favorite project?")
	runner := blades.NewRunner(agent)
	output, err := runner.Run(ctx, input)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(output.Text())
}
