---
type: design
title: Hook 扩展系统
date: 2026-05-05
status: draft
parent: design-agent-framework.md
related: [design-agent-framework.md]
tags: [agentos, hook, extension, guardrail]
---

# Hook 扩展系统

## 1. 概述

`hook/` 是 AgentOS core 的生命周期观察与拦截命名空间。v1 只定义两类处理器：

- `Observer`：旁路监听，不改变运行流程，用于 metrics、log、trace event、审计旁路写入。
- `Interceptor`：参与控制流，可通过 Mutator 改写请求/响应，或返回 abort sentinel 终止当前动作。

`hook.Event` 是 sealed union：核心事件都实现私有 marker `hookEvent()`，外部包不能扩展 hook event 集合。公开侧提供小接口 `Hook` 与类型安全 helper，例如 `OnPreModelCall`、`OnPostToolCall`；每个 helper 返回一个可直接传给 `blades.WithHooks(...hook.Hook)` 的 Hook。

Hook 只承载 Agent Loop 的稳定生命周期契约，不承载应用业务事件。配置加载、文件监听、任务队列、UI notification、workspace 事件等由应用自己的 event bus 处理。

## 2. 六类核心 event

v1 核心 hook event 全集固定为六类：`PreModelCall`、`PostModelCall`、`PreToolCall`、`PostToolCall`、`TurnStart`、`TurnEnd`。

```go
package hook

import (
    "context"
    "encoding/json"

    "github.com/go-kratos/blades/content"
    "github.com/go-kratos/blades/event"
    "github.com/go-kratos/blades/model"
    "github.com/go-kratos/blades/tools"
)

type Event interface{ hookEvent() }

type PreModelCall struct {
    AgentName string
    Turn      int
    Request   *model.Request
    Mutator   *ModelRequestMutator
}

type PostModelCall struct {
    AgentName string
    Turn      int
    Request   *model.Request
    Response  *model.Response
    Err       error
    Mutator   *ModelResponseMutator
}

type PreToolCall struct {
    AgentName string
    Turn      int
    ToolName  string
    Tool      tools.Tool
    Input     json.RawMessage
    Mutator   *ToolCallMutator
}

type PostToolCall struct {
    AgentName string
    Turn      int
    ToolName  string
    Tool      tools.Tool
    Input     json.RawMessage
    Result    *tools.Result
    Err       error
    Mutator   *ToolResultMutator
}

type TurnStart struct {
    AgentName string
    Turn      int
    SessionID string
    Input     event.Input
}

type TurnEnd struct {
    AgentName  string
    Turn       int
    SessionID  string
    Parts      []content.Part
    StopReason model.StopReason
    Usage      *model.Usage
    Err        error
}
```

字段约定：

- `AgentName`、`Turn`、`SessionID` 用于关联一轮执行；session 仍通过 `session.NewContext` / `session.FromContext` 传递。
- `Request` / `Response` 使用 v1 `model.Provider` 协议：Provider 只有 `Name`、`Stream`、`Count` 三方法，`Stream` 返回 `iter.Seq2[*model.Response, error]`。
- `Tool` 使用 v1 `tools.Tool` 两方法接口：`Spec(ctx)` 与 `Handle(ctx, input)`。
- `Input` 使用 v1 `event.Input`，文本由 `event.NewPromptText` / `event.NewSteerText` 构造，具体类型为 `event.Prompt` / `event.Steer`。
- `Parts` 直接使用 `content.Part`。
- 只有可改写的调用边界携带 `Mutator`；Observer 不应调用 Mutator。

## 3. Observer vs Interceptor

| 维度 | Observer | Interceptor |
|---|---|---|
| 目标 | 旁路观察 | 控制与改写 |
| 是否改变流程 | 否 | 是 |
| 典型用途 | metrics、log、trace event、审计镜像 | guardrail、脱敏、预算检查、策略拦截 |
| 错误语义 | 记录错误，不应影响主流程；应用层聚合 Hook 可按配置降级 | 返回错误会影响调用点；abort sentinel 终止当前动作 |
| Mutator | 不使用 | 可使用事件上的 Mutator 指针 |
| 顺序要求 | 可并行或顺序执行，结果不参与决策 | 必须按注册顺序执行，前一个改写对后一个可见 |

Observer 的设计目标是低耦合：即使 OTel exporter、日志 sink 或指标后端失败，也不应破坏 Agent Loop。Interceptor 的设计目标是强语义：它位于模型、工具和 turn 边界，可以阻止不安全行为。

## 4. Hook 接口与类型安全 helper

核心不提供必需的 Registry 类型。根包 `WithHooks(...hook.Hook)` 直接接收 Hook 列表；需要 registry、fan-out、动态启停或按应用配置分发时，由应用层实现一个聚合型 `hook.Hook` 并注入。

```go
type Hook interface {
    Handle(ctx context.Context, ev Event) error
}

type HookFunc func(context.Context, Event) error

func (f HookFunc) Handle(ctx context.Context, ev Event) error {
    return f(ctx, ev)
}
```

公开侧不要求调用方手写 type assertion，统一使用返回 `Hook` 的 helper：

```go
type Observer[E Event] func(context.Context, *E) error
type Interceptor[E Event] func(context.Context, *E) error

func OnPreModelCall(fn Observer[PreModelCall]) Hook
func OnPostModelCall(fn Observer[PostModelCall]) Hook
func OnPreToolCall(fn Observer[PreToolCall]) Hook
func OnPostToolCall(fn Observer[PostToolCall]) Hook
func OnTurnStart(fn Observer[TurnStart]) Hook
func OnTurnEnd(fn Observer[TurnEnd]) Hook
```

实现侧可以用泛型函数类型保证 helper 入参一致，并在 `Handle` 内部做 type switch：

```go
func OnPreModelCall(fn Observer[PreModelCall]) Hook {
    return HookFunc(func(ctx context.Context, ev Event) error {
        e, ok := ev.(*PreModelCall)
        if !ok {
            return nil
        }
        return fn(ctx, e)
    })
}
```

核心可提供简单的顺序组合 helper，但它只是一个 `Hook` 实现，不是唯一注册中心：

```go
func Chain(hooks ...Hook) Hook {
    return HookFunc(func(ctx context.Context, ev Event) error {
        for _, h := range hooks {
            if h == nil {
                continue
            }
            if err := h.Handle(ctx, ev); err != nil {
                return err
            }
        }
        return nil
    })
}
```

应用层 Registry 示例：

```go
type Registry struct {
    hooks []hook.Hook
}

func (r *Registry) Use(hooks ...hook.Hook) {
    r.hooks = append(r.hooks, hooks...)
}

func (r *Registry) Handle(ctx context.Context, ev hook.Event) error {
    for _, h := range r.hooks {
        if err := h.Handle(ctx, ev); err != nil {
            return err
        }
    }
    return nil
}
```

外部无法实现 `hookEvent()`，因此 Hook 不需要处理应用自定义 hook event。应用业务事件应进入应用自己的 bus。

## 5. Interceptor 改写与 abort 语义

Interceptor 通过事件上的 Mutator 指针改写请求或响应。Mutator 是显式能力对象，避免 handler 直接替换核心字段导致 invariant 失控。

```go
var ErrAbort = errors.New("hook abort")

type AbortError struct {
    Reason string
    Err    error
}

func Abort(reason string) error { return &AbortError{Reason: reason, Err: ErrAbort} }
```

推荐 Mutator 能力：

```go
type ModelRequestMutator struct{ /* internal */ }
func (m *ModelRequestMutator) ReplaceRequest(*model.Request)
func (m *ModelRequestMutator) SetSystem(string)
func (m *ModelRequestMutator) SetOptions([]model.Option)

type ModelResponseMutator struct{ /* internal */ }
func (m *ModelResponseMutator) ReplaceResponse(*model.Response)

type ToolCallMutator struct{ /* internal */ }
func (m *ToolCallMutator) ReplaceInput(json.RawMessage)

type ToolResultMutator struct{ /* internal */ }
func (m *ToolResultMutator) ReplaceResult(*tools.Result)
```

语义规则：

1. `PreModelCall` 可改写 `*model.Request`，常见场景是注入 policy 生成的系统块、删除敏感内容或限制工具 spec。
2. `PostModelCall` 可改写 `*model.Response`，常见场景是过滤敏感输出或标准化 stop reason。
3. `PreToolCall` 可改写工具输入，或返回 `Abort(reason)` 阻止工具执行。
4. `PostToolCall` 可改写 `tools.Result`，常见场景是截断超大结果；上下文压缩仍由 `compact.Compactor` 负责。
5. `TurnStart` / `TurnEnd` 只作为轮次边界，不提供 Mutator；需要控制输入时应在应用输入流或前置 policy 中处理。
6. ctx cancellation 优先于 hook abort；Loop 应按 Go 惯例传播 `context.Context` 的取消状态。

## 6. 应用事件总线边界

Hook 不是通用事件总线。下列事件不进入 `hook/`：

- 产品 UI notification、进度条、命令状态。
- workspace、文件系统、配置刷新、账号状态。
- 应用自定义任务、队列、channel、后台 job。
- Memory 的业务抽取任务；v1 `memory.Memory` 只有 `Recall` 与 `Remember` 两方法，应用可在 prompt section 或 turn 后处理里调用。

应用如需事件系统，应在自身包内定义 bus，并可在 bus handler 中调用 AgentOS API。核心只保证六类 hook event 的兼容性。

## 7. 设计决策

### 为什么 sealed

Hook 是核心运行时契约。sealed event 集合让默认 `llmAgent`、Hook 实现、contrib/otel 和测试都能穷尽处理生命周期边界，避免第三方事件混入后破坏顺序、错误传播或安全语义。应用扩展点应在应用 bus，而不是扩展核心 hook union。

### 为什么二元划分

观察与拦截的失败语义不同。Observer 失败通常应降级；Interceptor 失败必须反馈到模型或工具调用点。拆分后，metrics/log 不会意外改变行为，guardrail 也不会被当作旁路日志吞掉。

### 为什么统一 Pre*/Post* 命名

`Pre*` / `Post*` 直接表达调用边界：进入模型、离开模型、进入工具、离开工具、轮次开始、轮次结束。命名与根包 `Agent.Run(ctx, input <-chan event.Input) blades.Generator[event.Output, error]` 的单 Agent 执行模型一致，避免把产品级 run lifecycle 混入核心 hook。

## 与红线对照

- r22：Observer / Interceptor 二元划分、sealed `hook.Event`、六类核心 event、返回 `Hook` 的泛型 helper、Hook 内部 type switch、Mutator 与 abort 语义均已覆盖。
- 根包边界：Run 位于根包 `Agent.Run`，根包保留 `Agent`、`NewAgent`、`Option`、`Generator`、默认 `llmAgent` 与 `WithModel` / `WithTools` / `WithSession` / `WithPolicy` / `WithHooks` / `WithCompact` / `WithPrompt` / `WithRequestBuilder` / `WithToolExecutor`。
- 相关协议：`event.Prompt` / `event.Steer`、`model.Provider` 三方法、`tools.Tool` 两方法、`session.Session` 五方法、`compact.Compactor`、`memory.Memory`、`policy.Policy`、`prompt.Section` 函数类型、`NewContext` / `FromContext` 命名均按 v1 描述。
