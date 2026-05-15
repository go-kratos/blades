# Gemini Provider

This package adapts Google GenAI models to the Blades `model.Provider` protocol.

## Basic Usage

```go
provider, err := gemini.NewModel(
    ctx,
    "gemini-2.0-flash",
    gemini.WithClientConfig(genai.ClientConfig{
        APIKey:  os.Getenv("GEMINI_API_KEY"),
        Backend: genai.BackendGoogleAI,
    }),
)
if err != nil {
    return err
}

req := &model.Request{
    System: "You are a concise assistant.",
    Messages: []*model.Message{
        {
            Role:  model.RoleUser,
            Parts: []content.Part{content.Text{Text: "What is the capital of France?"}},
        },
    },
}

resp, err := provider.Generate(ctx, req)
```

## Vertex AI

```go
provider, err := gemini.NewModel(
    ctx,
    "gemini-2.0-flash",
    gemini.WithClientConfig(genai.ClientConfig{
        Backend:  genai.BackendVertexAI,
        Project:  "my-project-id",
        Location: "us-central1",
    }),
)
```

## Streaming

```go
for chunk, err := range provider.Stream(ctx, req) {
    if err != nil {
        return err
    }
    for _, part := range chunk.Parts {
        if text, ok := part.(content.Text); ok {
            fmt.Print(text.Text)
        }
    }
}
```

## Tools

Tool schemas are supplied on `model.Request.Tools`. Gemini function calls are converted to `content.ToolUse`, and function responses are represented as `content.ToolResult` in a `model.RoleTool` message.

When used through `blades.NewAgent`, the Agent Loop owns tool execution, session commit, and follow-up model steps.
