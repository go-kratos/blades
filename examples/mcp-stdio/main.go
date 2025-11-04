package main

import (
	"context"
	"fmt"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/mcp"
	"github.com/go-kratos/blades/contrib/openai"
)

func main() {
	// 1. Configure MCP server to use the official time server
	// This uses the @modelcontextprotocol/server-time from npm
	mcpResolver, err := mcp.NewToolsResolver(
		mcp.ClientConfig{
			Name:      "time",
			Transport: mcp.TransportStdio,
			Command:   "npx",
			Args:      []string{"-y", "@modelcontextprotocol/server-time"},
		},
	)
	if err != nil {
		log.Fatalf("Failed to create MCP tools resolver: %v", err)
	}
	defer mcpResolver.Close()

	// 2. Create OpenAI provider (requires OPENAI_API_KEY environment variable)
	openaiProvider := openai.NewChatProvider()

	// 3. Create Agent with MCP tools resolver
	// The resolver will dynamically provide tools from the MCP server
	agent := blades.NewAgent("time-assistant",
		blades.WithModel("gpt-5"),
		blades.WithProvider(openaiProvider),
		blades.WithInstructions("You are a helpful assistant that can tell time in different timezones."),
		blades.WithToolsResolver(mcpResolver),
	)

	// 4. Ask the agent about time
	prompt := blades.NewPrompt(
		blades.UserMessage("What time is it right now?"),
	)

	fmt.Println("Asking agent: What time is it right now?")
	fmt.Println("--------------------------------------------------")

	ctx := context.Background()
	result, err := agent.Run(ctx, prompt)
	if err != nil {
		log.Fatalf("Agent run failed: %v", err)
	}

	fmt.Printf("Agent: %s\n", result.Text())

	// 5. Ask about a specific timezone
	prompt2 := blades.NewPrompt(
		blades.UserMessage("What time is it in Tokyo right now?"),
	)

	fmt.Println("\n--------------------------------------------------")
	fmt.Println("Asking agent: What time is it in Tokyo right now?")
	fmt.Println("--------------------------------------------------")

	result2, err := agent.Run(ctx, prompt2)
	if err != nil {
		log.Fatalf("Agent run failed: %v", err)
	}

	fmt.Printf("Agent: %s\n", result2.Text())
}
