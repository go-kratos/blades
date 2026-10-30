# MiniMax Provider

This package offers helpers that adapt MiniMax models to the generic
`blades.ModelProvider` interface.

MiniMax serves two API surfaces from two regions:

- an OpenAI-compatible chat completion endpoint, wrapped by `NewModel`;
- an Anthropic-compatible messages endpoint, wrapped by `NewAnthropicModel`.

`Config.Region` selects the service region (`RegionGlobal` or `RegionChina`) and
defaults to `RegionGlobal`. The base URL is derived from the region unless
`Config.BaseURL` is set explicitly.

| Region         | OpenAI-compatible base URL     | Anthropic-compatible base URL       |
| -------------- | ------------------------------ | ----------------------------------- |
| `RegionGlobal` | `https://api.minimax.io/v1`    | `https://api.minimax.io/anthropic`  |
| `RegionChina`  | `https://api.minimaxi.com/v1`  | `https://api.minimaxi.com/anthropic`|

The `Models` table records the context window, token pricing, cache pricing,
input modalities and thinking modes for each supported model.

```go
provider := minimax.NewModel(minimax.ModelM3, minimax.Config{
    Region: minimax.RegionGlobal,
    APIKey: os.Getenv("MINIMAX_API_KEY"),
})
req := &blades.ModelRequest{
    Messages: []*blades.Message{
        blades.UserMessage("Summarize the plot of Hamlet in two sentences."),
    },
}
res, err := provider.Generate(ctx, req)
```

```go
provider := minimax.NewAnthropicModel(minimax.ModelM27, minimax.Config{
    Region: minimax.RegionChina,
    APIKey: os.Getenv("MINIMAX_API_KEY"),
})
```
