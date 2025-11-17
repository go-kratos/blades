# Gemini Provider for Blades

Google Gemini model provider for the Blades AI Agent framework, supporting both GenAI and Vertex AI deployments.

## Installation

```bash
go get github.com/go-kratos/blades/contrib/gemini
```

## Features

- **Unified Client**: Single `NewClient` function for GenAI SDK
- **Thinking Models**: Support for thinking models with configurable reasoning budgets
- **Tool Calling**: Automatic tool execution with iterative workflows
- **Streaming**: Real-time response streaming with tool call handling
- **Multimodal**: Support for text, images, and other modalities

## Usage

### Basic Example

```go
import (
	"context"
	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/gemini"
	"google.golang.org/genai"
)

// Create client with GenAI configuration
clientConfig := &genai.ClientConfig{
	APIKey:  "your-api-key",
	Backend: genai.BackendGoogleAI,
}

client, err := gemini.NewClient(context.Background(), clientConfig)
if err != nil {
	panic(err)
}

// Generate response
req := &blades.ModelRequest{
	Model: "gemini-2.0-flash-exp",
	Messages: []*blades.Message{
		{
			Role: blades.RoleUser,
			Parts: []blades.Part{
				blades.TextPart{Text: "What is the capital of France?"},
			},
		},
	},
}

resp, err := client.Generate(context.Background(), req)
if err != nil {
	panic(err)
}

// Access response
for _, msg := range resp.Messages {
	for _, part := range msg.Parts {
		if textPart, ok := part.(blades.TextPart); ok {
			fmt.Println(textPart.Text)
		}
	}
}
```

### Vertex AI

```go
import (
	"cloud.google.com/go/auth"
	"google.golang.org/genai"
)

// Create client with Vertex AI
creds, err := auth.Detect(context.Background(), &auth.DetectOptions{
	// Configure auth options
})
if err != nil {
	panic(err)
}

clientConfig := &genai.ClientConfig{
	Backend:  genai.BackendVertexAI,
	Project:  "my-project-id",
	Location: "us-central1",
	Credentials: creds.TokenProvider(),
}

client, err := gemini.NewClient(context.Background(), clientConfig)
// Use same request/response pattern as above
```

## Thinking Models

Enable Gemini's reasoning capabilities with thinking models:

```go
import "github.com/go-kratos/blades"

// Configure thinking budget
thinkingBudget := int32(10000) // 10K tokens for reasoning
includeThoughts := true

resp, err := client.Generate(context.Background(), req,
	blades.WithThinkingBudget(&thinkingBudget),
	blades.WithIncludeThoughts(&includeThoughts),
)

// The response will include thinking blocks showing the model's reasoning process
for _, msg := range resp.Messages {
	for _, part := range msg.Parts {
		if textPart, ok := part.(blades.TextPart); ok {
			fmt.Println(textPart.Text)
		}
	}
}
```

## Model Options

```go
resp, err := client.Generate(context.Background(), req,
	blades.WithMaxOutputTokens(4096),
	blades.WithTemperature(0.7),
	blades.WithTopP(0.9),
	blades.WithMaxIterations(5), // Tool calling iterations
)
```

## Tool Calling

Define tools and let Gemini automatically execute them:

```go
// Define a tool
weatherTool := &blades.Tool{
	Name:        "get_weather",
	Description: "Get current weather for a location",
	Handle: func(ctx context.Context, arguments string) (string, error) {
		// Parse arguments and fetch weather
		return `{"temperature": 72, "condition": "sunny"}`, nil
	},
}

// Add tools to request
req := &blades.ModelRequest{
	Model: "gemini-2.0-flash-exp",
	Tools: []*blades.Tool{weatherTool},
	Messages: []*blades.Message{
		{
			Role: blades.RoleUser,
			Parts: []blades.Part{
				blades.TextPart{Text: "What's the weather in San Francisco?"},
			},
		},
	},
}

// Generate with automatic tool execution
resp, err := client.Generate(context.Background(), req)

// Gemini will automatically:
// 1. Call the tool if needed
// 2. Process the tool result
// 3. Return the final response
```

## Streaming

Stream responses in real-time:

```go
stream, err := client.NewStream(context.Background(), req)
if err != nil {
	panic(err)
}

for stream.Next() {
	resp, err := stream.Current()
	if err != nil {
		panic(err)
	}

	for _, msg := range resp.Messages {
		for _, part := range msg.Parts {
			if textPart, ok := part.(blades.TextPart); ok {
				fmt.Print(textPart.Text) // Print as it streams
			}
		}
	}
}

if err := stream.Err(); err != nil {
	panic(err)
}
```

## Error Handling

The provider returns specific errors for common issues:

```go
import "github.com/go-kratos/blades/contrib/gemini"

resp, err := client.Generate(ctx, req)
if err != nil {
	switch err {
	case gemini.ErrEmptyResponse:
		// Handle empty response
	case gemini.ErrToolNotFound:
		// Handle missing tool
	case gemini.ErrTooManyIterations:
		// Handle iteration limit
	default:
		// Handle other errors
	}
}
```

## Models

Available models:
- `gemini-2.0-flash-exp` - Latest experimental model
- `gemini-2.0-flash-thinking-exp-01-21` - Thinking model with extended reasoning
- `gemini-1.5-pro` - Previous generation pro model
- `gemini-1.5-flash` - Previous generation fast model

## License

See the main Blades repository for license information.
