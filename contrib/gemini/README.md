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

The Google GenAI SDK exposes function arguments to this adapter as `map[string]any`, not as the model's raw JSON bytes. The SDK has therefore already decoded the arguments before Blades receives them, so this package has no tool-input JSON repair option. A syntax failure during SDK response decoding must be handled before the provider adapter; re-marshaling the decoded map would always produce new valid JSON and could not recover bytes rejected upstream.

When used through `blades.NewAgent`, the Agent Loop owns tool execution, session commit, and follow-up turns. Each turn corresponds to one primary model call.
