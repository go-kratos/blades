<p align="center">
    <a href="https://github.com/go-kratos/blades/actions"><img src="https://github.com/go-kratos/blades/workflows/Go/badge.svg" alt="Build Status"></a>
    <a href="https://pkg.go.dev/github.com/go-kratos/blades"><img src="https://pkg.go.dev/badge/github.com/go-kratos/blades" alt="GoDoc"></a>
    <a href="https://deepwiki.com/go-kratos/blades"><img src="https://deepwiki.com/badge.svg" alt="DeepWiki"></a>
    <a href="https://github.com/go-kratos/blades/blob/main/LICENSE"><img src="https://img.shields.io/github/license/go-kratos/blades" alt="License"></a>
</p>

## Blades

Blades is a multimodal AI Agent framework for Go. It provides a small event-driven Agent interface, model provider adapters, tools, session state, prompt building, context compaction, policy checks, hooks, and flow composition.

## Core Concepts

- **Agent**: the executable unit. `Run` consumes `event.Input` and emits `event.Output`.
- **Event**: the user-facing protocol for prompts, steering, aborts, streaming deltas, tool lifecycle events, turn completion, and runtime errors.
- **Model Provider**: the provider-facing protocol in `model/`, implemented by adapters such as `contrib/openai`, `contrib/anthropic`, and `contrib/gemini`.
- **Content Part**: the shared multimodal leaf type in `content/`, used by events, model messages, and tool results.
- **Tool**: external capability described by `tools.ToolSpec` and executed by `tools.Tool`.
- **Session**: append-only model message history used by the Agent Loop.
- **Prompt / Compact / Policy / Hook**: focused extension points for system prompt construction, context compression, tool guardrails, and observability.

## Agent Interface

```go
type Agent interface {
    Name() string
    Description() string
    Run(context.Context, <-chan event.Input) (<-chan event.Output, error)
}
```

`blades.NewAgent(name, opts...)` builds the default LLM-backed Agent. A custom runtime can implement the same interface directly.

## Model Provider Interface

```go
type Provider interface {
    Name() string
    Generate(context.Context, *model.Request) (*model.Response, error)
    Stream(context.Context, *model.Request) iter.Seq2[*model.Chunk, error]
}
```

Provider constructors use functional options:

```go
model := openai.NewModel("gpt-5",
    openai.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    openai.WithParallelToolCalls(true),
)
```

Tool concurrency is model-driven. If a provider returns multiple `content.ToolUse` parts in one assistant message, the Agent Loop executes that tool wave concurrently. To ask the model to emit at most one tool call per turn, configure the provider, for example `openai.WithParallelToolCalls(false)` or `anthropic.WithParallelToolCalls(false)`.

## Quick Start

```go
package main

import (
    "context"
    "log"
    "os"

    "github.com/go-kratos/blades"
    "github.com/go-kratos/blades/content"
    "github.com/go-kratos/blades/contrib/openai"
    "github.com/go-kratos/blades/event"
    "github.com/go-kratos/blades/prompt"
)

func main() {
    provider := openai.NewModel("gpt-5",
        openai.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    )

    agent, err := blades.NewAgent(
        "assistant",
        blades.WithModel(provider),
        blades.WithPrompt(prompt.New(prompt.System("You are a concise, accurate assistant."))),
    )
    if err != nil {
        log.Fatal(err)
    }

    out, err := blades.NewRunner(agent).Run(
        context.Background(),
        event.NewPromptText("What is the capital of France?"),
    )
    if err != nil {
        log.Fatal(err)
    }

    if turn, ok := out.(event.TurnEnd); ok {
        log.Println(textFromParts(turn.Parts))
    }
}

func textFromParts(parts []content.Part) string {
    var text string
    for _, part := range parts {
        if p, ok := part.(content.Text); ok {
            text += p.Text
        }
    }
    return text
}
```

## Documentation

Design notes live in [docs](./docs). The Agent Loop design is described in [docs/design-event-agent-loop.md](./docs/design-event-agent-loop.md), and the model protocol is described in [docs/design-model-provider.md](./docs/design-model-provider.md).

## License

Blades is licensed under the MIT License. See [LICENSE](LICENSE).
