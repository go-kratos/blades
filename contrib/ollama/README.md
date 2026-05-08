# Ollama Provider

The Ollama provider enables integration with [Ollama](https://ollama.ai/), allowing you to use local and remote Ollama models with the Blades framework.

## Installation

```bash
go get github.com/go-kratos/blades/contrib/ollama
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/go-kratos/blades"
    "github.com/go-kratos/blades/contrib/ollama"
)

func main() {
    // Create Ollama model provider
    model := ollama.NewModel("llama3.2", ollama.Config{
        BaseURL:     "http://localhost:11434", // Default
        Temperature: 0.7,
    })

    // Create agent with the model
    agent := blades.NewAgent(
        "Ollama Agent",
        blades.WithModel(model),
    )

    // Generate response
    runner := blades.NewRunner(agent)
    response, err := runner.Run(context.Background(), blades.UserMessage("Hello!"))
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(response.Text())
}
```

## Configuration

The `ollama.Config` struct provides various options to configure the Ollama client:

```go
type Config struct {
    BaseURL         string        // Ollama server URL (default: http://localhost:11434)
    Model           string        // Model name (passed separately to NewModel)
    Seed            int64         // Random seed for reproducible results
    MaxOutputTokens int           // Maximum number of tokens to generate
    FrequencyPenalty float64      // Repetition penalty (0-2)
    PresencePenalty  float64      // Presence penalty (0-2)
    Temperature      float64      // Sampling temperature (0-2)
    TopP             float64      // Nucleus sampling parameter (0-1)
    StopSequences    []string     // Sequences that stop generation
    KeepAlive        string       // How long to keep model loaded (e.g., "5m", "1h")
    Think           *string      // Enable thinking mode ("true", "false", "high", "medium", "low")
    Options         map[string]any // Additional model-specific options
}
```

### Environment Variables

- `OLLAMA_HOST`: Sets the Ollama server URL (overrides BaseURL)
- `OLLAMA_MODEL`: Default model name (used in examples)

## Features

### Streaming

Stream responses in real-time:

```go
err = runner.RunStream(ctx, input, func(resp *blades.Message) error {
    fmt.Print(resp.Text()) // Print each token as it arrives
    return nil
})
```

### System Instructions

Provide system-level instructions:

```go
response, err := model.Generate(ctx, &blades.ModelRequest{
    Instruction: &blades.Message{
        Role: blades.RoleSystem,
        Parts: []blades.Part{
            blades.TextPart{Text: "You are a helpful coding assistant."},
        },
    },
    Messages: []*blades.Message{
        {
            Role: blades.RoleUser,
            Parts: []blades.Part{
                blades.TextPart{Text: "Write a hello world function in Go."},
            },
        },
    },
})
```

### Structured Output

Generate JSON responses with a schema:

```go
response, err := model.Generate(ctx, &blades.ModelRequest{
    Messages: []*blades.Message{
        {
            Role: blades.RoleUser,
            Parts: []blades.Part{
                blades.TextPart{Text: "Generate user profile information."},
            },
        },
    },
    OutputSchema: &blades.JSONSchema{
        Type: "object",
        Properties: map[string]*blades.JSONSchema{
            "name": {Type: "string"},
            "age":  {Type: "integer"},
            "email": {Type: "string", Format: "email"},
        },
        Required: []string{"name", "age", "email"},
    },
})
```

### Tool Calling

Use tools with Ollama models:

```go
// Define a tool
weatherTool := tools.Func("get_weather", "Get current weather for a location",
    func(ctx context.Context, location string) (string, error) {
        // Weather API logic here
        return "72°F and sunny", nil
    })

response, err := model.Generate(ctx, &blades.ModelRequest{
    Messages: []*blades.Message{
        {
            Role: blades.RoleUser,
            Parts: []blades.Part{
                blades.TextPart{Text: "What's the weather like in New York?"},
            },
        },
    },
    Tools: []tools.Tool{weatherTool},
})
```

### Image Input

Send images with text (vision models only):

```go
response, err := model.Generate(ctx, &blades.ModelRequest{
    Messages: []*blades.Message{
        {
            Role: blades.RoleUser,
            Parts: []blades.Part{
                blades.TextPart{Text: "What do you see in this image?"},
                blades.FilePart{
                    URI:      "data:image/jpeg;base64,/9j/4AAQSkZJRgABAQAAAQ...",
                    MIMEType: "image/jpeg",
                },
            },
        },
    },
})
```

## Advanced Configuration

### Custom Model Options

Pass model-specific parameters:

```go
model := ollama.NewModel("llama3.2", ollama.Config{
    Temperature: 0.7,
    Options: map[string]any{
        "num_ctx":      4096,  // Context window size
        "num_thread":   4,     // Number of CPU threads
        "repeat_last_n": 64,    // Lookback window for repetition
    },
})
```

### Thinking Mode

Enable reasoning for supported models:

```go
think := "high"  // "true", "false", "high", "medium", "low"
model := ollama.NewModel("llama3.2", ollama.Config{
    Think: &think,
})
```

### Keep Alive

Control how long models stay loaded:

```go
model := ollama.NewModel("llama3.2", ollama.Config{
    KeepAlive: "30m", // Keep model loaded for 30 minutes
})
```

## Supported Models

Ollama provider works with any model available in your Ollama installation, including:

- Llama 3.1/3.2
- Mistral
- Mixtral
- Code Llama
- Qwen
- Gemma
- And many more...

Ensure the model is pulled in Ollama before use:

```bash
ollama pull llama3.2
```

## Error Handling

The provider returns standard Go errors for common issues:

- Connection errors when Ollama server is unavailable
- Model not found errors
- Invalid request parameters
- Timeout errors

```go
response, err := model.Generate(ctx, request)
if err != nil {
    // Handle different error types
    if strings.Contains(err.Error(), "connection refused") {
        log.Println("Ollama server is not running")
    } else if strings.Contains(err.Error(), "model not found") {
        log.Println("Model not found, pull it first with: ollama pull <model>")
    }
    return err
}
```

## Examples

See the [examples directory](../../../examples/model-ollama/) for complete working examples.