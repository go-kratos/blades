<p align="center">
    <a href="https://github.com/go-kratos/blades/actions"><img src="https://github.com/go-kratos/blades/workflows/Go/badge.svg" alt="Build Status"></a>
    <a href="https://pkg.go.dev/github.com/go-kratos/blades"><img src="https://pkg.go.dev/badge/github.com/go-kratos/blades" alt="GoDoc"></a>
    <a href="https://deepwiki.com/go-kratos/blades"><img src="https://deepwiki.com/badge.svg" alt="DeepWiki"></a>
    <a href="https://github.com/go-kratos/blades/blob/main/LICENSE"><img src="https://img.shields.io/github/license/go-kratos/blades" alt="License"></a>
</p>

## Blades

Blades 是一个 Go 语言的多模态 AI Agent 框架。核心提供事件驱动的 Agent 接口、模型 Provider 适配、工具、会话、Prompt 构建、上下文压缩、策略校验、Hook 和 Flow 编排。

## 核心概念

- **Agent**：可执行单元，`Run` 消费 `event.Input`，输出 `event.Output`。
- **Event**：面向用户和应用层的协议，承载 prompt、steer、abort、流式增量、工具生命周期、turn 结束和运行期错误。
- **Model Provider**：定义在 `model/` 的模型协议，由 `contrib/openai`、`contrib/anthropic`、`contrib/gemini` 等适配器实现。
- **Content Part**：定义在 `content/` 的统一多模态叶子类型，被 Event、模型消息和工具结果共享。
- **Tool**：由 `tools.ToolSpec` 描述、由 `tools.Tool` 执行的外部能力。
- **Session**：Agent Loop 使用的 append-only 模型消息历史。
- **Prompt / Compact / Policy / Hook**：分别负责 system prompt 构建、上下文压缩、工具 guardrail 和可观测扩展。

## Agent 接口

```go
type Agent interface {
    Name() string
    Description() string
    Run(context.Context, <-chan event.Input) (<-chan event.Output, error)
}
```

`blades.NewAgent(name, opts...)` 构建默认 LLM Agent。需要完全不同运行时的时候，直接实现同一个接口即可。

## Model Provider 接口

```go
type Provider interface {
    Name() string
    Generate(context.Context, *model.Request) (*model.Response, error)
    Stream(context.Context, *model.Request) iter.Seq2[*model.Chunk, error]
}
```

Provider 构造函数使用 Functional Options：

```go
model := openai.NewModel("gpt-5",
    openai.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    openai.WithParallelToolCalls(true),
)
```

工具并发由模型返回结果驱动。若 provider 在同一个 assistant message 中返回多个 `content.ToolUse`，Agent Loop 会把这一批 tool wave 并发执行。若希望模型一次最多返回一个工具调用，应在 provider 构造时配置，例如 `openai.WithParallelToolCalls(false)` 或 `anthropic.WithParallelToolCalls(false)`。

## 快速开始

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
    provider := openai.NewModel("gpt-5",
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
        event.NewPromptText("What is the capital of France?"),
    )
    if err != nil {
        log.Fatal(err)
    }

    log.Println(result.Text())
}
```

## 文档

设计文档位于 [docs](./docs)。Agent Loop 见 [docs/design-event-agent-loop.md](./docs/design-event-agent-loop.md)，模型协议见 [docs/design-model-provider.md](./docs/design-model-provider.md)。

## 许可证

Blades 使用 MIT 许可证，详见 [LICENSE](LICENSE)。
