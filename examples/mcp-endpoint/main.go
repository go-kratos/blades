package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/mcp"
	"github.com/go-kratos/blades/contrib/openai"
)

func main() {
	// https://github.com/modelcontextprotocol/servers/tree/main/src/time
	mcpResolver, err := mcp.NewToolsResolver(
		mcp.ClientConfig{
			Name:      "github",
			Transport: mcp.TransportHTTP,
			Endpoint:  "http://localhost:8000/mcp/time",
		},
	)
	if err != nil {
		log.Fatalf("Failed to create MCP tools resolver: %v", err)
	}
	defer mcpResolver.Close()

	// Create OpenAI provider (requires OPENAI_API_KEY environment variable)
	model := openai.NewModel(os.Getenv("OPENAI_MODEL"), openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	})

	// Create Agent with MCP tools resolver
	// The resolver will dynamically provide tools from the MCP server
	agent, err := blades.NewAgent("time-assistant",
		blades.WithModel(model),
		blades.WithInstruction("You are a helpful assistant that can tell time in different timezones."),
		blades.WithToolsResolver(mcpResolver),
	)
	if err != nil {
		log.Fatal(err)
	}
	// Ask the agent about time
	input := blades.UserMessage("What time is it right now?")

	fmt.Println("Asking agent: What time is it right now?")
	fmt.Println("--------------------------------------------------")

	ctx := context.Background()
	runner := blades.NewRunner(agent)
	output, err := runner.Run(ctx, input)
	if err != nil {
		log.Fatalf("Agent run failed: %v", err)
	}
	fmt.Printf("Agent: %s\n", output.Text())
}
