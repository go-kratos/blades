---
type: design
title: 基础设施（重试、Token 计数、可观测性、Graph）
parent: design-agent-framework.md
date: 2026-05-01
status: draft
modules: [module-9, module-10, module-11, module-12]
---

# 基础设施（重试、Token 计数、可观测性、Graph）

## 模块 9：API 错误处理与重试

### 现状对比

| 维度 | 当前 Blades | 新设计 |
|------|------------|--------|
| 重试 | `middleware/retry.go`（Agent 级） | `retry.Policy`（Provider 级，感知错误类型） |
| 错误分类 | 无 | 按 HTTP 状态码分类处理 |
| 降级 | 无 | 529 模型过载自动降级 |
| 认证刷新 | 无 | 401 自动刷新 token |

### 9.1 RetryPolicy

```go
package retry

// Policy 定义 Provider 级别的重试策略。
// 与 Agent 级 Middleware 不同，RetryPolicy 感知 Provider 的具体错误类型和 streaming 状态，
// 在 Agent Loop 内部的 Provider 调用处直接处理，不需要重建整个轮次。
type Policy struct {
    MaxRetries    int           // 最大重试次数，默认 3
    BaseDelay     time.Duration // 基础退避时间，默认 1s
    MaxDelay      time.Duration // 最大退避时间，默认 60s
    FallbackModel string        // 529 降级模型（如 claude-sonnet-4-6）
    OnRefresh     func(ctx context.Context) error // 401 认证刷新回调
}

// ErrorClassifier 将 Provider 错误分类为可重试/不可重试。
type ErrorClassifier interface {
    Classify(err error) ErrorClass
}

type ErrorClass int
const (
    ClassFatal       ErrorClass = iota // 不可重试（400 参数错误等）
    ClassRetryable                      // 可重试（5xx 服务端错误）
    ClassRateLimit                      // 限流（429），使用 Retry-After 退避
    ClassOverloaded                     // 过载（529），降级到备用模型
    ClassAuthExpired                    // 认证过期（401），刷新后重试
)

// Backoff 计算退避时间。
type Backoff struct {
    Base   time.Duration
    Max    time.Duration
    Jitter float64 // 0-1 之间的抖动因子
}

func (b *Backoff) Duration(attempt int) time.Duration
```

### 9.2 与 Agent Loop 的集成

```go
// Agent Loop 内部的 Provider 调用处：
func (a *agent) callProvider(ctx context.Context, req *model.Request) (iter.Seq2[*model.Response, error], error) {
    for attempt := 0; attempt <= a.retryPolicy.MaxRetries; attempt++ {
        stream := a.model.NewStreaming(ctx, req)
        // ... 消费 stream ...
        if err != nil {
            class := a.classifier.Classify(err)
            switch class {
            case ClassFatal:
                return nil, err
            case ClassAuthExpired:
                if refreshErr := a.retryPolicy.OnRefresh(ctx); refreshErr != nil {
                    return nil, refreshErr
                }
                continue
            case ClassOverloaded:
                if a.retryPolicy.FallbackModel != "" {
                    req.Model = a.retryPolicy.FallbackModel
                }
                fallthrough
            case ClassRetryable, ClassRateLimit:
                time.Sleep(a.backoff.Duration(attempt))
                continue
            }
        }
        return stream, nil
    }
    return nil, ErrMaxRetriesExceeded
}
```

---

## 模块 10：Token 计数

### 现状对比

| 维度 | 当前 Blades | 新设计 |
|------|------------|--------|
| 计数 | `internal/counter`（1 token ≈ 4 chars） | `model.Counter` 接口 + 多实现 |
| 精度 | 粗略估算 | Provider 原生 / tiktoken / 估算三级降级 |
| 使用 | 仅 context/window 和 context/summary | 压缩管线、TurnState、prompt.Builder 全局使用 |

### 10.1 model.Counter 接口

```go
package model

// Counter 计算消息的 token 数量。
type Counter interface {
    Count(messages ...*Message) int64
}

// CharCounter 字符估算实现（1 token ≈ 4 chars）。
// 作为降级方案，不需要外部依赖。
type CharCounter struct{}

// ProviderCounter 使用 Provider 原生 token 计数 API。
// 如 Anthropic 的 /v1/messages/count_tokens。
type ProviderCounter struct {
    provider Provider
}

// CachedCounter 缓存已计算的 token 数，避免重复计算。
// 包装任意 Counter 实现。
type CachedCounter struct {
    inner Counter
    cache map[string]int64 // key = message ID
}
```

---

## 模块 11：可观测性

### 设计

Event 系统天然适合 tracing——每个 OutputEvent 可以关联到当前 span。
可观测性通过 Hook 系统集成，不侵入核心代码。

```go
// OTelHook 通过 Hook 系统集成 OpenTelemetry。
// 注册为全局 Hook，自动为关键生命周期事件创建 span。
func RegisterOTelHooks(registry *hook.Registry, tracer trace.Tracer) {
    // Agent 生命周期
    hook.Observe(registry, func(ctx context.Context, e *hook.HookAgentStart) error {
        _, span := tracer.Start(ctx, "agent.turn",
            trace.WithAttributes(
                attribute.String("agent.name", e.AgentName),
                attribute.Int("agent.turn", e.Turn),
            ))
        // span 通过 context 传播，在 HookAgentEnd 中结束
        return nil
    })

    // Model 调用
    hook.Observe(registry, func(ctx context.Context, e *hook.HookAfterModelResponse) error {
        span := trace.SpanFromContext(ctx)
        span.SetAttributes(
            attribute.Int64("gen_ai.usage.input_tokens", e.Usage.InputTokens),
            attribute.Int64("gen_ai.usage.output_tokens", e.Usage.OutputTokens),
        )
        return nil
    })

    // Tool 执行
    hook.Observe(registry, func(ctx context.Context, e *hook.HookPreToolUse) error {
        _, _ = tracer.Start(ctx, "tool."+e.ToolName)
        return nil
    })
}
```

---

## 模块 12：graph 包定位

### 现状

`graph/` 是完全独立的 DAG 执行器，有自己的 `State`、`Handler`、`Middleware` 类型，与 `blades.Agent` 接口不兼容。

### 定位决策

`graph/` 保持独立子系统，不强制统一到 Event 系统。原因：

1. DAG 执行器的语义（编译时验证、检查点恢复、条件边）与 Agent Loop 的语义（LLM 对话、工具调用、压缩）本质不同
2. 强制统一会增加不必要的复杂度，且破坏 graph 包的独立可用性
3. 当前 `flow/deep.go` 已经通过 `DeepAgent` 桥接了 graph 和 Agent

### 桥接方式

通过 `flow/` 包提供桥接，而非在 graph 包内部集成 Event：

```go
// flow/graph.go — 将 graph.Executor 包装为 blades.Agent
func GraphAgent(name string, executor *graph.Executor, opts ...GraphAgentOption) Agent

// GraphAgent 内部：
// 1. 从 InputEvent 中提取初始 State
// 2. 调用 executor.Execute(ctx, state)
// 3. 将结果转换为 OutputEvent 序列
// 4. graph.Handler 中如果需要调用 LLM，通过注入的 model.Provider 实现
```
