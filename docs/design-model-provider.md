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

- Provider 接口由 *stream-only* 调整为 **`Generate` + `Stream`** 双方法并列，与各 provider 原生 SDK 直接对齐，同步路径无需经 `Collect` 累加。
- token 计数从 `Provider` 抽离为独立的 **`TokenCounter` 接口**，按能力探测；返回 `Usage` 而非裸 `int`。
- `Response` 拆分为两个类型：**`Response`（同步终态）** 与 **`Chunk`（流式增量帧）**，避免一种类型承担两种语义。
- `Message` 收敛为 **protocol-only**（仅 `Role` + `Parts`），`Status` / `FinishReason` / `TokenUsage` 等运行时字段移到 `Response`/`Chunk` 上。
- `Chunk` 复用 `content.Part`：流式增量直接是 `[]content.Part`，不引入独立的 *Delta* 变体。
- `Request.System` 简化为 `string`（不再用 `[]*SystemBlock`）；引入 `Request.Options` sealed Option 列表承载 cache / reasoning / response_format / sampling / parallel tool calls 等 provider hints，adapter 选择性应用。

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
}

// TokenCounter estimates token usage for a request without invoking the model.
// It is split from Provider because counting is often offered by a separate
// endpoint (or pure local encoder) and not all providers support it.
type TokenCounter interface {
    Count(ctx context.Context, req *Request) (Usage, error)
}
```

设计约束：

- `Generate` 与 `Stream` 平级独立，调用方按需选择。两者对同一 `Request` 的最终态语义必须等价。
- `Stream` 返回 `iter.Seq2[*Chunk, error]`，避免 `model/` 反向依赖根包；调用方用 `for chunk, err := range provider.Stream(ctx, req)` 消费。
- **`TokenCounter` 从 `Provider` 抽离**：token 计数与生成是两类能力 —— 一些 provider 走独立 endpoint（Anthropic `/v1/messages/count_tokens`、OpenAI tiktoken 本地编码），一些 provider 不支持。adapter 可同时实现 `Provider` 与 `TokenCounter`，调用方按 `if tc, ok := p.(TokenCounter); ok { ... }` 探测能力。
- `TokenCounter.Count` 返回 `Usage`（与 `Response.Usage` 同类型），可同时给出 input / output 估算；不支持的字段填 0。
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
    Model    string     // model identifier, may differ from Provider.Name()
    System   string     // top-level system instruction; empty means none
    Messages []*Message // conversation history (user / assistant / tool)
    Tools    []ToolSpec // tool schemas exposed to the model
    Options  []Option   // provider hints; adapters apply what they understand
}
```

要点：

- `System` 简化为单个 `string`。绝大多数 provider 只接受单段 system 文本（OpenAI / Gemini / Mistral / DeepSeek 等），少数支持多块或缓存控制（Anthropic）。**多块结构与缓存策略不进入协议层字段**，统一通过 `Options` 表达。
- `Messages` 仅承载 user / assistant / tool 三角色（参见 §6）。
- `Tools` 描述工具 schema，工具调用与结果通过 `Message.Parts` 中的 `content.ToolUse` / `content.ToolResult` 表达。
- `Options` 是 provider hints 容器：协议层不预设语义，adapter 选择性应用，不识别的 Option **安全忽略**。详见 §4.1。

### 4.1 Options：provider hints 设计

设计动机：cache control / reasoning effort / response format / 采样参数 / 是否允许模型返回并行工具调用等能力在各 provider 间差异巨大，多数模型并不支持。把它们放进 `Request` 顶层字段会污染协议；放进 adapter 又无法在请求级覆盖。`Options` 用 sealed union + 默认值机制兼顾这两点。

```go
// Option is a sealed provider hint. Adapters apply known variants and
// silently ignore the rest, so adding a new Option is non-breaking.
type Option interface { option() }

// CacheHint requests provider-side prompt caching where supported
// (e.g. Anthropic prompt caching, OpenAI cached input). Scope=System
// caches the system block; Scope=Tool caches tool schemas.
type CacheHint struct {
    Scope CacheScope    // System | Message | Tool
    TTL   time.Duration // 0 = provider default
}

// ReasoningEffort tunes thinking depth on reasoning models
// (OpenAI o-series, Anthropic extended thinking, Gemini thinking).
type ReasoningEffort struct {
    Level string // "minimal" | "low" | "medium" | "high"
}

// ResponseFormat constrains the model output shape.
type ResponseFormat struct {
    Schema *jsonschema.Schema // nil → free-form JSON object
    Strict bool
}

// Sampling carries common sampling knobs. Pointer fields distinguish
// "not set" from zero value so adapters can fall back to their defaults.
type Sampling struct {
    Temperature *float64
    TopP        *float64
    MaxTokens   *int
    Stop        []string
}

// ParallelToolCalls asks providers to enable or disable model-emitted
// parallel tool calls. Agent Loop 不读取它；Loop 只执行模型实际返回的 tool wave。
type ParallelToolCalls struct {
    Enabled bool
}

func (CacheHint) option()          {}
func (ReasoningEffort) option()    {}
func (ResponseFormat) option()     {}
func (Sampling) option()           {}
func (ParallelToolCalls) option()  {}
```

**默认值与覆盖优先级**：

adapter 构造采用 **Functional Options（`WithXxx`）** 风格表达默认值，由各 contrib provider 自行定义；`model/` 协议层不暴露这些 helper，根包 Agent 不承载模型默认值配置。请求级 `Request.Options` 覆盖 adapter 默认值。

```go
// adapter 级默认值：通过 provider 构造 options 设置
m := openai.NewModel("gpt-4o",
    openai.WithParallelToolCalls(false),
)

// 请求级 hint：覆盖 adapter 默认值
req := &model.Request{
    System: "you are a helpful assistant",
    Options: []model.Option{
        model.CacheHint{Scope: model.CacheScopeSystem, TTL: 5 * time.Minute},
        model.ParallelToolCalls{Enabled: true}, // 覆盖 adapter 默认 false
    },
}
```

优先级与合并规则：

- **adapter 默认值（`WithXxx`） → `Request.Options` 覆盖**：adapter 内部把自身 `WithXxx` 累积的默认值与 `Request.Options` 合并后再下发。
- 同一 Option 类型按"请求级覆盖默认级"。
- `Sampling` 等多字段 Option **整体替换**，不做字段级 patch（避免歧义）。
- `CacheHint` 在不同 `Scope` 下被视为不同 key，可叠加。
- 不识别的 Option 由 adapter 静默忽略，便于跨 provider 共享同一份 `Request`。
- `ParallelToolCalls` 是 provider hint：OpenAI 映射为 `parallel_tool_calls`，Claude 映射为 `tool_choice.auto.disable_parallel_tool_use = !Enabled`。Agent Loop 不读取该选项，也不提供手动工具执行模式；它只按模型实际返回的同一 assistant message tool wave 执行。

> 协议层只规定 `Request.Options` 的 sealed union 与上述优先级语义；adapter 构造时的 `WithXxx` 命名、粒度、是否暴露成 `Config` 结构体均由各 contrib provider 决定。

**要点**：

- `Request` 不含 stream 开关；是否流式由调用方法决定。
- `Request` 不含 stream/temperature/max_tokens 等顶层字段；采样参数走 `Options` 中的 `Sampling`，保持顶层稳定。

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
- **运行时控制流信号不入 Message**：工具触发的 `ErrLoopExit` / `ErrHandoff` 等 sentinel 由默认 tool wave 翻译为 `event.TurnEnd.Action` 上的 `event.LoopExit` / `event.Handoff`（参见 [design-event-agent-loop.md](design-event-agent-loop.md) §4.2 与 [design-tool-system.md](design-tool-system.md) §6），不污染协议层 `Message`。

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

Options 处理：

| Option | OpenAI 映射 |
|--------|------------|
| `Sampling` | `temperature` / `top_p` / `max_tokens` / `stop` |
| `ResponseFormat` | `response_format`（`json_schema` / `json_object`） |
| `ReasoningEffort` | `reasoning_effort`（仅 o-series 模型） |
| `CacheHint` | 忽略（OpenAI 自动缓存，无显式开关）；或映射到 `prompt_cache_key`（如启用） |

text 增量：`delta.content` → `content.Text{Text: chunk}`。tool_call 增量：按 `tool_calls[].index` / `id` 累加 `arguments`。

### 10.2 Anthropic Messages

| 协议字段 | Anthropic 映射 |
|---------|---------------|
| `Request.System` | `system: [{type:"text", text}]`（单 block） |
| `Request.Messages` | `messages[]`，role ∈ user/assistant |
| `Message.Parts` 中 `content.ToolUse` | content block `tool_use` |
| `Message.Parts` 中 `content.ToolResult` | user 消息的 `tool_result` content block |
| `Request.Tools` | `tools[]` |
| `Generate` | `POST /v1/messages`（非流） |
| `Stream` | SSE 事件流：`content_block_start` / `content_block_delta` / `content_block_stop` / `message_delta` |
| `StopReason` | `message_delta.stop_reason`（end_turn / tool_use / max_tokens） |
| `Usage` | `message_start.usage` + `message_delta.usage`（增量补充） |

Options 处理：

| Option | Anthropic 映射 |
|--------|---------------|
| `Sampling` | `temperature` / `top_p` / `max_tokens` / `stop_sequences` |
| `CacheHint{Scope:System}` | 在 `system[0]` 上加 `cache_control: {type:"ephemeral"}` |
| `CacheHint{Scope:Tools}` | 在 `tools[last]` 上加 `cache_control: {type:"ephemeral"}` |
| `ReasoningEffort` | `thinking: {type:"enabled", budget_tokens: ...}` |
| `ResponseFormat` | 当前无原生支持，adapter 可选退化为 prompt 注入或忽略 |

`content_block_delta` 中的 `text_delta` → `content.Text{Text: delta}`；`input_json_delta` 按 block index 累加到对应 `content.ToolUse.Input`。

## 11. 实现职责边界

`model/` 只定义协议和最小 helper（`Collect`、`MergeOptions`）。以下能力由 provider adapter 或上层组合实现，不进入协议字段：

- 网络重试、限速、熔断与 provider fallback。
- token 编码器加载与缓存。
- 请求签名、鉴权与区域选择。
- `Options` 解释与默认值合并：adapter 自行决定支持哪些 `Option`、不识别的 Option 静默忽略；同 provider 不同模型可有不同支持集。
- provider 私有字段映射（自定义 header、region routing 等）超出 `Option` 体系的能力。

这样 `model/` 保持稳定，具体 provider 可在 `contrib/<provider>` 中独立演进。

## 与红线对照

本文覆盖 r2、r13、r14、r15。
