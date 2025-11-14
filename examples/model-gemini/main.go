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
	config := &genai.ClientConfig{APIKey: apiKey}
	model, err := google.NewModel(ctx, "gemini-2.5-flash-preview-09-2025", config)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	// Simple text generation
	request := &blades.ModelRequest{
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
	response, err := model.Generate(ctx, request)
	if err != nil {
		log.Fatalf("Failed to generate response: %v", err)
	}
	log.Println("🤖 Response:", response.Message.Text())
}
