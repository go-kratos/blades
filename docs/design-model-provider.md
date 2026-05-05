---
type: design
title: Model 与 Provider 协议
parent: design-agent-framework.md
date: 2026-05-01
status: draft
modules: [module-2, module-9, module-10]
---

# Model 与 Provider 协议

`model/` 是模型上下文协议叶子包。它只描述 Provider、Session、Compact 需要共享的 DTO 和小接口，不导入 `event/`、`tools/`、`blades/`、`hook/` 或 `policy/`。

## Message 与 Part

```go
package model

type Role string

const (
    RoleSystem    Role = "system"
    RoleUser      Role = "user"
    RoleAssistant Role = "assistant"
    RoleTool      Role = "tool"
)

type Message struct {
    ID     string
    Role   Role
    Parts  []Part
    Status Status
}

type Part interface{ part() }

type TextPart struct{ Text string }
type FilePart struct {
    URI      string
    MimeType string
    Name     string
}
type DataPart struct {
    Data     []byte
    MimeType string
    Name     string
}
type JSONPart struct{ Value any }
type ThinkingPart struct{ Text string }
type ToolUsePart struct {
    CallID string
    Name   string
    Args   json.RawMessage
}
type ToolResultPart struct {
    CallID string
    Parts  []Part
    Err    error
}
type CompactionSummaryPart struct {
    Summary      string
    TokensBefore int64
    TokensAfter  int64
}
```

`model.Part` 与 `event.InputPart` / `event.OutputPart` 不共享 Go 类型。Event-to-Message、ToolResult-to-Message 和 Message-to-Event 的转换只在 `internal/loop`。

## Provider

```go
type Provider interface {
    Stream(ctx context.Context, req *Request) (Stream, error)
}

type Stream interface {
    Recv(ctx context.Context) (*Response, error)
    Close() error
}

type Request struct {
    Model        string
    Messages     []*Message
    Tools        []ToolSpec
    System       string
    Cache        []CacheBreakpoint
    MaxTokens    int64
    Temperature  float64
    Source       string
}

type Response struct {
    MessageID string
    Delta     []Part
    ToolCalls []ToolUsePart
    Usage     *TokenUsage
    Stop      StopReason
}

type RequestSnapshot struct {
    Model        string
    MessageCount int
    ToolNames    []string
    MaxTokens    int64
    Source       string
}

type ResponseSnapshot struct {
    MessageID string
    PartCount int
    Usage     *TokenUsage
    Stop      StopReason
}
```

Hook 使用 `RequestSnapshot` / `ResponseSnapshot`，不能接触 raw `*Request` 或修改消息。

## ToolSpec

```go
type ToolSpec struct {
    Name        string
    Description string
    InputSchema json.RawMessage
}
```

`ToolSpec` 是 provider-neutral 声明。`tools.Tool` 到 `model.ToolSpec` 的映射由 Agent Loop 完成，避免 `model/` 导入 `tools/`。

## Token 计数

```go
type Counter interface {
    Count(ctx context.Context, messages ...*Message) (int64, error)
}

type CharCounter struct{}
type ProviderCounter struct{ Provider Provider }
type CachedCounter struct{ Inner Counter }
```

`CharCounter` 是 stdlib-only 估算降级；Provider 原生计数和 tokenizer 依赖放在 contrib 或可选实现中。

## RetryPolicy

```go
type RetryPolicy struct {
    MaxRetries      int
    BaseDelay       time.Duration
    MaxDelay        time.Duration
    FallbackModel   string
    OnRefresh       func(ctx context.Context) error
    SourceAwareness SourceAwareness
    PersistentRetry *PersistentRetryConfig
}
```

重试发生在 Agent Loop 调用 `Provider.Stream` 的边界，不放进 `policy/`，也不要求 Provider 自己实现。退避必须使用 timer + `ctx.Done()`，不能用裸 `time.Sleep`：

```go
timer := time.NewTimer(delay)
select {
case <-timer.C:
case <-ctx.Done():
    if !timer.Stop() {
        select {
        case <-timer.C:
        default:
        }
    }
    return nil, ctx.Err()
}
```

无人值守持久重试可以发 heartbeat，但 heartbeat 必须绑定当前 retry wait 的 context 或 timer 生命周期，不能每次 retry 新建一个可能泄漏的 goroutine。

## 设计决策

1. **`model/` 是叶子协议包**：只保留 Provider、Session、Compact 共享的模型上下文语言。
2. **Provider 只消费 `model.Request`**：OpenAI、Anthropic、Gemini 等格式差异留在 contrib provider 内部。
3. **重试在调用边界**：网络错误、429、529、401 refresh 和 fallback 是模型调用策略，不进入 `policy/`。
4. **hook 看快照**：避免 hook 绕过 `internal/loop` 修改 Message 或 Provider request。
