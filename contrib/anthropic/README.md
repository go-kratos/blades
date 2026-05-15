# Anthropic Provider

This package adapts Claude Messages API responses to the Blades `model.Provider` protocol.

## Basic Usage

```go
provider := anthropic.NewModel("claude-sonnet-4-20250514",
    anthropic.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")),
    anthropic.WithParallelToolCalls(true),
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

## Tool Calls

Tool schemas are supplied on `model.Request.Tools`. Claude tool-use blocks are converted to `content.ToolUse`; tool results should be sent back as a `model.RoleTool` message containing `content.ToolResult`.

When used through `blades.NewAgent`, the Agent Loop handles this cycle:

1. stream Claude response chunks;
2. collect `content.ToolUse` parts from the assistant message;
3. execute the returned tool wave concurrently;
4. append ordered `content.ToolResult` parts to the session;
5. continue with the next model step.

`WithParallelToolCalls(false)` maps to Claude `tool_choice.auto.disable_parallel_tool_use=true`. The Agent Loop does not inspect this option; it executes the tool wave returned by the model.

## Request Options

Provider defaults are configured on `NewModel` with functional options. Request-level hints can still be supplied with `model.Request.Options` and override provider defaults by option type.

```go
provider := anthropic.NewModel("claude-sonnet-4-20250514",
    anthropic.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")),
    anthropic.WithParallelToolCalls(false),
)

req.Options = []model.Option{
    model.ParallelToolCalls{Enabled: true},
}
```
