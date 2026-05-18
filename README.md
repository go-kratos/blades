<p align="center">
    <a href="https://github.com/go-kratos/blades/actions"><img src="https://github.com/go-kratos/blades/workflows/Go/badge.svg" alt="Build Status"></a>
    <a href="https://pkg.go.dev/github.com/go-kratos/blades"><img src="https://pkg.go.dev/badge/github.com/go-kratos/blades" alt="GoDoc"></a>
    <a href="https://deepwiki.com/go-kratos/blades"><img src="https://deepwiki.com/badge.svg" alt="DeepWiki"></a>
    <a href="https://github.com/go-kratos/blades/blob/main/LICENSE"><img src="https://img.shields.io/github/license/go-kratos/blades" alt="License"></a>
</p>

# Blades

[简体中文](./README_zh.md)

Blades is an event-driven multimodal Agent framework for Go. It provides a small public Agent interface, a provider-agnostic model protocol, tool execution, session history, prompt construction, context compaction, policy checks, lifecycle hooks, and multi-agent flow composition.

The framework is designed for applications that need LLM agents as normal Go components: explicit inputs and outputs, cancellable runtime contexts, provider adapters at the edge, and extension points that stay outside the core protocol.

## Why Blades

- **Event-first runtime**: applications interact with Agents through `event.Input` and `event.Output`, including streaming text, tool lifecycle events, turn endings, errors, and `Done`.
- **Provider-neutral core**: OpenAI, Anthropic, Gemini, MCP, and observability integrations live in `contrib/`; the root module does not depend on provider SDKs.
- **Multimodal protocol**: `content.Part` is the shared content union used by events, model messages, and tool results.
- **Tool-ready Agent loop**: tools are described by `tools.ToolSpec`, executed by `tools.Tool`, filtered by policy, observed by hooks, and committed back into session history.
- **Composable agents**: use `flow/` for sequential, parallel, loop, routing, and deep agent composition, or wrap any Agent as a tool with `blades.NewAgentTool`.

## Install

```sh
go get github.com/go-kratos/blades
go get github.com/go-kratos/blades/contrib/openai
```

Each provider integration is its own Go module. Add only the contrib modules your application uses:

```sh
go get github.com/go-kratos/blades/contrib/anthropic
go get github.com/go-kratos/blades/contrib/gemini
go get github.com/go-kratos/blades/contrib/mcp
```

## Quick Start

```go
package main

import (
    "context"
    "log"
    "os"

    "github.com/go-kratos/blades"
    "github.com/go-kratos/blades/contrib/openai"
    "github.com/go-kratos/blades/event"
)

func main() {
    provider := openai.NewChat("gpt-5",
        openai.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    )

    agent, err := blades.NewAgent(
        "assistant",
        blades.WithModel(provider),
        blades.WithInstruction("You are a concise, accurate assistant."),
    )
    if err != nil {
        log.Fatal(err)
    }

    result, err := blades.NewRunner(agent).Run(
        context.Background(),
        event.NewPrompt("What is the capital of France?"),
    )
    if err != nil {
        log.Fatal(err)
    }

    log.Println(result.Text())
}
```

## Agent Interface

Every runtime implements the same small interface:

```go
type Agent interface {
    Name() string
    Description() string
    Run(context.Context, <-chan event.Input) (<-chan event.Output, error)
}
```

`blades.NewAgent(name, opts...)` builds the default LLM-backed Agent. If you need a custom runtime, implement `Agent` directly and it can still be used by `Runner`, `flow/`, or `blades.NewAgentTool`.

## Runtime Building Blocks

| Package | Purpose |
| --- | --- |
| `content/` | Shared multimodal `Part` union for text, blobs, thinking, tool use, and tool results. |
| `event/` | User-facing input and output events for prompts, steering, aborts, streaming, tools, turn endings, errors, and completion. |
| `model/` | Provider-facing requests, messages, chunks, responses, usage, options, and the `model.Provider` interface. |
| `tools/` | Tool specs, execution interface, resolver/filter helpers, and tool context. |
| `session/` | Append-only model message history and context helpers. |
| `prompt/` | System prompt builders and ordered sections. |
| `compact/` | Context compaction strategies and model-backed summarization. |
| `policy/` | Tool invocation decisions such as allow, deny, ask, and modify. |
| `hook/` | Lifecycle callbacks around turns, model calls, and tool calls. |
| `flow/` | Agent composition primitives for sequential, parallel, loop, routing, and deep flows. |
| `contrib/` | Provider and integration modules for OpenAI, Anthropic, Gemini, MCP, and OpenTelemetry. |

## Providers

Providers implement `model.Provider`:

```go
type Provider interface {
    Name() string
    Generate(context.Context, *model.Request) (*model.Response, error)
    Stream(context.Context, *model.Request) iter.Seq2[*model.Chunk, error]
}
```

Provider options stay in the matching contrib module:

```go
provider := openai.NewChat("gpt-5",
    openai.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    openai.WithParallelToolCalls(true),
)
```

Tool concurrency is driven by model output. If a provider returns multiple `content.ToolUse` parts in one assistant message, the Agent loop executes that tool wave concurrently. To request at most one tool call per turn, configure the provider, for example `openai.WithParallelToolCalls(false)` or `anthropic.WithParallelToolCalls(false)`.

## Tools And Agents

Tools expose a schema and a handler:

```go
type Tool interface {
    Spec() tools.ToolSpec
    Handle(context.Context, json.RawMessage) (*tools.Result, error)
}
```

Attach tools to an Agent with `blades.WithTools(...)`, resolve them dynamically with `blades.WithToolsResolver(...)`, and guard them with `blades.WithPolicy(...)`. Any Agent can also become a tool:

```go
researcher, _ := blades.NewAgent("researcher", blades.WithModel(provider))
writer, _ := blades.NewAgent(
    "writer",
    blades.WithModel(provider),
    blades.WithTools(blades.NewAgentTool(researcher)),
)
_ = writer
```

## Streaming

Use `Runner.RunStream` when the application needs incremental output:

```go
out, err := blades.NewRunner(agent).RunStream(ctx, event.NewPrompt("Write a haiku."))
if err != nil {
    return err
}
for e := range out {
    switch v := e.(type) {
    case event.TextDelta:
        fmt.Print(v.Text)
    case event.Error:
        return v.Err
    }
}
```

Use `Runner.RunLive` when the application needs to send additional input, such as steering or abort events, while the Agent is running.

## Documentation

- [Documentation index](./docs/INDEX.md)
- [Event system and Agent loop](./docs/design-event-agent-loop.md)
- [Model and Provider protocol](./docs/design-model-provider.md)
- [Tool system](./docs/design-tool-system.md)
- [Agent composition and orchestration](./docs/design-agent-orchestration.md)
- [Development guide](./docs/DEVELOPMENT_GUIDE.md)

## Development

The root `Makefile` discovers all Go modules and runs checks per module:

```sh
make tidy
make build
make test
make all
```

For targeted iteration:

```sh
go test -race ./event
go test -race ./tests/agent
(cd contrib/openai && go test -race ./...)
```

## License

Blades is licensed under the MIT License. See [LICENSE](LICENSE).
