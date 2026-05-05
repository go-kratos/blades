---
type: design
title: Event 协议与 Agent Loop 设计
date: 2026-05-05
status: draft
parent: design-agent-framework.md
related: [design-agent-framework.md]
tags: [agentos, event, loop, content, protocol]
---

# Event 协议与 Agent Loop 设计

## 1. 概述

`event/` 是 AgentOS 面向用户、应用接入、hook 与运行时的协议层。它只表达输入、输出、控制、通知、工具生命周期和流式输出，不承载 provider 消息约束。

Agent Loop 的默认实现位于公开 `loop/` 包。Loop 使用顺序代码表达一次运行过程，不导出显式状态枚举；可观测点通过行为事件 hook 暴露。Event 与 `model.Message` 不合并，转换边界集中在 `internal/convert/`。

## 2. `content.Part` 共享叶子

通用模态定义在 `content/` 包，仅依赖 Go 标准库。`content.Part` 是 sealed marker，使用私有 `part()` 方法收口扩展点，所有变体都在 `content/` 内定义。

```go
package content

type Part interface{ part() }

// 用户多模态变体
type Text struct {
    Text string
}

type Blob struct {
    MIME   string
    Source BlobSource
}

type Thinking struct {
    Text      string
    Signature []byte
}

// Provider/工具协议变体
type ToolUse struct {
    ID    string
    Name  string
    Input json.RawMessage
}

type ToolResult struct {
    ID      string
    Name    string
    Parts   []Part
    IsError bool
}
```

`Blob.Source` 同样是 sealed union，由三种命名类型表达二进制来源：

```go
type BlobSource interface{ blobSource() }

type InlineBytes []byte
type URI string
type FileID string
```

`content/` 不提供统一元数据字段，也不提供解析或读取字节的便捷方法。业务扩展在应用层结构中表达，二进制拉取、权限校验、缓存与传输由上层处理。`ToolUse` 与 `ToolResult` 直接进入 `content.Part` 单一 union；`event` / `model` / `tools` 三个包都使用 `content.Part`，不再各自定义 Part 类型。

## 3. Event Input 协议

`event.Input` 是 sealed marker，使用私有 `input()` 方法收口扩展点。Loop 的 type switch 穷尽内置变体，无需 `default` 分支。

```go
package event

type Input interface{ input() }
```

内置输入事件：

```go
type Prompt struct {
    Parts []content.Part
}

type Steer struct {
    Parts []content.Part
}

type Abort struct {
    Reason string
}

type Pause struct{}
type Resume struct{}
```

`Prompt` 用于发起一个新 turn；`Steer` 用于运行中追加修正、上下文或继续指令。二者是独立类型，但多模态字段都直接使用 `[]content.Part`。

文本构造只提供函数糖：

```go
func NewPromptText(s string) Prompt {
    return Prompt{Parts: []content.Part{content.Text{Text: s}}}
}

func NewSteerText(s string) Steer {
    return Steer{Parts: []content.Part{content.Text{Text: s}}}
}
```

Control 使用独立类型而非枚举：

- `Abort{Reason string}`：请求中止并携带人类可读原因。
- `Pause{}`：请求暂停可暂停的运行段。
- `Resume{}`：请求恢复已暂停的运行段。

`Abort` 与 `context.CancelFunc` 互补：前者进入协议流，后者负责取消调用栈与资源等待。

## 4. Event Output 协议

`event.Output` 同样是 sealed marker：

```go
type Output interface{ output() }
```

### 4.1 流式内容输出

常用文本和思考增量走 hot path 紧凑值类型，避免在高频路径上使用额外接口包装：

```go
type TextDelta struct {
    Text string
}

type ThinkingDelta struct {
    Text      string
    Signature []byte
}
```

其他模态走 cold path 多模态生命周期事件：

```go
type PartStart struct {
    Index int
    Part  content.Part
}

type PartDelta struct {
    Index int
    Part  content.Part
}

type PartEnd struct {
    Index int
    Part  content.Part
}
```

两条路径不重叠：Loop 按 part 模态分发。文本与思考增量使用 `TextDelta` / `ThinkingDelta`；Blob 等多模态内容使用 `PartStart` / `PartDelta` / `PartEnd`。

### 4.2 工具生命周期输出

工具执行由 Loop 编排并以事件形式公开：

```go
type ToolStart struct {
    ID   string
    Name string
}

type ToolDelta struct {
    ID    string
    Parts []content.Part
}

type ToolEnd struct {
    ID     string
    Name   string
    Result *tools.Result
    Err    error
}
```

`ToolEnd.Result.Parts` 直接复用 `tools.Result{Parts []content.Part}`。当工具返回 error 时，Loop 负责把错误落入工具结束事件，并在转换到 provider 消息时标记工具结果为错误语义。

### 4.3 Turn 结束、生命周期结束与错误

单 turn 结束和整个 Run 结束是两个事件：

```go
type Usage struct {
    InputTokens  int
    OutputTokens int
    TotalTokens  int
}

type TurnEnd struct {
    Parts []content.Part
    Usage Usage
}

type Done struct{}
```

`TurnEnd` 表示一次模型 turn 完成，携带最终内容与 token 用量。`Done` 是 channel sentinel，表示 Agent 生命周期结束，通常在关闭输出流前发送。

运行期错误作为输出事件进入同一条流：

```go
type Error struct {
    Err error
}
```

`Error` 不携带协议级错误码。错误分类使用 Go 标准 `errors.Is` / `errors.As` 与 `event` 包内 sentinel。无法作为普通输出恢复的 fatal 错误通过 `loop.Run` 返回的 generator yield error 表达。

## 5. Agent Loop 顺序流程

默认 Loop 以顺序代码组织一次运行，不暴露状态枚举。推荐流程：

1. 从输入 channel 读取 `Prompt` 或 `Steer`，开始一个 turn。
2. 触发 `TurnStart` hook。
3. 通过 `loop.Builder` 构建 `*model.Request`。
4. 触发 `PreModelCall` hook。
5. 调用 `model.Provider.Stream(ctx, req)`。
6. 消费 provider 响应，将文本、思考、多模态 part 和工具请求转换为 `event.Output`。
7. 触发 `PostModelCall` hook。
8. 如存在工具请求，触发 `PreToolCall`，交给 `loop.Orchestrator` 执行，再触发 `PostToolCall`。
9. 工具结果转换为 `event.ToolEnd` 与下一轮 `model.ToolResultPart`。
10. 输出 `TurnEnd`，触发 `TurnEnd` hook。
11. 输入结束或上下文取消后输出 `Done`。

Hook 名称固定为行为事件：`PreModelCall`、`PostModelCall`、`PreToolCall`、`PostToolCall`、`TurnStart`、`TurnEnd`。这些事件描述发生了什么，而不是暴露 Loop 内部状态。

## 7. Event ↔ Message 转换边界

Event 面向用户协议，Message 面向 provider 协议。二者通过 `content.Part` 共享模态叶子，但不共享顶层结构。

唯一转换边界在 `internal/convert/`：

- `event.Prompt` / `event.Steer` 转为 `model.Message{Role: model.RoleUser, Parts: ...}`。
- provider 文本响应转为 `event.TextDelta`。
- provider 思考响应转为 `event.ThinkingDelta`。
- provider 多模态响应转为 `event.PartStart` / `event.PartDelta` / `event.PartEnd`。
- provider 工具请求转为工具生命周期输出。
- `tools.Result.Parts` 包装为 `model.ToolResultPart` 并复用同一 `[]content.Part`。

用户代码不应直接依赖 `internal/convert/`。需要改变构建或工具编排时，应替换 `loop.Builder` 或 `loop.Orchestrator`。

## 8. Loop 公开 API

`loop/` 暴露三件套：`Run`、`Builder`、`Orchestrator`。

```go
package loop

func Run(
    ctx context.Context,
    agent blades.Agent,
    input <-chan event.Input,
) blades.Generator[event.Output, error]

type Builder interface {
    Build(ctx context.Context, in BuildInput) (*model.Request, error)
}

type Orchestrator interface {
    Run(ctx context.Context, uses []model.ToolUsePart) ([]event.ToolEnd, error)
}
```

`Run` 不在函数返回值上返回 error；fatal 错误通过 generator 的第二个 yield 值表达。`Builder` 负责把 session、prompt、tool spec、compact 等运行时材料组装为 `model.Request`。`Orchestrator` 只返回工具结束事件，不返回 provider message；消息转换仍归 Loop 边界处理。

## 与红线对照

本文覆盖 r1、r3、r5、r6、r7、r8、r9、r10、r11、r12、r25、r30、r31、r32。