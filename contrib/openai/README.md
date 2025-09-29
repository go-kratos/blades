# OpenAI Providers

This package offers helpers that adapt OpenAI APIs to the generic `blades.ModelProvider` interface.

- `NewChatProvider` wraps the chat completion endpoints for text and multimodal conversations.
- `NewImageProvider` wraps the image generation endpoint (`/v1/images/generations`) and returns image bytes or URLs as `DataPart`/`FilePart` message contents.
- `NewAudioProvider` wraps the text-to-speech endpoint (`/v1/audio/speech`) and returns synthesized audio as `DataPart` payloads.

## Configuration

All providers accept configuration options to avoid exposing OpenAI library types directly:

```go
provider := openai.NewChatProvider(
    openai.WithAPIKey("your-api-key"),
    openai.WithBaseURL("https://api.openai.com/v1"),
    openai.WithRequestTimeout(30*time.Second),
    openai.WithMaxRetries(3),
)
```

Available options: `WithAPIKey`, `WithBaseURL`, `WithOrganization`, `WithProject`, `WithHTTPClient`, `WithRequestTimeout`, `WithMaxRetries`, `WithHeader`.

## Examples

```go
// Image generation
provider := openai.NewImageProvider(
    openai.WithAPIKey("your-api-key"),
)
req := &blades.ModelRequest{
    Model: "gpt-image-1",
    Messages: []*blades.Message{
        blades.UserMessage("a watercolor painting of a cozy reading nook"),
    },
}
res, err := provider.Generate(ctx, req, blades.ImageSize("1024x1024"))
```

```go
// Text-to-speech
provider := openai.NewAudioProvider(
    openai.WithAPIKey("your-api-key"),
)
req := &blades.ModelRequest{
    Model: "gpt-4o-mini-tts",
    Messages: []*blades.Message{
        blades.UserMessage("Hello from Blades audio!"),
    },
}
res, err := provider.Generate(ctx, req, blades.AudioVoice("alloy"), blades.AudioResponseFormat("mp3"))
```
