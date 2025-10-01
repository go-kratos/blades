package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/gemini"
	"github.com/google/jsonschema-go/jsonschema"
)

func main() {
	ctx := context.Background()

	// Get API key from environment
	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		log.Fatal("Please set GOOGLE_API_KEY environment variable")
	}

	fmt.Println("=== Simple Gemini Example ===")
	fmt.Println()

	// Create Gemini client with basic configuration
	config := gemini.NewGeminiConfig(
		gemini.WithGenAI(apiKey),
		gemini.WithDefaultSafetySettings(), // Enable default safety filtering
	)

	client, err := gemini.NewClient(ctx, config)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Simple text generation
	request := &blades.ModelRequest{
		Model: "gemini-2.5-flash-preview-09-2025",
		Messages: []*blades.Message{
			{
				Role: blades.RoleUser,
				Parts: []blades.Part{
					blades.TextPart{Text: "Write a short poem about artificial intelligence."},
				},
			},
		},
	}

	// Generate response
	response, err := client.Generate(ctx, request,
		blades.Temperature(0.7),
		blades.MaxOutputTokens(200),
	)
	if err != nil {
		log.Fatalf("Failed to generate response: %v", err)
	}

	if len(response.Messages) > 0 && len(response.Messages[0].Parts) > 0 {
		if textPart, ok := response.Messages[0].Parts[0].(blades.TextPart); ok {
			fmt.Printf("🤖 Response:\n%s\n", textPart.Text)
		}
	}

	fmt.Println()
	fmt.Println("---")
	fmt.Println()

	// Example 2: Demonstrate thinking features
	fmt.Println("🧠 Example 2: Thinking Configuration")

	thinkingConfig := gemini.NewGeminiConfig(
		gemini.WithGenAI(apiKey),
		gemini.WithThinkingBudget(1000),  // Enable reasoning
		gemini.WithIncludeThoughts(true), // Include thinking process in response
		gemini.WithDefaultSafetySettings(),
	)

	thinkingClient, err := gemini.NewClient(ctx, thinkingConfig)
	if err != nil {
		log.Printf("Failed to create thinking client: %v", err)
	} else {
		thinkingRequest := &blades.ModelRequest{
			Model: "gemini-2.5-flash-preview-09-2025",
			Messages: []*blades.Message{
				{
					Role: blades.RoleUser,
					Parts: []blades.Part{
						blades.TextPart{Text: "What's 15 × 23? Show your reasoning step by step."},
					},
				},
			},
		}

		thinkingResponse, err := thinkingClient.Generate(ctx, thinkingRequest,
			blades.Temperature(0.3),
			blades.MaxOutputTokens(300),
		)
		if err != nil {
			log.Printf("Failed to generate thinking response: %v", err)
		} else if len(thinkingResponse.Messages) > 0 && len(thinkingResponse.Messages[0].Parts) > 0 {
			if textPart, ok := thinkingResponse.Messages[0].Parts[0].(blades.TextPart); ok {
				fmt.Printf("🤖 Response with thinking:\n%s\n", textPart.Text)
			}
		}
	}

	fmt.Println()
	fmt.Println("---")
	fmt.Println()

	// Example 3: Function calling
	fmt.Println("🔧 Example 3: Function Calling")

	// Define a simple weather tool
	weatherTool := &blades.Tool{
		Name:        "get_weather",
		Description: "Get current weather information for a city",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"city": {Type: "string"},
			},
			Required: []string{"city"},
		},
		Handle: func(ctx context.Context, city string) (string, error) {
			var payload struct {
				City string `json:"city"`
			}
			if err := json.Unmarshal([]byte(city), &payload); err == nil && strings.TrimSpace(payload.City) != "" {
				city = payload.City
			}

			// Simple mock weather data
			return fmt.Sprintf("Weather data for query: %s - Temperature: 22°C, Sunny", city), nil
		},
	}

	// Create request with tool
	toolRequest := &blades.ModelRequest{
		Model: "gemini-2.5-flash-preview-09-2025",
		Messages: []*blades.Message{
			{
				Role: blades.RoleUser,
				Parts: []blades.Part{
					blades.TextPart{Text: "What's the weather like in Tokyo? Please use the weather tool."},
				},
			},
		},
		Tools: []*blades.Tool{weatherTool},
	}

	// Generate response with tool calling
	toolResponse, err := client.Generate(ctx, toolRequest,
		blades.Temperature(0.3),
		blades.MaxOutputTokens(300),
	)
	if err != nil {
		log.Printf("Failed to generate tool response: %v", err)
	} else if len(toolResponse.Messages) > 0 {
		for _, msg := range toolResponse.Messages {
			if len(msg.Parts) > 0 {
				if textPart, ok := msg.Parts[0].(blades.TextPart); ok {
					fmt.Printf("🤖 Response with tool:\n%s\n", textPart.Text)
				}
			}
			// Print tool calls if any
			if len(msg.ToolCalls) > 0 {
				fmt.Printf("🔧 Tool calls executed:\n")
				for _, tc := range msg.ToolCalls {
					fmt.Printf("  - %s: %s -> %s\n", tc.Name, tc.Arguments, tc.Result)
				}
			}
		}
	}

	fmt.Println()
	fmt.Println("✨ Gemini integration examples complete!")
	fmt.Println()
	fmt.Println("💡 Available configuration options:")
	fmt.Println("   - WithGenAI(apiKey) / WithVertexAI(project, location)")
	fmt.Println("   - WithDefaultSafetySettings() / WithSafetySettings(custom)")
	fmt.Println("   - WithThinkingBudget(tokens) - Enable reasoning")
	fmt.Println("   - WithIncludeThoughts(bool) - Show thinking process")
	fmt.Println("   - WithCredentialsPath(path) / WithCredentialsContent(json)")
}
