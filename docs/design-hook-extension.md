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

`hook/` 是 AgentOS core 的生命周期回调命名空间。v1 只提供一个接口 `Hook`：6 个生命周期方法（`BeforeModel` / `AfterModel` / `BeforeTool` / `AfterTool` / `BeforeTurn` / `AfterTurn`），用户嵌入 `hook.Noop` 后只重写自己关心的方法即可。改写直接修改指针入参，拦截通过返回 `Abort(reason)` 表达。无 sealed event union、无 Mutator、无 type-safe helper、无中间件包装。

Hook 只承载 Agent Loop 的稳定生命周期契约，不承载应用业务事件。配置加载、文件监听、任务队列、UI notification、workspace 事件等由应用自己的 event bus 处理。控制信号（`LoopExit` / `Handoff`）也不在 hook 范围内——它们已经是 `event.Output` 流上的 sealed 变体，应用消费 `<-chan event.Output` 即可观察。

## 2. `hook.Hook` 全部 surface

```go
package hook

import (
    "context"
    "encoding/json"
    "errors"

    "github.com/go-kratos/blades/content"
    "github.com/go-kratos/blades/event"
    "github.com/go-kratos/blades/model"
    "github.com/go-kratos/blades/tools"
)

// Hook is the lifecycle interface implemented by all hooks.
// Six methods cover the full Agent Loop boundary set; embed hook.Noop to
// inherit no-op defaults and override only the methods you care about.
type Hook interface {
    // BeforeModel runs before each model step. Mutate *req in place
    // (e.g. inject system text, append messages, narrow tool spec).
    BeforeModel(ctx context.Context, req *model.Request) error

    // AfterModel runs after each model step completes (including errors).
    // Mutate *resp in place; consume err by returning nil; or escalate by
    // returning a different error.
    AfterModel(ctx context.Context, req *model.Request, resp *model.Response, err error) error

    // BeforeTool runs before each tool invocation.
    BeforeTool(ctx context.Context, call *ToolCall) error

    // AfterTool runs after each tool invocation completes (including errors).
    AfterTool(ctx context.Context, call *ToolCall, result *tools.Result, err error) error

    // BeforeTurn runs once at the start of a turn, before any model step.
    BeforeTurn(ctx context.Context, t *Turn) error

    // AfterTurn runs once at the end of a turn (including errors).
    // summary aggregates the parts / stop reason / usage of the turn.
    AfterTurn(ctx context.Context, t *Turn, summary *TurnSummary, err error) error
}

// Noop is an embeddable zero-value type that implements Hook with no-op
// defaults for every method. Compose it into your own hook to override only
// the lifecycle points you need.
//
//   type AuditHook struct{ hook.Noop }
//   func (AuditHook) BeforeTool(ctx context.Context, call *hook.ToolCall) error { ... }
type Noop struct{}

func (Noop) BeforeModel(context.Context, *model.Request) error                            { return nil }
func (Noop) AfterModel(context.Context, *model.Request, *model.Response, error) error     { return nil }
func (Noop) BeforeTool(context.Context, *ToolCall) error                                  { return nil }
func (Noop) AfterTool(context.Context, *ToolCall, *tools.Result, error) error             { return nil }
func (Noop) BeforeTurn(context.Context, *Turn) error                                      { return nil }
func (Noop) AfterTurn(context.Context, *Turn, *TurnSummary, error) error                  { return nil }

// ToolCall is the carrier passed to BeforeTool / AfterTool.
// Mutate Input in BeforeTool to rewrite tool arguments.
type ToolCall struct {
    AgentName string
    Turn      int
    Tool      tools.Tool
    Input     json.RawMessage
}

// Turn is the carrier passed to BeforeTurn / AfterTurn.
type Turn struct {
    AgentName string
    Turn      int
    Input     event.Input
}

// TurnSummary aggregates the result of a turn for AfterTurn observers.
// Mutate Parts / StopReason in AfterTurn to redact or normalize the final
// output before it is committed to the session.
type TurnSummary struct {
    Parts      []content.Part
    StopReason model.StopReason
    Usage      *model.Usage
}

// Abort sentinel: returning Abort(reason) from any callback triggers a
// protocol-level turn abort. See §5.
var ErrAbort = errors.New("hook abort")

type AbortError struct {
    Reason string
    Err    error
}

func (e *AbortError) Error() string { return "hook abort: " + e.Reason }
func (e *AbortError) Unwrap() error { return e.Err }

func Abort(reason string) error { return &AbortError{Reason: reason, Err: ErrAbort} }
```

字段约定：

- `AgentName` / `Turn` 用于在事件流中关联一轮执行；当前会话**不**通过 carrier 字段携带，统一由 `session.FromContext(ctx)` 读取（与 framework 的 ctx 三准则一致），避免同一信息在 ctx 与 carrier 上重复。
- `*model.Request` / `*model.Response` 使用 v1 `model.Provider` 协议；Stream 路径下 Loop 在 step 完成后用 `model.Collect` 把 chunk 序列汇总为 `*model.Response` 再触发 `AfterModel`。
- `tools.Tool` 使用 v1 两方法接口；如需工具名调用 `call.Tool.Spec().Name`，carrier 不再单独保留 `ToolName` 字段。
- `event.Input` 使用 v1 文本由 `event.NewPromptText` / `event.NewSteerText` 构造，具体类型为 `event.Prompt` / `event.Steer`。
- `Parts` 直接使用 `content.Part`。
- `Before/AfterModel` 即 model **step 边界**：一个 turn 可能包含多次 step（多轮 tool call），每个 step 对应一对 `BeforeModel` / `AfterModel`，与 `event.StepEnd` 一一对应。`BeforeTurn` / `AfterTurn` 才是 turn 边界。

## 3. 注册与组合

根包提供单一 option：

```go
func WithHooks(hooks ...Hook) Option
```

多次调用 `WithHooks(...)` 与单次传多个 `Hook` 等价，按注册顺序对每个生命周期方法串行调用（先所有的 `BeforeModel`，再 provider 调用，再所有的 `AfterModel`，依此类推）。任一调用返回非 nil error → 跳过同位置剩余 hook，按 §5 传播。

> **推荐**：在大多数应用里只需要一个嵌入 `hook.Noop` 的自定义 Hook。需要把多个独立关注点（metrics / 审计 / guardrail）拆开维护时，再传多个；分桶/动态启停/异步 fan-out 等聚合行为由调用方自行在某个方法里实现。

## 4. 改写、观察、拦截

三种使用方式都用同一接口表达，Hook 自身不区分类型；嵌入 `hook.Noop` 让你只写关心的方法。

### 4.1 旁路观察

读取入参后写出指标 / 日志 / trace，不修改指针、不返回 abort。失败应在方法内部 swallow（记录后 `return nil`），避免影响 Agent Loop。

```go
type Metrics struct{ hook.Noop }

func (Metrics) AfterModel(ctx context.Context, req *model.Request, resp *model.Response, err error) error {
    recordLatency(ctx, time.Since(reqStart(ctx)), err)
    return nil
}
```

### 4.2 改写

`Before*` 直接改请求 / 工具入参，`After*` 直接改响应 / 工具结果 / turn summary。多个 hook 的修改按调用顺序合并，最后写入者覆盖（与 Go 约定一致；需要正交合并时，由 hook 自身实现追加而非整体替换）。

```go
type Guardrail struct{ hook.Noop }

func (Guardrail) BeforeModel(ctx context.Context, req *model.Request) error {
    req.System = "policy: be terse\n" + req.System
    return nil
}

func (Guardrail) AfterTool(ctx context.Context, call *hook.ToolCall, result *tools.Result, err error) error {
    if result != nil && len(result.Parts) > 32 {
        result.Parts = result.Parts[:32]
    }
    return nil
}
```

### 4.3 拦截

任意方法返回 `hook.Abort(reason)` 触发协议级 turn 中止。`Before*` 中止跳过对应调用；`After*` 中止不把结果反馈给后续步骤，turn 以 abort 收尾。`After*` 也可以"消费"上游 error（接收 err 后 `return nil`）或"升级"为别的 error。

```go
type Deny struct{ hook.Noop }

func (Deny) BeforeTool(ctx context.Context, call *hook.ToolCall) error {
    if call.Tool.Spec().Name == "shell" && containsSecret(call.Input) {
        return hook.Abort("shell denied: secret detected")
    }
    return nil
}
```

## 5. Abort 与 error 传播

每个方法的返回值落到统一规则：

| 返回值 | 行为 |
|---|---|
| `nil` | 同位置下一个 hook 继续；流程正常 |
| `errors.Is(err, ErrAbort)` | 协议级中止：跳过同位置后续 hook，发送 `event.TurnEnd{StopReason: model.StopAbort, Err: err}` 与 `event.Done`，关闭输出流；`Run` 第二返回值仍为 `nil` |
| 其它非 nil error | fatal：跳过同位置后续 hook，向输出流发送 `event.Error{Err: err}`，再发送 `event.TurnEnd{Err: err}` 与 `event.Done`，关闭输出流；`Run` 第二返回值仍为 `nil`（启动期错误才走第二返回值） |
| `context.Canceled` / `DeadlineExceeded` | 优先于以上两类；按 Go 惯例向上层传播 ctx 取消，输出流以 `event.Error` + `event.Done` 收尾 |

各位置 abort 的具体效果：

| 方法 | abort 含义 |
|---|---|
| `BeforeTurn` | 跳过本 turn 的所有 model step / tool wave，直接进 `TurnEnd` |
| `BeforeModel` | 跳过本 step 的 provider 调用（不发请求），结束当前 step；若已无后续 step → turn 终止 |
| `AfterModel` | 不再触发后续 tool wave 或下一个 step，turn 提前结束 |
| `BeforeTool` | 不执行该工具，向模型反馈 `content.ToolResult{IsError:true, Parts:[Text(reason)]}`，继续后续 step（用于 guardrail 反思） |
| `AfterTool` | 不把当前 tool result 反馈给模型，turn 以 abort 收尾 |
| `AfterTurn` | abort/error 仅记录到 `TurnEnd.Err`；无法回滚已发出事件 |

## 6. 并发与生命周期

- Loop 保证同一生命周期方法的所有 hook 在单 goroutine 内顺序调用；指针入参（`*model.Request` 等）**仅在该方法返回前有效**，handler 不得保留到方法外（例如塞进异步队列）。
- Hook 不是 goroutine-safe：如果应用层在方法内部 fan-out，必须自己保证不并发触碰指针。
- `ctx` cancellation 优先于 hook abort；Loop 在每次进入 hook 方法前检查 `ctx.Err()`。

## 7. 应用事件总线边界

Hook 不是通用事件总线。下列事件不进入 `hook/`：

- 产品 UI notification、进度条、命令状态。
- workspace、文件系统、配置刷新、账号状态。
- 应用自定义任务、队列、channel、后台 job。
- Memory 的业务抽取任务；v1 `memory.Memory` 暴露 `Recall` / `Remember` / `Forget` 三方法（全部 variadic option），应用可在 prompt section 或 turn 后处理里调用。
- 控制信号 `LoopExit` / `Handoff`：消费 `<-chan event.Output` 时按 sealed 变体观察。

应用如需事件系统，应在自身包内定义 bus，并可在 bus handler 中调用 AgentOS API。核心只保证本文档列出的六个生命周期方法的兼容性。

## 8. 设计决策

### 为什么用 `Hook` 接口 + `Noop` 嵌入，而不是 sealed event union + helper

早期草案是 sealed `hook.Event` + 8 类 event + 8 个 `On*` helper + 4 个 Mutator 细粒度 setter。问题：

- **event 一加，helper 一加**：每新增一类 event（如 `LoopExit` / `Handoff`）必须配套加 helper，公开 surface 随 event 集合线性增长。
- **Pre/Post 二元拆分**：本质同一边界被切两半，重复 carrier 与 Mutator。
- **Mutator 是显式能力对象**：请求 / 响应字段扩张时 Mutator 也要扩；并且 Loop 的"细粒度 setter + invariant 校验"在多数应用里并未真正用到。
- **手感重**：看一次设计图要理解 `Hook` 接口、`HookFunc`、`Handler[E]`、8 个 `On*`、`Chain`、4 个 Mutator、abort sentinel 与传播矩阵共十几个概念。

新设计的取舍：

- 用一个**接口**覆盖全部生命周期，6 个方法描述六个固定边界。`hook.Noop` 提供全 no-op 默认实现，用户嵌入后只重写关心的方法 —— partial impl 的体验等价于"可选回调字段"，但获得了静态接口契约、`var _ hook.Hook = (*MyHook)(nil)` 的编译期断言以及更易识别的 Go 主流惯例（`http.Handler` / 各类 codec 都是接口 + 嵌入 base 的范式）。
- 用**指针入参**表达改写，放弃 Mutator 的"字段级合并 + invariant 校验"。多 hook 的修改是顺序覆盖，需要正交合并由 hook 自身实现追加（这与 Go 标准库风格一致）。
- 把"是否拦截"折叠到方法返回值上，复用 `Abort` 三件套与传播矩阵。
- 控制信号（`LoopExit` / `Handoff`）从 hook 移除：它们不是"可拦截边界"，已有 `event.Output` 流的 sealed 变体提供观察通道。

### 为什么用接口 + Noop 嵌入而不是结构体 + 函数字段

两种形态都能表达"按需实现部分回调"。最终选接口理由：

- **类型契约**：接口让 `var _ hook.Hook = (*MyHook)(nil)` 这种编译期断言可用；新增方法在编译时立刻暴露未升级的实现（结构体形态新增字段是悄悄的 nil）。
- **方法接收者**：方法形态可以是值接收者或指针接收者，`hook.Noop` 的零值嵌入即可工作；函数字段形态强制每个回调都是闭包/方法值，在分配上稍多一层。
- **更接近 Go 主流惯例**：`http.Handler` / `database/sql.Driver` / 各类 codec 都是接口 + 嵌入 base struct 的范式；Go 用户一眼能识别。
- **Noop 可被替换为 partial base**：未来若希望在 default 实现里写非 nil 行为（例如通用的 trace span 注入），只需另写一个 `hook.Trace` 嵌入基；函数字段形态做不到这种 base 替换。

代价是 partial impl 必须 `embed Noop`；这是个一行成本，换来的类型层收益值得。

### 为什么没有 `RunStart` / `RunEnd` / `PreCompact`

- **Compact** 是独立扩展点 `compact.Compactor`（`Compact(ctx, msgs) -> msgs`）；需要在压缩前后做指标 / 日志的应用包一层 `Compactor` 即可。把 compact 放进 hook 会让 `compact/` 被迫依赖 `hook/`，与依赖图里 `compact/ -> model/` 的单向约束冲突。
- **Run 生命周期**由输出流上的 `event.Done` / `event.Error` 自然表达：`Done` 在 channel 关闭前发送；run 起点则是调用 `Agent.Run` 本身。再额外引入 `RunStart` / `RunEnd` 会与 channel 语义重复。

### 为什么 `WithHooks` 接收变长 `Hook`

- 单实例最常见；变长用于把多个独立关注点（metrics / 审计 / guardrail）解耦维护，每个关注点单独嵌入 `Noop` 实现自己关心的方法。
- 不引入 `Registry` / `Bus` / `Chain` 等聚合概念，分桶 / 动态启停 / fan-out 由用户自己在某个方法里实现。
- 多实例的"最后写入者覆盖"是显式的 Go 顺序约定，不是隐藏的 invariant。

### 关于多 Hook 的合并语义

- 同位置方法按注册顺序串行调用；任一返回非 nil error 中断同位置剩余 hook。
- 改写顺序覆盖，与 Go 通用约定一致。需要"追加而非覆盖"的关注点（如多个 hook 都想往 `req.System` 拼接文本），由各自方法内部用 `req.System += "..."` 表达。

## 与红线对照

- r22：单一 `Hook` 接口（6 个生命周期方法）+ 嵌入式 `hook.Noop` 取代旧的 sealed event union + 8 个 helper + Mutator 体系；abort 三件套与传播矩阵保留；控制信号从 hook 移除并改走 `event.Output`。
- 根包边界：Run 位于根包 `Agent.Run(ctx, <-chan event.Input) (<-chan event.Output, error)`，根包保留 `Agent`、`NewAgent`、`Option`、`NewAgentTool`、默认 `llmAgent` 与 `WithModel` / `WithTools` / `WithSession` / `WithPolicy` / `WithHooks` / `WithCompact` / `WithPrompt` / `WithRequestBuilder` / `WithToolExecutor`；`WithHooks` 签名为 `WithHooks(...hook.Hook)`。
- 相关协议：`event.Prompt` / `event.Steer`、`model.Provider` 三方法、独立 `model.TokenCounter`、`tools.Tool` 两方法（`Spec() ToolSpec` + `Handle(ctx, input)`，控制流通过 sentinel error 翻译为 `event.LoopExit` / `event.Handoff`）、`session.Session` 六方法 append-only（hook 通过 `session.FromContext(ctx)` 读取）、`compact.Compactor`、`memory.Memory`、`policy.Policy`、`prompt.Section` 函数类型、`NewContext` / `FromContext` 命名均按 v1 描述。

## 修订记录

- 2026-05-07：初版"Observer / Interceptor 二元 + Mutator + sealed Event union"。
- 2026-05-07（简化 v1）：合并为单一 `Hook` 接口；helper 收敛为 `Handler[E]` 泛型签名；Mutator 收紧为细粒度 setter；新增 abort/error 传播矩阵。
- 2026-05-07（控制信号专用事件）：新增 `event.LoopExit` / `event.Handoff` Output 变体与同源 `hook.LoopExit` / `hook.Handoff`。
- 2026-05-07（重设计 v2）：改为单一 `Hooks` 结构体 + 6 个可选回调字段（`BeforeModel` / `AfterModel` / `BeforeTool` / `AfterTool` / `BeforeTurn` / `AfterTurn`）；删除 sealed `hook.Event` union、`Hook` 接口、`HookFunc`、`Handler[E]`、8 个 `On*` helper、`Chain`、4 个 Mutator 类型与全部 setter；改写改为直接修改指针入参；控制信号 `LoopExit` / `Handoff` 从 hook 移除，由 `event.Output` 流承载；`WithHooks` 签名改为 `WithHooks(...hook.Hooks)`。
- 2026-05-07（重设计 v3）：把 `hook.Hooks` 结构体改为 `hook.Hook` 接口（6 个生命周期方法 `BeforeModel` / `AfterModel` / `BeforeTool` / `AfterTool` / `BeforeTurn` / `AfterTurn`）+ 提供 `hook.Noop` 嵌入式默认实现以支持 partial impl。`WithHooks` 签名调整为 `WithHooks(...hook.Hook)`。语义保持不变（顺序串行调用、最后写入者覆盖、abort 三件套与传播矩阵保留），但引入静态接口契约 / 编译期断言 / 与 `http.Handler` 一致的 Go 风格。

