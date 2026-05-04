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
| 重试 | `middleware/retry.go`（Agent 级） | `retry.Policy`（Provider 级，感知错误类型与 QuerySource） |
| 错误分类 | 无 | 按 HTTP 状态码 + 连接错误分类处理 |
| 降级 | 无 | 529 模型过载自动降级；后台查询快速失败 |
| 认证刷新 | 无 | 401 自动刷新 token |
| 连接失效 | 无 | ECONNRESET/EPIPE 自动重试 |
| 持久重试 | 无 | 无人值守会话长退避 + 心跳 |

### 9.1 RetryPolicy

```go
package retry

// Policy 定义 Provider 级别的重试策略。
// 与 Agent 级 Middleware 不同，RetryPolicy 感知 Provider 的具体错误类型和 streaming 状态，
// 在 Agent Loop 内部的 Provider 调用处直接处理，不需要重建整个轮次。
type Policy struct {
    MaxRetries      int           // 最大重试次数，默认 10
    BaseDelay       time.Duration // 基础退避时间，默认 500ms
    MaxDelay        time.Duration // 最大退避时间，默认 60s
    FallbackModel   string        // 529 降级模型（如 claude-sonnet-4-6）
    OnRefresh       func(ctx context.Context) error // 401 认证刷新回调
    SourceAwareness SourceAwareness // QuerySource 感知重试配置
    PersistentRetry *PersistentRetryConfig // 无人值守会话的持久重试配置（可选）
}

// SourceAwareness 根据请求来源（QuerySource）区分前台/后台查询，
// 对后台查询在 529 过载时执行快速失败策略，防止容量级联时产生 3-10x 的网关放大效应。
type SourceAwareness struct {
    ForegroundSources []string // ["user", "sub_agent", "compact", "sdk"]
    BackgroundSources []string // ["extract_memory", "task_summary", "skill"]
    Max529Retries     int      // 后台查询 529 最大重试次数，默认 3
}

// PersistentRetryConfig 用于无人值守（headless）会话的持久重试模式。
// 当会话没有用户交互时，采用更长的退避上限和心跳机制，避免静默失败。
type PersistentRetryConfig struct {
    MaxBackoff        time.Duration // 最大退避时间，默认 5min
    HeartbeatInterval time.Duration // 心跳日志间隔，默认 30s
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
    ClassStaleConn                      // 连接失效（ECONNRESET/EPIPE），立即重试
)

// Backoff 计算退避时间。
type Backoff struct {
    Base   time.Duration
    Max    time.Duration
    Jitter float64 // 0-1 之间的抖动因子
}

func (b *Backoff) Duration(attempt int) time.Duration
```

### 9.2 默认常量

| 常量 | 值 | 说明 |
|------|----|------|
| MaxRetries | 10 | 前台查询最大重试次数 |
| Max529Retries | 3 | 后台查询 529 过载最大重试次数 |
| BaseDelay | 500ms | 指数退避基础延迟 |
| MaxDelay | 60s | 指数退避上限 |
| PersistentMaxBackoff | 5min | 无人值守会话退避上限 |
| HeartbeatInterval | 30s | 无人值守会话心跳间隔 |

### 9.3 与 Agent Loop 的集成

```go
// Agent Loop 内部的 Provider 调用处：
func (a *agent) callProvider(ctx context.Context, req *model.Request) (iter.Seq2[*model.Response, error], error) {
    maxRetries := a.retryPolicy.MaxRetries
    overloadRetries := 0

    for attempt := 0; attempt <= maxRetries; attempt++ {
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

            case ClassStaleConn:
                // ECONNRESET/EPIPE：连接失效，无需退避直接重试
                continue

            case ClassOverloaded:
                overloadRetries++
                // QuerySource 感知：后台查询在 529 时快速失败
                if a.isBackgroundSource(req.QuerySource) &&
                    overloadRetries > a.retryPolicy.SourceAwareness.Max529Retries {
                    return nil, fmt.Errorf("background query %s: 529 fast-fail after %d retries",
                        req.QuerySource, overloadRetries)
                }
                if a.retryPolicy.FallbackModel != "" {
                    req.Model = a.retryPolicy.FallbackModel
                }
                fallthrough

            case ClassRetryable, ClassRateLimit:
                delay := a.backoff.Duration(attempt)
                // 持久重试模式：无人值守会话使用更长退避上限 + 心跳
                if pc := a.retryPolicy.PersistentRetry; pc != nil {
                    if delay > pc.MaxBackoff {
                        delay = pc.MaxBackoff
                    }
                    go a.emitHeartbeat(ctx, pc.HeartbeatInterval, attempt, delay)
                }
                time.Sleep(delay)
                continue
            }
        }
        return stream, nil
    }
    return nil, ErrMaxRetriesExceeded
}

// isBackgroundSource 判断 QuerySource 是否属于后台查询。
func (a *agent) isBackgroundSource(source string) bool {
    for _, s := range a.retryPolicy.SourceAwareness.BackgroundSources {
        if s == source {
            return true
        }
    }
    return false
}
```

### 9.4 设计决策：QuerySource 感知重试

后台查询（`extract_memory`、`task_summary`、`skill` 等）在遇到 529 过载时采用快速失败策略，最多重试 3 次后立即放弃。原因：

1. **防止网关放大效应**：容量级联（capacity cascade）期间，每个后台查询的重试会产生 3-10x 的额外请求量。如果后台查询与前台查询使用相同的重试策略（10 次），大量后台重试会进一步加剧上游过载，形成正反馈回路
2. **后台查询可延迟**：`extract_memory` 和 `task_summary` 等后台任务不影响用户交互的即时体验，失败后可以在下一个空闲窗口重新调度
3. **保护前台查询的可用性**：通过限制后台查询的重试预算，将有限的容量优先分配给用户直接发起的前台请求（`user`、`sub_agent`、`compact`、`sdk`）

### 9.5 错误处理分类表

| 错误类型 | ErrorClass | 重试策略 | 说明 |
|----------|------------|----------|------|
| 400 Bad Request | `ClassFatal` | 不重试 | 请求参数错误，重试无意义 |
| 401 Unauthorized | `ClassAuthExpired` | 刷新 token 后重试 | 调用 `OnRefresh` 回调 |
| 429 Too Many Requests | `ClassRateLimit` | 指数退避，尊重 `Retry-After` | 限流，等待后重试 |
| 529 Overloaded | `ClassOverloaded` | 前台：降级 + 退避；后台：快速失败（≤3 次） | 模型过载，区分查询来源 |
| 5xx Server Error | `ClassRetryable` | 指数退避重试 | 服务端临时错误 |
| ECONNRESET | `ClassStaleConn` | 立即重试（无退避） | TCP 连接被对端重置，常见于长连接复用 |
| EPIPE | `ClassStaleConn` | 立即重试（无退避） | 写入已关闭的连接，常见于 HTTP/2 idle timeout |
| 其他网络错误 | `ClassRetryable` | 指数退避重试 | DNS 解析失败、连接超时等 |

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
