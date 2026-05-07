---
type: design
title: Hook 扩展系统
date: 2026-05-07
status: draft
parent: design-agent-framework.md
related: [design-agent-framework.md, design-event-agent-loop.md, design-model-provider.md, design-tool-system.md]
tags: [agentos, hook, extension, guardrail]
---

# Hook 扩展系统

## 1. 概述

`hook/` 是 AgentOS core 的生命周期观察与拦截命名空间。v1 只定义一个处理器接口 `Hook`：handler 自行决定是否通过 Mutator 改写请求/响应、是否返回 abort sentinel 终止当前动作。无论"旁路观察"还是"控制改写"，都是同一个 `Hook` 接口的不同使用方式，不在核心引入额外角色概念。

`hook.Event` 是 sealed union：核心事件都实现私有 marker `hookEvent()`，外部包不能扩展 hook event 集合。公开侧提供小接口 `Hook` 与类型安全 helper，例如 `OnPreModelCall`、`OnPostToolCall`；每个 helper 返回一个可直接传给 `blades.WithHooks(...hook.Hook)` 的 Hook。

Hook 只承载 Agent Loop 的稳定生命周期契约，不承载应用业务事件。配置加载、文件监听、任务队列、UI notification、workspace 事件等由应用自己的 event bus 处理。

## 2. 八类核心 event

v1 核心 hook event 全集固定为八类：`PreModelCall`、`PostModelCall`、`PreToolCall`、`PostToolCall`、`TurnStart`、`TurnEnd`、`LoopExit`、`Handoff`。其中 `LoopExit` / `Handoff` 与 `event.LoopExit` / `event.Handoff` 同源同步触发——前者面向 Output 流的机器消费者（`flow.LoopAgent` / `flow.RoutingAgent`），后者面向 hook handler 的人工业务介入（审计、日志、报警）。

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
    Tool      tools.Tool
    Input     json.RawMessage
    Mutator   *ToolCallMutator
}

type PostToolCall struct {
    AgentName string
    Turn      int
    Tool      tools.Tool
    Input     json.RawMessage
    Result    *tools.Result
    Err       error
    Mutator   *ToolResultMutator
}

type TurnStart struct {
    AgentName string
    Turn      int
    Input     event.Input
}

type TurnEnd struct {
    AgentName  string
    Turn       int
    Parts      []content.Part
    StopReason model.StopReason
    Usage      *model.Usage
    Err        error
}

// LoopExit / Handoff 与 event.LoopExit / event.Handoff 同源；hook 在
// 对应 Output 帧发出时同步触发，便于审计与日志介入。
type LoopExit struct {
    AgentName string
    Turn      int
    Event     event.LoopExit
}

type Handoff struct {
    AgentName string
    Turn      int
    Event     event.Handoff
}
```

字段约定：

- `AgentName`、`Turn` 用于在事件流中关联一轮执行；当前会话**不**通过 event 字段携带，统一由 `session.FromContext(ctx)` 读取（与 framework 的 ctx 三准则一致），避免同一信息在 ctx 与 event 上重复。
- `Request` / `Response` 使用 v1 `model.Provider` 协议：Provider 三方法 `Name` / `Generate(ctx, *Request) (*Response, error)` / `Stream(ctx, *Request) iter.Seq2[*model.Chunk, error]`；token 计数由独立的 `model.TokenCounter` 接口承担（按能力探测），不在 Provider 上。Stream 路径下 Loop 在 step 完成后用 `model.Collect` 把 chunk 序列汇总为 `*model.Response` 再触发 `PostModelCall`。
- `Tool` 使用 v1 `tools.Tool` 两方法接口：`Spec() ToolSpec` 与 `Handle(ctx, input json.RawMessage) (*Result, error)`；如需工具名调用 `Tool.Spec().Name`，event 上不再单独保留 `ToolName` 字段。
- `Input` 使用 v1 `event.Input`，文本由 `event.NewPromptText` / `event.NewSteerText` 构造，具体类型为 `event.Prompt` / `event.Steer`。
- `Parts` 直接使用 `content.Part`。
- 只有可改写的调用边界携带 `Mutator`；不需要改写的 Hook 直接忽略 `Mutator` 字段即可（详见 §3、§4）。
- `Pre/PostModelCall` 即 model **step 边界**：一个 turn 可能包含多次 step（多轮 tool call），每个 step 对应一对 `PreModelCall` / `PostModelCall`，与 `event.StepEnd` 一一对应。`TurnStart` / `TurnEnd` 才是 turn 边界。

## 3. 使用模式

Hook 是单一接口，使用上有两种典型形态，但核心不区分类型，由 handler 自己决定行为：

- **旁路观察**（metrics / log / trace / 审计镜像）：handler 读取 event 字段后写出指标或日志，不调用 Mutator、不返回 abort。失败应在 handler 内部 swallow（记录后 `return nil`），避免影响 Agent Loop。
- **改写与拦截**（guardrail / 脱敏 / 预算检查 / 策略）：handler 通过事件上的 Mutator 字段改写请求、响应或工具输入；必要时返回 `Abort(reason)` 终止当前动作（语义见 §5.3）。

无论哪种形态，所有 Hook 都按注册顺序串行派发；前一个 Hook 通过 Mutator 写入的字段对后一个可见。需要按事件类型路由、异步 fan-out、按配置启停等聚合行为，由应用层自行实现一个 `hook.Hook` 并通过 `WithHooks` 注入。

## 4. Hook 接口与类型安全 helper

核心只提供 `Hook` 接口、`HookFunc` 适配器、8 个 `On*` helper 与 `Chain` 顺序组合 helper。根包 `WithHooks(...hook.Hook)` 直接接收 Hook 列表，按注册顺序串行派发。动态启停、按应用配置分发、按事件类型路由、异步 fan-out、热更新等聚合行为均由应用层自行实现一个 `hook.Hook` 并通过 `WithHooks` 注入即可，不进入核心 surface area。

```go
type Hook interface {
    Handle(ctx context.Context, ev Event) error
}

type HookFunc func(context.Context, Event) error

func (f HookFunc) Handle(ctx context.Context, ev Event) error {
    return f(ctx, ev)
}
```

公开侧不要求调用方手写 type assertion，统一使用返回 `Hook` 的 helper。所有 helper 共用同一回调签名 `func(context.Context, *E) error`：

```go
// Handler 是 helper 的统一回调签名；E 受 hook.Event 约束。
type Handler[E Event] func(context.Context, *E) error

func OnPreModelCall(fn Handler[PreModelCall]) Hook
func OnPostModelCall(fn Handler[PostModelCall]) Hook
func OnPreToolCall(fn Handler[PreToolCall]) Hook
func OnPostToolCall(fn Handler[PostToolCall]) Hook
func OnTurnStart(fn Handler[TurnStart]) Hook
func OnTurnEnd(fn Handler[TurnEnd]) Hook
func OnLoopExit(fn Handler[LoopExit]) Hook
func OnHandoff(fn Handler[Handoff]) Hook
```

实现侧用泛型函数类型保证 helper 入参一致，并在 `Handle` 内部做 type switch：

```go
func OnPreModelCall(fn Handler[PreModelCall]) Hook {
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

外部无法实现 `hookEvent()`，因此 Hook 不需要处理应用自定义 hook event。应用业务事件应进入应用自己的 bus。

## 5. 改写、abort 与 hook 链执行模型

### 5.1 Mutator：细粒度 setter，避免整体替换

Hook 通过事件上的 Mutator 指针改写请求或响应。Mutator 是显式能力对象，仅暴露**细粒度 setter**，**不**提供整体替换 API（如 `ReplaceRequest` / `ReplaceResponse` / `ReplaceResult`）。这样可以：

- 让 Loop 在每个修改点保留 invariant 校验机会（比如 message role 顺序、tool call ID 配对）。
- 让多个 Hook 顺序写入时彼此正交（A 改 system，B 追加 message，互不覆盖）。
- 让 trace / debug 能精确知道哪个字段被哪个 hook 改写。

```go
type ModelRequestMutator struct{ /* internal */ }
func (m *ModelRequestMutator) SetSystem(string)
func (m *ModelRequestMutator) SetOptions([]model.Option)
func (m *ModelRequestMutator) AppendMessages(...*model.Message)
func (m *ModelRequestMutator) SetTools([]model.ToolSpec)

type ModelResponseMutator struct{ /* internal */ }
func (m *ModelResponseMutator) SetParts([]content.Part)
func (m *ModelResponseMutator) SetStopReason(model.StopReason)

type ToolCallMutator struct{ /* internal */ }
func (m *ToolCallMutator) SetInput(json.RawMessage)

type ToolResultMutator struct{ /* internal */ }
func (m *ToolResultMutator) SetParts([]content.Part)
func (m *ToolResultMutator) SetIsError(bool)
```

> 设计取舍：早期草案考虑提供 `ReplaceRequest(*model.Request)` 让 Hook 整体替换。但整体替换会绕过 Loop 在 system / messages / tool spec 上的不变式检查，并让多个 Hook 顺序执行时变成"最后写入者覆盖"的隐式语义。改用 setter 之后：(1) Loop 可以在 setter 内部立刻验证，(2) 多 Hook 之间的修改是字段级合并，(3) trace 可以记录"PreModelCall step 2: SetSystem by guardrail"。需要"几乎完全重写请求"时，Hook 可在自身实现里组装一个全新对象，再调用所有 setter，等价于 Replace 但语义显式。

### 5.2 Abort sentinel

```go
var ErrAbort = errors.New("hook abort")

type AbortError struct {
    Reason string
    Err    error
}

func (e *AbortError) Error() string { return "hook abort: " + e.Reason }
func (e *AbortError) Unwrap() error { return e.Err }

func Abort(reason string) error { return &AbortError{Reason: reason, Err: ErrAbort} }
```

### 5.3 Abort 与 error 的传播矩阵

每个 hook handler 的返回值落到统一的传播规则：

| 返回值 | 行为 |
|---|---|
| `nil` | 继续派发同事件的下一个 hook；事件正常完成 |
| `errors.Is(err, ErrAbort)` | 协议级 turn 终止：跳过同事件后续 hook，发送 `event.TurnEnd{StopReason: model.StopAbort, Err: err}`，然后发送 `event.Done` 并关闭输出流；`Run` 第二返回值仍为 `nil` |
| 其它非 nil error | fatal：跳过同事件后续 hook，向输出流发送 `event.Error{Err: err}`，再发送 `event.TurnEnd{Err: err}` 和 `event.Done`，关闭输出流；`Run` 第二返回值仍为 `nil`（启动期错误才走第二返回值） |
| `context.Canceled` / `DeadlineExceeded` | 优先于以上两类；按 Go 惯例向上层传播 ctx 取消，输出流以 `event.Error` + `event.Done` 收尾 |

各事件位置上 abort 的具体效果：

| 事件 | abort 含义 |
|---|---|
| `TurnStart` | 跳过本 turn 的所有 model step / tool wave，直接进 `TurnEnd` |
| `PreModelCall` | 跳过本 step 的 provider 调用（不发请求），结束当前 step；若已无后续 step → turn 终止 |
| `PostModelCall` | 不再触发后续 tool wave 或下一个 step，turn 提前结束 |
| `PreToolCall` | 不执行该工具，向模型反馈 `content.ToolResult{IsError:true, Parts: [Text(reason)]}`，继续后续 step（用于 guardrail 反思） |
| `PostToolCall` | 不把当前 tool result 反馈给模型，turn 以 abort 收尾 |
| `TurnEnd` | abort/error 仅记录到 `TurnEnd.Err`；无法回滚已发出事件 |

### 5.4 Hook 链执行顺序与 Mutator 生命周期

- `WithHooks(a, b, c)` 等价于注入单个 `Chain(a, b, c)`：按注册顺序串行派发，前一个 hook 通过 Mutator 写入的字段对后一个可见。
- 任意一个 hook 返回非 nil error（包含 abort）会中断同事件的后续派发，按 §5.3 传播。
- Mutator 指针**仅在对应的 Pre*/Post* 派发期间有效**；handler 不得把 Mutator 指针保留到自身返回之后（例如塞进异步队列），Loop 会在派发结束后释放或失效化 Mutator 内部状态。
- Mutator 不是 goroutine-safe：Loop 保证同一事件的 hook 链在单 goroutine 内顺序调用；如果应用层在 hook handler 内部 fan-out，必须自己保证不并发触碰 Mutator。
- ctx cancellation 优先于 hook abort；Loop 在每次进入 hook 前检查 `ctx.Err()`。

### 5.5 通用规则

1. `PreModelCall` 可改写 `*model.Request`，常见场景是注入 policy 生成的系统块、追加上下文消息、限制工具 spec、调整 sampling 参数。
2. `PostModelCall` 可改写响应的 parts 与 stop reason，常见场景是过滤敏感输出或标准化 stop reason。
3. `PreToolCall` 可改写工具输入，或返回 `Abort(reason)` 阻止工具执行（参见 §5.3）。
4. `PostToolCall` 可改写 `tools.Result`，常见场景是截断超大结果；上下文压缩仍由 `compact.Compactor` 负责，hook 不承担。
5. `TurnStart` / `TurnEnd` 只作为轮次边界，不提供 Mutator；需要控制输入时应在应用输入流或前置 policy 中处理。

## 6. 应用事件总线边界

Hook 不是通用事件总线。下列事件不进入 `hook/`：

- 产品 UI notification、进度条、命令状态。
- workspace、文件系统、配置刷新、账号状态。
- 应用自定义任务、队列、channel、后台 job。
- Memory 的业务抽取任务；v1 `memory.Memory` 暴露 `Recall` / `Remember` / `Forget` 三方法（全部 variadic option），应用可在 prompt section 或 turn 后处理里调用。

应用如需事件系统，应在自身包内定义 bus，并可在 bus handler 中调用 AgentOS API。核心只保证六类 hook event 的兼容性。

## 7. 设计决策

### 为什么没有 PreCompact / RunStart / RunEnd

读者常会问为什么 sealed 六类事件里没有 compact 边界与 run 生命周期。结论：

- **Compact** 是独立扩展点 `compact.Compactor`（`Compact(ctx, msgs) -> msgs`），需要在压缩前后做指标 / 日志的应用直接包装一个 `Compactor` 即可；无需让 hook union 增加两类事件。把 compact 放进 hook 会让 `compact/` 被迫依赖 `hook/`，与依赖图里 `compact/ -> model/` 的单向约束冲突。
- **Run 生命周期**由输出流上的 `event.Done` / `event.Error` 自然表达：`Done` 在 channel 关闭前发送，对外部观察者是稳定的 run 结束信号；run 起点则是调用 `Agent.Run` 本身。再额外引入 `RunStart` / `RunEnd` 会与 channel 语义重复。
- 因此 v1 维持「sealed 六类」的最小集；任何"运行管理"语义（队列、daemon、cron、主动通知）都属于应用接入层，不进核心 hook union。

### 为什么 sealed

Hook 是核心运行时契约。sealed event 集合让默认 `llmAgent`、Hook 实现、contrib/otel 和测试都能穷尽处理生命周期边界，避免第三方事件混入后破坏顺序、错误传播或安全语义。应用扩展点应在应用 bus，而不是扩展核心 hook union。

### 为什么只用一个 `Hook` 接口

早期草案曾把"旁路观察"与"控制改写"拆为 `Observer` / `Interceptor` 两类角色。最终决定合并为单一 `Hook`：

- 两者无法在 Go 类型层面强制隔离：都接收同一组 event 与同一份 Mutator 指针，"是否调用 Mutator"只能是约定。
- 拆分会引入两套并行 helper、两组注册路径，增加 surface area 而不带来静态保证。
- 大多数实际 Hook 同时包含旁路观察（写日志、计数）与改写逻辑，硬性拆分会迫使同一关注点分裂为两个 Hook。
- 错误传播可以由 handler 自己决定：希望 best-effort 的 Hook 在内部 swallow 错误并 `return nil`；希望硬阻断的 Hook 直接 `return err` 或 `Abort(reason)`，统一走 §5.3 的传播矩阵。

需要更强分桶 / 角色管理的应用，可在自身实现里再做包装（按事件类型路由、按配置启停、异步 fan-out 等），不在核心承担。

### 为什么统一 Pre*/Post* 命名

`Pre*` / `Post*` 直接表达调用边界：进入模型、离开模型、进入工具、离开工具、轮次开始、轮次结束。命名与根包 `Agent.Run(ctx, <-chan event.Input) (<-chan event.Output, error)` 的单 Agent 执行模型一致，避免把产品级 run lifecycle 混入核心 hook。

### 控制信号的可见性

工具或 flow 编排触发的运行时控制信号（`ErrLoopExit` / `ErrHandoff`，参见 [design-tool-system.md](design-tool-system.md) §6）通过 sealed Output 帧 `event.LoopExit` / `event.Handoff` 与同源 hook 事件 `hook.LoopExit` / `hook.Handoff` 暴露给 hook handler：

- `OnLoopExit` 在 `event.LoopExit` 紧跟在 `event.ToolEnd` 后发出时同步触发，handler 通过 `Event event.LoopExit` 字段读取 `ToolID` / `ToolName` / `Escalate`；
- `OnHandoff` 在 `event.Handoff` 紧跟在 `event.ToolEnd` 后发出时同步触发，handler 通过 `Event event.Handoff` 字段读取 `Agent` / `Carry` 等；
- `PostToolCall` 仅承载工具调用本身的结果与 mutator；控制信号的语义改写（如要把 `LoopExit` 抑制掉）应在专属的 `OnLoopExit` 中完成；
- 控制信号不进入 `model.Message`（与 [design-model-provider.md](design-model-provider.md) §6 一致），因此 hook 不能也不需要从请求体里寻找它。

## 与红线对照

- r22：单一 `Hook` 接口（`Handle(ctx, Event) error`）、sealed `hook.Event`、八类核心 event（含 `LoopExit` / `Handoff`）、统一签名的泛型 helper、Hook 内部 type switch、Mutator 细粒度 setter 与 abort 语义矩阵均已覆盖。
- 根包边界：Run 位于根包 `Agent.Run(ctx, <-chan event.Input) (<-chan event.Output, error)`，根包保留 `Agent`、`NewAgent`、`Option`、`NewAgentTool`、默认 `llmAgent` 与 `WithModel` / `WithTools` / `WithSession` / `WithPolicy` / `WithHooks` / `WithCompact` / `WithPrompt` / `WithRequestBuilder` / `WithToolExecutor`。
- 相关协议：`event.Prompt` / `event.Steer`、`model.Provider` 三方法（`Name` / `Generate` / `Stream` 返回 `iter.Seq2[*model.Chunk, error]`）、独立 `model.TokenCounter`、`tools.Tool` 两方法（`Spec() ToolSpec` + `Handle(ctx, input)`，控制流通过 sentinel error 翻译为专用 Output 帧 `event.LoopExit` / `event.Handoff`）、`session.Session` 六方法 append-only（hook 通过 `session.FromContext(ctx)` 读取）、`compact.Compactor`、`memory.Memory`、`policy.Policy`、`prompt.Section` 函数类型、`NewContext` / `FromContext` 命名均按 v1 描述。

## 修订记录

- 2026-05-07：与 `design-event-agent-loop.md` / `design-model-provider.md` / `design-tool-system.md` 对齐协议形态（修正 Provider 三方法、Stream 返回 `*model.Chunk`、`tools.Tool.Spec()` 签名）；删除 event 上重复的 `SessionID` 与 `ToolName` 字段；helper 收敛为单一 `Handler[E]` 签名；Mutator API 收紧为细粒度 setter，移除 `ReplaceXxx`；新增 abort/error 传播矩阵、`WithHooks` 顺序与 Mutator 生命周期约束；§7 增补"为什么没有 PreCompact / RunStart / RunEnd"决策段；移除应用层 Registry 示例与相关概念，聚合行为由应用层用普通 `hook.Hook` 实现承担。
- 2026-05-07（简化）：取消 `Observer` / `Interceptor` 二元角色划分，合并为单一 `Hook` 概念。§3 由对照表改为短小的"使用模式"段；§5 标题与文中残余角色措辞一并去除；§7 把"为什么二元划分"决策段替换为"为什么只用一个 `Hook` 接口"；红线对照点 r22 同步更新。
- 2026-05-07（控制信号专用事件）：取消 `event.ToolEnd.Actions` / `event.TurnEnd.Actions` map[string]any 形式；新增 sealed Output 变体 `event.LoopExit` / `event.Handoff`；hook 同步新增 `hook.LoopExit` / `hook.Handoff` 两类核心 event（六类→八类），新增 `OnLoopExit` / `OnHandoff` helper；§Action 信号的可见性 → §控制信号的可见性 改写；红线 r22 同步更新。
