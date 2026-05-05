---
type: design
title: Model Provider 协议设计
date: 2026-05-05
status: draft
parent: design-agent-framework.md
related: [design-agent-framework.md]
tags: [agentos, model, provider, protocol]
---

# Model Provider 协议设计

## 1. 概述

`model/` 是 AgentOS 面向 LLM provider 的协议叶子包。它定义请求、响应、消息、工具调用和 token 计数接口，不依赖 `event/`、根包 Agent runtime 或应用层运行时。

Event 面向用户协议，Message 面向 provider 协议。二者通过 `content.Part` 共享通用模态叶子，但顶层结构保持独立，由 Loop 在 `internal/convert/` 边界转换。

## 2. Provider 接口

v1 Provider 使用流式优先接口：

```go
package model

type Provider interface {
    Name() string
    Stream(ctx context.Context, req *Request) iter.Seq2[*Response, error]
    Count(ctx context.Context, req *Request) (int, error)
}
```

设计约束：

- `Stream` 是唯一生成入口；同步收集由 `model.Collect` helper 基于流式响应累加得到。
- `Count` 合并在 Provider 内，因为 token 计算与模型、编码器、工具 schema 和 system block 强耦合。
- `Stream` 返回 `iter.Seq2[*Response, error]` 风格序列，避免 `model/` 依赖根包。
- 资源释放依赖 `ctx` 取消、deadline 和 provider 内部 defer，不提供额外关闭方法。

Embedding 与聊天生成平级独立：

```go
type EmbeddingProvider interface {
    Name() string
    Embed(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error)
}
```

## 3. Request

`Request` 是一次 provider 调用的完整输入：

```go
type Request struct {
    Model    string
    System   []*SystemBlock
    Messages []*Message
    Tools    []ToolSpec
}

type SystemBlock struct {
    Text         string
    CacheControl CacheControl
}
```

`System` 是顶层独立字段，不通过消息角色表达。这样 provider adapter 可以直接映射到 Anthropic system blocks、OpenAI instructions 或其他等价字段，并独立处理缓存控制。

`Request` 不包含流式开关。是否流式由调用的方法决定：生成使用 `Provider.Stream`，计数使用 `Provider.Count`。

## 4. Response

`Response` 表达 provider 流式返回的一帧或最终帧：

```go
type Response struct {
    Delta      []content.Part
    StopReason StopReason
    Usage      Usage
}

type StopReason string

type Usage struct {
    InputTokens  int
    OutputTokens int
    TotalTokens  int
}
```

`Delta` 承载本帧新增的 provider 协议 part。`StopReason` 承载模型停止原因，例如自然结束、工具调用、长度限制或安全停止。`Usage` 可在最后一帧给出，也可按 provider 能力逐步补充。

不完整或停止状态不放在 `Message` 上，而由 `Response.StopReason` 表达。

## 5. Message

`Message` 是 provider 历史消息单元：

```go
type Message struct {
    Role  Role
    Parts []content.Part
}

type Role string

const (
    RoleUser      Role = "user"
    RoleAssistant Role = "assistant"
    RoleTool      Role = "tool"
)
```

仅保留三种角色：用户、助手、工具。系统内容使用 `Request.System` 顶层字段表达。

Message 不携带运行状态；消息历史只保存 provider 可重放的协议内容。Loop、session 和 compact 必须维护 provider message invariant，避免生成无法被 adapter 发送的历史。

## 6. Part

`Message.Parts` 直接使用 `content.Part`：通用模态变体（`Text` / `Blob` / `Thinking`）与 provider 协议变体（`ToolUse` / `ToolResult`）都在 `content/` 同一 sealed union 内，`model/` 不再定义独立 Part 类型。

```go
msg := &model.Message{
    Role: model.RoleUser,
    Parts: []content.Part{
        content.Text{Text: "hello"},
    },
}
```

工具调用与工具结果作为 `content.ToolUse` / `content.ToolResult` 出现在 `Message.Parts` 中，由 provider adapter 在协议映射时解释。

## 7. Provider 资源管理

Provider 不提供关闭方法。资源管理遵循 Go context：

- 调用方通过 `ctx` 传递取消和 deadline。
- provider adapter 在 `Stream` 内监听 `ctx.Done()`。
- HTTP body、SSE 连接、goroutine 和内部缓冲由 adapter 使用 defer 释放。
- 调用方停止消费 generator 时应取消 ctx，确保底层连接退出。

示例：

```go
ctx, cancel := context.WithCancel(parent)
defer cancel()

for resp, err := range provider.Stream(ctx, req) {
    if err != nil {
        return err
    }
    if shouldStop(resp) {
        cancel()
        break
    }
}
```

## 8. 实现职责边界

`model/` 只定义协议和最小 helper。以下能力由 provider adapter 或上层组合实现，不进入协议字段：

- 网络重试、限速、熔断和 provider fallback。
- token 编码器加载与缓存。
- 请求签名、鉴权和区域选择。
- provider 私有字段映射。
- 响应收集：由 `model.Collect` 基于 `Stream` 实现。

这样 `model/` 保持稳定，具体 provider 可在 `contrib/<provider>` 中演进实现细节。

## 与红线对照

本文覆盖 r2、r13、r14、r15。
