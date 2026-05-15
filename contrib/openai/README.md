# OpenAI Provider

This package adapts OpenAI-compatible chat, image, and audio APIs to the Blades `model.Provider` protocol.

## Chat

```go
provider := openai.NewModel("gpt-5",
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
