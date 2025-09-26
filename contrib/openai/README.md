# OpenAI Providers

This package offers helpers that adapt OpenAI APIs to the generic `blades.ModelProvider` interface.

- `NewChatProvider` wraps the chat completion endpoints for text and multimodal conversations.
- `NewImageProvider` wraps the image generation endpoint (`/v1/images/generations`) and returns image bytes or URLs as `DataPart`/`FilePart` message contents.
- `NewAudioProvider` wraps the text-to-speech endpoint (`/v1/audio/speech`) and returns synthesized audio as `DataPart` payloads.

```go
provider := openai.NewImageProvider()
req := &blades.ModelRequest{
    Model: "gpt-image-1",
    Messages: []*blades.Message{
        blades.UserMessage("a watercolor painting of a cozy reading nook"),
    },
}
res, err := provider.Generate(ctx, req, blades.ImageSize("1024x1024"))
```

```go
provider := openai.NewAudioProvider()
req := &blades.ModelRequest{
    Model: "gpt-4o-mini-tts",
    Messages: []*blades.Message{
        blades.UserMessage("Hello from Blades audio!"),
    },
}
res, err := provider.Generate(ctx, req, blades.AudioVoice("alloy"), blades.AudioResponseFormat("mp3"))
```
