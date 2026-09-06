package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/mcp"
	"github.com/go-kratos/blades/contrib/openai"
)

// This example connects a Blades agent to the You.com remote MCP server
// (https://api.you.com/mcp) over the HTTP transport, so the agent can use
// current web search tools such as you-search.
//
// Setup:
//
//	export OPENAI_API_KEY=...   # any OpenAI-compatible endpoint via OPENAI_BASE_URL
//	export OPENAI_MODEL=...
//	export YDC_API_KEY=...      # You.com API key from https://you.com/platform/api-keys
//
// The keyless profile (https://api.you.com/mcp?profile=free) exposes the basic
// you-search tool without an API key; drop the Authorization header and switch
// the Endpoint to use it.
func main() {
	endpoint := "https://api.you.com/mcp"
	headers := map[string]string{}
	if key := os.Getenv("YDC_API_KEY"); key != "" {
		headers["Authorization"] = "Bearer " + key
	} else {
		// Keyless fallback: the free profile provides basic you-search.
		endpoint = "https://api.you.com/mcp?profile=free"
	}

	mcpResolver, err := mcp.NewToolsResolver(
		mcp.ClientConfig{
			Name:      "youcom",
			Transport: mcp.TransportHTTP,
			Endpoint:  endpoint,
			Headers:   headers,
			Timeout:   30 * time.Second,
		},
	)
	if err != nil {
		log.Fatalf("Failed to create MCP tools resolver: %v", err)
	}
	defer mcpResolver.Close()

	model := openai.NewModel(os.Getenv("OPENAI_MODEL"), openai.Config{
		BaseURL: os.Getenv("OPENAI_BASE_URL"),
		APIKey:  os.Getenv("OPENAI_API_KEY"),
	})

	agent, err := blades.NewAgent("web-research-assistant",
		blades.WithModel(model),
		blades.WithInstruction(
			"You are a research assistant. When a question depends on current web information, "+
				"use the You.com tools from the youcom MCP server to search the web and cite the URLs you used."),
		blades.WithToolsResolver(mcpResolver),
	)
	if err != nil {
		log.Fatal(err)
	}

	query := "What changed in the latest Go release?"
	if len(os.Args) > 1 {
		query = strings.Join(os.Args[1:], " ")
	}

	fmt.Printf("Asking agent: %s\n", query)
	fmt.Println("--------------------------------------------------")

	ctx := context.Background()
	runner := blades.NewRunner(agent)
	output, err := runner.Run(ctx, blades.UserMessage(query))
	if err != nil {
		log.Fatalf("Agent run failed: %v", err)
	}
	fmt.Printf("Agent: %s\n", output.Text())
}
