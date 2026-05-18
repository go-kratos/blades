<p align="center">
    <a href="https://github.com/go-kratos/blades/actions"><img src="https://github.com/go-kratos/blades/workflows/Go/badge.svg" alt="Build Status"></a>
    <a href="https://pkg.go.dev/github.com/go-kratos/blades"><img src="https://pkg.go.dev/badge/github.com/go-kratos/blades" alt="GoDoc"></a>
    <a href="https://deepwiki.com/go-kratos/blades"><img src="https://deepwiki.com/badge.svg" alt="DeepWiki"></a>
    <a href="https://github.com/go-kratos/blades/blob/main/LICENSE"><img src="https://img.shields.io/github/license/go-kratos/blades" alt="License"></a>
</p>

# Blades

[English](./README.md)

Blades 是一个 Go 语言的事件驱动多模态 Agent 框架。它提供小而稳定的公开 Agent 接口、与模型厂商解耦的模型协议、工具执行、会话历史、Prompt 构建、上下文压缩、策略校验、生命周期 Hook，以及多 Agent Flow 编排。

Blades 适合把 LLM Agent 当作普通 Go 组件嵌入应用：输入输出显式、运行时可取消、Provider 适配停留在边界层，核心协议保持轻量，扩展点也各自收敛在独立包里。

## 为什么选择 Blades

- **事件优先的运行时**：应用通过 `event.Input` 和 `event.Output` 与 Agent 交互，覆盖流式文本、工具生命周期、turn 结束、错误和 `Done` 等事件。
- **Provider 无关的核心**：OpenAI、Anthropic、Gemini、MCP 和可观测集成都放在 `contrib/`，根模块不依赖任何模型厂商 SDK。
- **统一多模态协议**：`content.Part` 是 Event、模型消息和工具结果共享的内容 union。
- **内置工具循环**：工具由 `tools.ToolSpec` 描述、由 `tools.Tool` 执行，可经过 policy 校验、hook 观测，并写回 session 历史。
- **可组合的 Agent**：使用 `flow/` 做顺序、并行、循环、路由和 deep flow 编排，也可以用 `blades.NewAgentTool` 把任意 Agent 包装成工具。

## 安装

```sh
go get github.com/go-kratos/blades
go get github.com/go-kratos/blades/contrib/openai
```

每个 Provider 集成都是独立 Go module。按应用实际需要安装对应 contrib 模块：

```sh
go get github.com/go-kratos/blades/contrib/anthropic
go get github.com/go-kratos/blades/contrib/gemini
go get github.com/go-kratos/blades/contrib/mcp
```

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

## Agent 接口

所有运行时都实现同一个小接口：

```go
type Agent interface {
    Name() string
    Description() string
    Run(context.Context, <-chan event.Input) (<-chan event.Output, error)
}
```

`blades.NewAgent(name, opts...)` 会构建默认的 LLM Agent。如果需要完全自定义运行时，直接实现 `Agent` 即可继续被 `Runner`、`flow/` 或 `blades.NewAgentTool` 使用。

## 运行时模块

| 包 | 职责 |
| --- | --- |
| `content/` | 共享的多模态 `Part` union，覆盖 text、blob、thinking、tool use 和 tool result。 |
| `event/` | 面向用户和应用层的输入输出事件，覆盖 prompt、steer、abort、streaming、tool、turn end、error 和 done。 |
| `model/` | 面向 Provider 的 request、message、chunk、response、usage、option 和 `model.Provider` 接口。 |
| `tools/` | 工具 spec、执行接口、resolver/filter 辅助能力和 tool context。 |
| `session/` | append-only 模型消息历史和 context helper。 |
| `prompt/` | system prompt builder 和有序 section。 |
| `compact/` | 上下文压缩策略和模型驱动的 summary。 |
| `policy/` | 工具调用决策，例如 allow、deny、ask、modify。 |
| `hook/` | turn、model call、tool call 周围的生命周期回调。 |
| `flow/` | 顺序、并行、循环、路由和 deep flow 等 Agent 组合原语。 |
| `contrib/` | OpenAI、Anthropic、Gemini、MCP、OpenTelemetry 等 Provider 和集成模块。 |

## Provider

Provider 实现 `model.Provider`：

```go
type Provider interface {
    Name() string
    Generate(context.Context, *model.Request) (*model.Response, error)
    Stream(context.Context, *model.Request) iter.Seq2[*model.Chunk, error]
}
```

Provider 相关配置留在对应 contrib 模块里：

```go
provider := openai.NewChat("gpt-5",
    openai.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    openai.WithParallelToolCalls(true),
)
```

工具并发由模型输出驱动。如果 Provider 在同一个 assistant message 中返回多个 `content.ToolUse`，Agent loop 会并发执行这一批 tool wave。若希望模型每轮最多返回一个工具调用，可在 Provider 上配置，例如 `openai.WithParallelToolCalls(false)` 或 `anthropic.WithParallelToolCalls(false)`。

## 工具与 Agent

工具暴露 schema 和 handler：

```go
type Tool interface {
    Spec() tools.ToolSpec
    Handle(context.Context, json.RawMessage) (*tools.Result, error)
}
```

使用 `blades.WithTools(...)` 为 Agent 绑定静态工具，使用 `blades.WithToolsResolver(...)` 动态解析工具，使用 `blades.WithPolicy(...)` 给工具调用加策略约束。任意 Agent 也可以被包装成工具：

```go
researcher, _ := blades.NewAgent("researcher", blades.WithModel(provider))
writer, _ := blades.NewAgent(
    "writer",
    blades.WithModel(provider),
    blades.WithTools(blades.NewAgentTool(researcher)),
)
_ = writer
```

## 流式输出

应用需要增量输出时，可以使用 `Runner.RunStream`：

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

如果应用需要在 Agent 运行过程中继续发送 steer 或 abort 等输入，可以使用 `Runner.RunLive`。

## 文档

- [文档索引](./docs/INDEX.md)
- [Event 系统与 Agent Loop](./docs/design-event-agent-loop.md)
- [Model 与 Provider](./docs/design-model-provider.md)
- [工具系统](./docs/design-tool-system.md)
- [Agent 组合与编排](./docs/design-agent-orchestration.md)
- [开发规范](./docs/DEVELOPMENT_GUIDE.md)

## 开发

根目录 `Makefile` 会发现所有 Go module，并逐个运行检查：

```sh
make tidy
make build
make test
make all
```

针对性迭代可以直接跑包级测试：

```sh
go test -race ./event
go test -race ./tests/agent
(cd contrib/openai && go test -race ./...)
```

## 许可证

Blades 使用 MIT 许可证，详见 [LICENSE](LICENSE)。
