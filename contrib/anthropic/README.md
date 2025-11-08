# Claude Provider for Blades

Anthropic Claude model provider for the Blades AI Agent framework, supporting Direct API, AWS Bedrock, and Google Vertex AI.

## Installation

```bash
go get github.com/go-kratos/blades/contrib/claude
```

## Features

- **Unified Client**: Single `NewClient` function supporting multiple deployment channels
- **Extended Thinking**: Beta API support for reasoning models with configurable token budgets
- **Tool Calling**: Automatic tool execution with iterative workflows
- **Streaming**: Real-time response streaming with tool call handling
- **Multi-Channel**: Direct API, AWS Bedrock, and Google Vertex AI support

## Usage

### Direct API (Anthropic)

```go
import (
	"context"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/claude"
)

// Create client with API key
client := claude.NewClient(
	option.WithAPIKey("sk-ant-..."),
)

// Generate response
req := &blades.ModelRequest{
	Model: "claude-3-5-sonnet-20241022",
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

### AWS Bedrock

```go
import (
	"github.com/anthropics/anthropic-sdk-go/option/bedrock"
	"github.com/go-kratos/blades/contrib/claude"
)

// Create client with AWS Bedrock
client := claude.NewClient(
	bedrock.WithLoadDefaultConfig(context.Background()),
)

// Use same request/response pattern as above
```

### Google Vertex AI

```go
import (
	"github.com/anthropics/anthropic-sdk-go/option/vertex"
	"github.com/go-kratos/blades/contrib/claude"
)

// Create client with Google Vertex AI
client := claude.NewClient(
	vertex.WithGoogleAuth(
		context.Background(),
		"us-central1",      // region
		"my-project-id",    // projectID
	),
)

// Use same request/response pattern as above
```

## Extended Thinking

Enable Claude's reasoning capabilities with extended thinking using the standard API:

```go
import "github.com/go-kratos/blades"

// Configure thinking budget
thinkingBudget := int32(10000) // 10K tokens for reasoning

resp, err := client.Generate(context.Background(), req,
	blades.WithThinkingBudget(&thinkingBudget),
)

// The response will include thinking blocks showing Claude's reasoning process
// Access thinking content from the response messages
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

Define tools and let Claude automatically execute them:

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
	Model: "claude-3-5-sonnet-20241022",
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

// Claude will automatically:
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
import "github.com/go-kratos/blades/contrib/claude"

resp, err := client.Generate(ctx, req)
if err != nil {
	switch err {
	case claude.ErrEmptyResponse:
		// Handle empty response
	case claude.ErrToolNotFound:
		// Handle missing tool
	case claude.ErrTooManyIterations:
		// Handle iteration limit
	default:
		// Handle other errors
	}
}
```

## Models

Available models:
- `claude-3-5-sonnet-20241022` - Most intelligent model
- `claude-3-5-haiku-20241022` - Fastest model
- `claude-3-opus-20240229` - Previous generation
- `claude-sonnet-4-20250514` - Extended thinking model (use with `WithThinkingBudget`)

## License

See the main Blades repository for license information.
