package main

import (
	"context"
	"log"
	"os"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/google"
	"google.golang.org/genai"
)

func main() {
	// Get API key from environment
	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		log.Fatal("Please set GOOGLE_API_KEY environment variable")
	}
	// Create Gemini client with basic configuration
	ctx := context.Background()
	config := &genai.ClientConfig{
		APIKey: apiKey,
	}
	client, err := google.NewProvider(ctx, config)
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
	log.Println("🤖 Response:", response.Message.Text())
}
