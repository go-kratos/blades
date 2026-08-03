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
5. end the current turn and continue with the next turn/model call.

`WithParallelToolCalls(false)` maps to Claude `tool_choice.auto.disable_parallel_tool_use=true`. The Agent Loop does not inspect this option; it executes the tool wave returned by the model.

Completed streamed tool input is syntax-checked and semantically repaired by default before a `content.ToolUse` is emitted. Disable repair to retain strict rejection:

```go
provider := anthropic.NewModel("claude-sonnet-4-20250514",
    anthropic.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")),
    anthropic.WithToolInputJSONRepairer(nil),
)
```

The provider-neutral [`github.com/go-kratos/blades/jsonrepair`](../../jsonrepair) package uses only the Go standard library. `jsonrepair.New()` returns the package's sole `PermissiveEngine`, which preserves valid JSON byte-for-byte and reports malformed recovery as a whole-document rewrite. Recovery can normalize or discard malformed syntax. Use `WithToolInputJSONRepairer` to supply an engine with custom limits; passing nil disables repair. It cannot reconstruct content that was never sent, so normal schema, policy, and authorization checks still apply.

At clean stream EOF, the provider automatically finalizes an accumulated tool input block even if `content_block_stop` was not received. The default repairer can close truncated strings and containers at that boundary. With `WithToolInputJSONRepairer(nil)`, malformed EOF input remains a strict validation error.

An EOF-truncated tool argument may be semantically incomplete even after its JSON syntax is repaired. Ensure the surrounding tool schema and policy can safely review or reject the recovered call.

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
