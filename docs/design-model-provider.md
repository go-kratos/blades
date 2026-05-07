---
type: design
title: Model Provider 协议设计
date: 2026-05-07
status: draft
parent: design-agent-framework.md
related: [design-agent-framework.md]
tags: [agentos, model, provider, protocol]
---

# Model Provider 协议设计

## 1. 概述

`model/` 是 AgentOS 面向 LLM provider 的协议叶子包。它只定义请求、响应、消息、工具调用与 token 计数协议，不依赖 `event/`、根包 Agent runtime 或应用层运行时。

设计上区分两个面向：

- **Event** 面向用户与上层框架，承载状态、生命周期、可观测信息。
- **Message** 面向 provider 协议，仅承载可重放的协议内容。

二者通过 `content.Part` 共享通用模态叶子，但顶层结构相互独立，由 Loop 在 `internal/convert/` 边界互转。

本文档相比上一版主要变化：

- Provider 接口由 *stream-only* 调整为 **`Generate` + `Stream` + `Count`** 三方法并列，与各 provider 原生 SDK 直接对齐，同步路径无需经 `Collect` 累加。
- `Response` 拆分为两个类型：**`Response`（同步终态）** 与 **`Chunk`（流式增量帧）**，避免一种类型承担两种语义。
- `Message` 收敛为 **protocol-only**（仅 `Role` + `Parts`），`Status` / `FinishReason` / `TokenUsage` 等运行时字段移到 `Response`/`Chunk` 上。
- `Chunk` 复用 `content.Part`：流式增量直接是 `[]content.Part`，不引入独立的 *Delta* 变体。

## 2. 设计目标与原则

1. **协议最小化**：`model/` 只定义 wire-equivalent 协议；运行态、调度、重试、限速一律不入侵协议字段。
2. **同步与流式语义一致**：`Generate` 的结果应等价于把 `Stream` 全部 `Chunk` 经 `model.Collect` 合成出的 `Response`。
3. **provider-agnostic**：字段尽量是各家 provider 的最小公共集合；私有字段由 adapter 内部处理。
4. **复用 `content.Part`**：通用模态与协议模态共享同一 sealed union，避免在 `model/` 重复定义 Part 体系。
5. **ctx-only 资源管理**：不提供 Close 方法；取消、deadline、连接释放统一通过 `context.Context` 表达。
6. **可演进**：协议接口稳定，具体能力（Embed、Rerank、Vision、Audio）以平级独立接口扩展。

## 3. Provider 接口

```go
package model

type Provider interface {
    // Name returns the provider/model identifier, e.g. "openai/gpt-4o".
    Name() string

    // Generate executes a request and returns the terminal Response in one shot.
    Generate(ctx context.Context, req *Request) (*Response, error)

    // Stream executes a request and yields incremental Chunks until the
    // provider terminates the response.
    Stream(ctx context.Context, req *Request) iter.Seq2[*Chunk, error]

    // Count returns the number of input tokens the request would consume.
    // Token counting is provider-coupled (model id, encoder, tool schema,
    // system blocks), so it lives on Provider rather than as a free helper.
    Count(ctx context.Context, req *Request) (int, error)
}
```

设计约束：

- `Generate` 与 `Stream` 平级独立，调用方按需选择。两者对同一 `Request` 的最终态语义必须等价。
- `Stream` 返回 `iter.Seq2[*Chunk, error]`，避免 `model/` 反向依赖根包；调用方用 `for chunk, err := range provider.Stream(ctx, req)` 消费。
- `Count` 合并在 `Provider` 内，因为 token 计数与模型、编码器、工具 schema 与 system block 强耦合，独立 helper 难以保证一致性。
- 资源释放依赖 `ctx` 取消、deadline 与 provider 内部 `defer`，不提供额外关闭方法。
- `Request` 不包含流式开关，是否流式由调用方法决定。

Embedding 与聊天生成平级独立：

```go
type EmbeddingProvider interface {
    Name() string
    Embed(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error)
}
```

后续如需 Rerank / Vision / Audio / Realtime，按同样形态新增独立接口，不混入 `Provider`。

## 4. Request

`Request` 是一次 provider 调用的完整输入：

```go
type Request struct {
    Model    string         // model identifier, may differ from Provider.Name()
    System   []*SystemBlock // top-level system content with optional cache control
    Messages []*Message     // conversation history (user / assistant / tool)
    Tools    []ToolSpec     // tool schemas exposed to the model
}

type SystemBlock struct {
    Text         string
    CacheControl CacheControl
}
```

要点：

- `System` 是顶层独立字段，不通过消息角色表达。adapter 可直接映射到 Anthropic system blocks、OpenAI instructions / system role 或其他等价字段，并独立处理缓存控制。
- `Messages` 仅承载 user / assistant / tool 三角色（参见 §6）。
- `Tools` 描述工具 schema，工具调用与结果通过 `Message.Parts` 中的 `content.ToolUse` / `content.ToolResult` 表达。
- `Request` 不含 stream/temperature/max_tokens 等参数：模型采样参数由 adapter 在构造时通过自身配置注入；协议层只表达"请求内容"。

## 5. Response 与 Chunk

同步与流式分别返回两个类型，类型即语义：

```go
// Response is the terminal result returned by Provider.Generate.
type Response struct {
    Message    *Message   // assistant message, fully populated
    StopReason StopReason // why generation stopped
    Usage      Usage      // token accounting
}

// Chunk is one incremental frame yielded by Provider.Stream.
type Chunk struct {
    // Parts carries incremental content for this frame.
    // Reuses content.Part: text deltas are content.Text fragments,
    // tool_use deltas accumulate by ToolUse.ID, thinking deltas the same.
    Parts []content.Part

    // StopReason is empty until the terminal chunk.
    StopReason StopReason

    // Usage is non-nil when the provider reports usage; typically only
    // on the terminal chunk, but some providers emit interim numbers.
    Usage *Usage
}

type StopReason string

const (
    StopEnd       StopReason = "end_turn"
    StopToolUse   StopReason = "tool_use"
    StopMaxTokens StopReason = "max_tokens"
    StopSafety    StopReason = "safety"
)

type Usage struct {
    InputTokens  int
    OutputTokens int
    TotalTokens  int
}
```

设计要点：

- **拆分而非复用**：`Response` 与 `Chunk` 用不同类型表达"终态"与"增量"两种语义，调用方在编译期即可区分，避免运行时判空 `Message != nil` 才能识别终态。
- **复用 `content.Part`**：流式增量不引入 `TextDelta` / `ToolUseDelta` 等变体；text 增量就是 `content.Text{Text: "片段"}`，tool_use 增量按 `ToolUse.ID` 在多帧累加。
- **Chunk 不含 Role**：Stream 默认 assistant 角色；多 candidate / 多 turn 暂不在协议层表达，由上层组合。
- **不完整或停止状态不放在 `Message` 上**：由 `Response.StopReason` / `Chunk.StopReason` 表达。

合成 helper：

```go
// Collect drains a stream and synthesizes a terminal Response by
// concatenating Chunk.Parts (text by index, tool_use by ID) and adopting
// the last non-empty StopReason / Usage.
func Collect(seq iter.Seq2[*Chunk, error]) (*Response, error)
```

`Collect` 用于测试、兜底以及 stream-only adapter 实现 `Generate`；正常路径下 `Generate` 直接调用 provider 的同步 API，避免无谓累加。

## 6. Message

`Message` 是 provider 历史消息单元，**只承载协议内容**：

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

约束：

- 仅保留三种角色：用户、助手、工具。系统内容统一走 `Request.System`。
- Message **不携带运行状态**：没有 `Status` / `FinishReason` / `TokenUsage` / `Actions` / `Metadata`。这些信息：
  - 生成态归 `Response` / `Chunk`；
  - 业务态（author、invocationId、metadata）归 `event/` 与上层 session；
  - compact 与 session 重放只需要 protocol-only 的 Message。
- Message 历史只保存 provider 可重放的协议内容。Loop / session / compact 必须维护 provider message invariant，避免生成无法被 adapter 发送的历史。

## 7. Part

`Message.Parts` 与 `Chunk.Parts` 直接使用 `content.Part`：通用模态变体（`Text` / `Blob` / `Thinking`）与协议变体（`ToolUse` / `ToolResult`）都在 `content/` 同一 sealed union 内，`model/` 不再定义独立 Part 类型。

```go
msg := &model.Message{
    Role: model.RoleAssistant,
    Parts: []content.Part{
        content.Text{Text: "let me look that up"},
        content.ToolUse{ID: "call_1", Name: "search", Input: `{"q":"go iter"}`},
    },
}
```

工具调用与工具结果作为 `content.ToolUse` / `content.ToolResult` 出现在 `Message.Parts` 中，由 provider adapter 在协议映射时解释。

## 8. 同步与流式语义对照

| 字段 | `Generate → Response` | `Stream → Chunk` 序列 |
|------|----------------------|----------------------|
| `Message` | 终态完整填充 | 由 `Collect` 拼接 `Chunk.Parts` 而成 |
| `Parts` 内 text | 单个 `content.Text` | 多个 `content.Text` 片段，按顺序拼接 |
| `Parts` 内 `ToolUse` | 完整 input JSON | 多帧 input 增量，按 `ID` 合并 |
| `StopReason` | `Response.StopReason` | 终止帧 `Chunk.StopReason` |
| `Usage` | `Response.Usage` | 任一帧 `Chunk.Usage`（通常终止帧） |

不变式：对同一 `Request`，`Generate(req)` 与 `Collect(Stream(req))` 的输出在 `Message.Parts`、`StopReason`、`Usage` 上等价（允许 part 切片粒度不同，但拼接后内容一致）。

## 9. 资源管理

Provider 不提供关闭方法。资源管理遵循 Go context：

- 调用方通过 `ctx` 传递取消和 deadline。
- adapter 在 `Stream` / `Generate` 内监听 `ctx.Done()`。
- HTTP body、SSE 连接、goroutine 与内部缓冲由 adapter 使用 `defer` 释放。
- 调用方停止消费 generator 时应取消 ctx，确保底层连接退出。

```go
ctx, cancel := context.WithCancel(parent)
defer cancel()

for chunk, err := range provider.Stream(ctx, req) {
    if err != nil {
        return err
    }
    if shouldStop(chunk) {
        cancel()
        break
    }
}
```

## 10. Adapter 映射示例

### 10.1 OpenAI Chat Completions

| 协议字段 | OpenAI 映射 |
|---------|------------|
| `Request.System` | 首个 `messages[role=system]`（或 `instructions`） |
| `Request.Messages` | `messages[]`，role ∈ user/assistant/tool |
| `Message.Parts` 中 `content.Text` | `messages[].content` 文本 |
| `Message.Parts` 中 `content.ToolUse` | assistant 消息的 `tool_calls[]` |
| `Message.Parts` 中 `content.ToolResult` | role=tool 消息的 `content` |
| `Request.Tools` | `tools[]` |
| `Generate` | `POST /chat/completions`（非流） |
| `Stream` | `POST /chat/completions` SSE，每个 chunk 的 `choices[].delta` 转 `Chunk.Parts` |
| `StopReason` | `finish_reason` 映射（stop→`StopEnd`、tool_calls→`StopToolUse`、length→`StopMaxTokens`） |
| `Usage` | 终止 chunk 或响应 `usage` 字段 |

text 增量：`delta.content` → `content.Text{Text: chunk}`。tool_call 增量：按 `tool_calls[].index` / `id` 累加 `arguments`。

### 10.2 Anthropic Messages

| 协议字段 | Anthropic 映射 |
|---------|---------------|
| `Request.System` | `system: [{type:"text", text, cache_control}]` |
| `Request.Messages` | `messages[]`，role ∈ user/assistant |
| `Message.Parts` 中 `content.ToolUse` | content block `tool_use` |
| `Message.Parts` 中 `content.ToolResult` | user 消息的 `tool_result` content block |
| `Request.Tools` | `tools[]` |
| `Generate` | `POST /v1/messages`（非流） |
| `Stream` | SSE 事件流：`content_block_start` / `content_block_delta` / `content_block_stop` / `message_delta` |
| `StopReason` | `message_delta.stop_reason`（end_turn / tool_use / max_tokens） |
| `Usage` | `message_start.usage` + `message_delta.usage`（增量补充） |

`content_block_delta` 中的 `text_delta` → `content.Text{Text: delta}`；`input_json_delta` 按 block index 累加到对应 `content.ToolUse.Input`。

## 11. 实现职责边界

`model/` 只定义协议和最小 helper（`Collect`）。以下能力由 provider adapter 或上层组合实现，不进入协议字段：

- 网络重试、限速、熔断与 provider fallback。
- token 编码器加载与缓存。
- 请求签名、鉴权与区域选择。
- 采样参数（temperature / top_p / max_tokens）注入。
- provider 私有字段映射（response_format / reasoning_effort / 自定义 header）。

这样 `model/` 保持稳定，具体 provider 可在 `contrib/<provider>` 中独立演进。

## 与红线对照

本文覆盖 r2、r13、r14、r15。
