# OpenAI Provider

This package adapts OpenAI-compatible chat, responses, image, and audio APIs to the Blades `model.Provider` protocol.

## Chat

```go
provider := openai.NewChat("gpt-5",
    openai.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    openai.WithParallelToolCalls(true),
)

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

Streaming uses the same request:

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

`WithParallelToolCalls(false)` maps to OpenAI `parallel_tool_calls=false`. The Agent Loop does not read this option; it only executes the tool calls the model actually returns.

Chat tool-call arguments are syntax-checked and semantically repaired by default after all argument deltas have been accumulated. The default `jsonrepair.PermissiveEngine` can normalize or discard malformed syntax, while valid JSON remains byte-for-byte unchanged. Disable receive-side repair to retain strict rejection:

```go
provider := openai.NewChat("gpt-5",
    openai.WithToolInputJSONRepairer(nil),
)
```

Use `WithToolInputJSONRepairer` to supply a custom [`jsonrepair.Repairer`](../../jsonrepair), for example to set tighter resource limits. Passing nil disables repair.

## Responses

```go
provider := openai.NewResponses("gpt-5",
    openai.WithResponsesAPIKey(os.Getenv("OPENAI_API_KEY")),
    openai.WithResponsesParallelToolCalls(true),
)

resp, err := provider.Generate(ctx, req)
```

`NewResponses` uses the Responses API with Blades-managed full-history input by default. `WithResponsesPreviousResponseID` is available for explicit OpenAI server-side response chaining.

Responses function-call arguments use the same default-on repair behavior. The Responses API has a prefixed opt-out because its option type is separate:

```go
provider := openai.NewResponses("gpt-5",
    openai.WithResponsesToolInputJSONRepairer(nil),
)
```

Use `WithResponsesToolInputJSONRepairer` for a custom repairer; passing nil disables repair. Valid JSON is preserved byte-for-byte, and repaired inputs still pass through normal schema, policy, and authorization checks.

## Image

```go
provider := openai.NewImage("gpt-image-1", openai.ImageConfig{
    APIKey: os.Getenv("OPENAI_API_KEY"),
    Size:   "1024x1024",
})

resp, err := provider.Generate(ctx, &model.Request{
    Messages: []*model.Message{
        {
            Role:  model.RoleUser,
            Parts: []content.Part{content.Text{Text: "a watercolor painting of a cozy reading nook"}},
        },
    },
})
```

## Audio

```go
provider := openai.NewAudio("gpt-4o-mini-tts", openai.AudioConfig{
    APIKey:         os.Getenv("OPENAI_API_KEY"),
    Voice:          "alloy",
    ResponseFormat: "mp3",
})

resp, err := provider.Generate(ctx, &model.Request{
    Messages: []*model.Message{
        {
            Role:  model.RoleUser,
            Parts: []content.Part{content.Text{Text: "Hello from Blades audio!"}},
        },
    },
})
```
