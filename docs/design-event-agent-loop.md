---
type: design
title: Event 协议与内置 Agent Loop 设计
date: 2026-05-05
status: draft
parent: design-agent-framework.md
related: [design-agent-framework.md]
tags: [agentos, event, agent-loop, llm-agent, content, protocol]
---

# Event 协议与内置 Agent Loop 设计

## 1. 概述

`event/` 是 AgentOS 面向用户、应用接入、hook 与运行时的协议层。它只表达输入、输出、控制、工具生命周期和流式输出，不承载 provider 消息约束。

默认 Agent Loop 不作为公开 `loop/` 包存在。Loop 是根包默认 `llmAgent` 的内部运行机制；用户只需要理解 `blades.Agent`、`blades.NewAgent`、`event.Input` 和 `event.Output`。高级定制通过根包 options 替换局部策略，例如 request 构建、tool wave 执行和 hook；完全不同的运行时直接实现 `blades.Agent`。

Event 与 `model.Message` 不合并，转换边界集中在 `internal/convert/`。这样用户协议和 provider 协议保持独立，但通过 `content.Part` 共享同一多模态叶子。

## 2. `content.Part` 共享叶子

通用模态定义在 `content/` 包，仅依赖 Go 标准库。`content.Part` 是 sealed marker，使用私有 `part()` 方法收口扩展点，所有变体都在 `content/` 内定义。

```go
package content

type Part interface{ part() }

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

`Blob.Source` 同样是 sealed union：

```go
type BlobSource interface{ blobSource() }

type InlineBytes []byte
type URI string
type FileID string
```

`content/` 不提供统一元数据字段，也不读取二进制内容。业务扩展、二进制拉取、权限校验、缓存与传输由应用层处理。`event` / `model` / `tools` 都直接使用 `content.Part`，不再各自定义同构 Part。

## 3. Event Input 协议

`event.Input` 是 sealed marker：

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

文本构造只提供函数糖：

```go
func NewPromptText(s string) Prompt {
    return Prompt{Parts: []content.Part{content.Text{Text: s}}}
}

func NewSteerText(s string) Steer {
    return Steer{Parts: []content.Part{content.Text{Text: s}}}
}
```

输入语义固定如下：

- `Prompt`：发起一个新 turn。若当前 turn 正在运行，v1 中排队等待，不并发执行。
- `Steer`：注入当前 turn 的 pending user message，在下一次 model step 构建 request 时生效；不打断正在 streaming 的 provider 调用。
- `Abort`：结束当前 turn，并携带人类可读原因；不关闭整个 Run。
- `Pause` / `Resume`：只在 tool wave 边界生效；provider streaming 中不暂停底层连接。

`Abort` 与 `context.CancelFunc` 互补：`Abort` 是协议级 turn 控制，`context.CancelFunc` 负责结束整个 Run 调用栈和底层资源。

## 4. Event Output 协议

`event.Output` 同样是 sealed marker：

```go
type Output interface{ output() }
```

### 4.1 流式内容输出

常用文本和思考增量走 hot path 紧凑值类型：

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

两条路径不重叠：文本与思考增量使用 `TextDelta` / `ThinkingDelta`；Blob 等多模态内容使用 `PartStart` / `PartDelta` / `PartEnd`。

### 4.2 Step、工具与 Turn 生命周期

一次 turn 可以包含多个 model step。`StepEnd` 只表示一次 provider 调用结束，不代表用户 turn 完成：

```go
type StepEnd struct {
    Index      int
    StopReason model.StopReason
    Usage      model.Usage
}
```

工具执行由默认 `llmAgent` 编排并以事件形式公开：

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

// LoopExit 与 Handoff 是工具触发的运行时控制信号，作为 sealed Output 变体在
// stream 上紧跟在产生它们的 ToolEnd 帧之后。
type LoopExit struct {
    ToolID   string
    ToolName string
    Escalate bool // true 表示向外层 flow.LoopAgent 升级（以 ErrLoopEscalated 终止）
}

type Handoff struct {
    ToolID   string
    ToolName string
    Agent    string              // 目标子 Agent 名字
    Carry    *content.ToolResult // 可选的转交 payload
}
```

`ToolEnd.Result.Parts` 直接复用 `tools.Result{Parts []content.Part}`。控制信号不再放进 `ToolEnd` 字段，而是作为独立的 sealed Output 帧 `event.LoopExit` / `event.Handoff` 紧跟在 `ToolEnd` 之后；翻译规则见 [design-tool-system.md](design-tool-system.md) §6，与 `model.Message` 的隔离约束见 [design-model-provider.md](design-model-provider.md) §6。工具 recoverable error 同时出现在 `ToolEnd.Err`，并被转换成 `content.ToolResult{IsError:true}` 反馈给模型；context cancel/deadline 属于 fatal。

单 turn 结束和整个 Run 结束是两个事件：

```go
type TurnEnd struct {
    Parts      []content.Part
    StopReason model.StopReason
    Usage      model.Usage
    Err        error
}

type Done struct{}
```

`TurnEnd` 只在整个 turn 完成时输出一次。工具中间轮次不输出 `TurnEnd`，只输出 `StepEnd` 和 `Tool*`。本 turn 内出现的 `LoopExit` / `Handoff` 帧已经按发生顺序在流上呈现给消费者，`TurnEnd` 不再做"聚合"。`Done` 是整个 Run 的结束 sentinel，通常在 input channel 关闭或 context 取消后输出一次。

运行期错误作为输出事件进入同一条流：

```go
type Error struct {
    Err error
}
```

错误分类使用 Go 标准 `errors.Is` / `errors.As` 与 `event` 包内 sentinel。Run 返回的 `<-chan event.Output` 在 `event.Done` 之前可以多次输出 `event.Error`；fatal、无法继续运行的错误以 `event.Error` 形式输出后立即输出 `event.Done` 并关闭通道。仅当 Run 在启动阶段就无法建立通道（参数错误、依赖装配失败等）时，才通过 `Run` 第二返回值 `error` 直接返回。

## 5. 根包 Agent 运行接口

根包定义唯一 Agent 接口：

```go
package blades

type Agent interface {
    Name() string
    Description() string
    Run(context.Context, <-chan event.Input) (<-chan event.Output, error)
}
```

`blades.NewAgent(name, opts...)` 返回默认 `llmAgent`。`llmAgent` 内部持有 provider、tools、session、prompt、compact、policy、hooks、request builder、tool executor 等依赖。Run 返回的 `<-chan event.Output` 在 `event.Done` 输出后被关闭；运行期错误以 `event.Error` 写入同一通道，仅当无法启动 Run 时通过第二返回值 `error` 抛出。

默认运行时的代码在根包内按职责拆分，而不是暴露为用户可导入包：

- `agent_run.go`：Run 生命周期、input 消费、`Done` 输出。
- `agent_turn.go`：单 turn 执行、`Prompt` / `Steer` / `Abort` / `Pause` / `Resume` 处理。
- `agent_step.go`：一次 model step，构建 request、stream provider、收集 delta。
- `agent_tools.go`：tool wave 执行、tool result 回填。

## 6. Run / Turn / Step / Tool Wave

默认 `llmAgent` 使用四层运行模型：

1. **Run**：长生命周期事件流，消费 input channel，顺序处理多个 turn，管理 context 取消和 `Done`。
2. **Turn**：一次用户任务，从 `Prompt` 开始，到最终 assistant 响应、abort、错误或 max steps 结束。
3. **Step**：一次 model provider 调用，包含 request 构建、stream 消费、assistant delta 收集和 `StepEnd`。
4. **Tool Wave**：同一 step 中所有 `content.ToolUse` 的执行批次，产出 `ToolStart` / `ToolEnd`，并回填下一 step 的 `content.ToolResult`。

单 turn 推荐流程：

1. 收到 `Prompt`，触发 `Hook.BeforeTurn`，并 append user prompt（一次 `Session.Append`）。
2. 构建第 0 个 model step 的 `*model.Request`（通过 `RequestBuilder`：snapshot session → prompt builder → compactor → 组装 request）。
3. 触发 `Hook.BeforeModel`，调用 `model.Provider.Stream(ctx, req)`。
4. 消费 provider stream，将文本、思考、多模态 part 和 tool use 转为 `event.Output`。
5. 触发 `Hook.AfterModel`，输出 `StepEnd`。
6. 若存在 tool use，触发 `Hook.BeforeTool` / `Hook.AfterTool`，执行 tool wave；识别 `ErrLoopExit` / `ErrHandoff` sentinel，紧跟在产生它的 `ToolEnd` 帧后发出 `event.LoopExit` / `event.Handoff` Output 帧。
7. 将本 step 的 `assistant` 消息与本轮 tool wave 全部 `tool` 结果消息合并为一个语义组，调用一次 `Session.Append(ctx, assistantMsg, toolResultMsgs...)`。
8. 将 tool result 和 pending steer 注入下一 step，继续循环。
9. 若没有 tool use，或收到 abort/error/max steps、本 turn 已经发出 `LoopExit` / `Handoff`，输出 `TurnEnd`，触发 `Hook.AfterTurn`。`LoopExit` / `Handoff` 不进 `Session`（Session 只承载 `model.Message`），消费者按发生顺序在 Output 流上自行识别。

Hook 回调位置固定为六个生命周期边界：`BeforeModel` / `AfterModel`（每个 model step 前后）、`BeforeTool` / `AfterTool`（每个工具调用前后）、`BeforeTurn` / `AfterTurn`（每个 turn 前后）。详见 `design-hook-extension.md`。

## 7. 扩展点

不公开独立 `loop/` 包，也不导出 `loop.Builder` 或 `loop.Orchestrator` 这类运行时包接口。高级定制通过根包 options 替换局部策略：

```go
type RequestBuilder interface {
    Build(ctx context.Context, in RequestBuildInput) (*model.Request, error)
}

type ToolExecutor interface {
    Execute(ctx context.Context, in ToolExecuteInput) ToolExecuteOutput
}
```

推荐 options：

- `WithRequestBuilder(RequestBuilder)`：替换 prompt/session/compact/tools 到 `model.Request` 的构建策略。
- `WithToolExecutor(ToolExecutor)`：替换 tool wave 的执行策略，例如串行、并行、限流或审批。
- `WithMaxSteps(int)`：限制单 turn 内 model step 数。
- `WithHooks(...hook.Hook)`、`WithPolicy(policy.Policy)`：观察、拦截和策略决策。`hook.Hook` 是一个接口，6 个生命周期方法（`BeforeModel`/`AfterModel`/`BeforeTool`/`AfterTool`/`BeforeTurn`/`AfterTurn`）；用户嵌入 `hook.Noop` 后只重写关心的方法。多次调用 `WithHooks` 与单次传多个 `Hook` 等价，按注册顺序串行派发。详见 `design-hook-extension.md`。

完全特殊的运行时不通过公开 loop API 扩展，而是直接实现 `blades.Agent`。

## 8. Event ↔ Message 转换边界

Event 面向用户协议，Message 面向 provider 协议。二者通过 `content.Part` 共享模态叶子，但不共享顶层结构。

唯一转换边界在 `internal/convert/`：

- `event.Prompt` / `event.Steer` 转为 `model.Message{Role: model.RoleUser, Parts: ...}`。
- provider 文本响应转为 `event.TextDelta`。
- provider 思考响应转为 `event.ThinkingDelta`。
- provider 多模态响应转为 `event.PartStart` / `event.PartDelta` / `event.PartEnd`。
- `content.ToolUse` 转为工具生命周期输出。
- `tools.Result.Parts` 包装为 `content.ToolResult` 并复用同一 `[]content.Part`。

用户代码不应直接依赖 `internal/convert/`。需要改变构建或工具编排时，应替换 `RequestBuilder` 或 `ToolExecutor`；需要完全不同的 runtime 时，实现 `blades.Agent`。

## 9. Session 写入规则

Session 历史只追加 protocol-only 的 `model.Message`，并以"语义组"为原子单元写入：

1. **turn 起始**：append user prompt（一次 `Append(ctx, userMsg)`）。
2. **每个 model step + tool wave 完成后**：将本 step 的 `assistant` 消息与同 step 全部 `tool` 结果消息作为一组，调用一次 `Append(ctx, assistantMsg, toolResultMsgs...)`。该组写入是 step 级原子单元，避免崩溃留下"有 tool_call 但无 tool_result"的半截历史。
3. final assistant message 完成且无新工具调用后输出 `TurnEnd`。

**不写回 Session 的内容**：compact view、summary、被截断的 tool result 视图、以及 `event.LoopExit` / `event.Handoff` 等运行时控制信号——这些由 `RequestBuilder` 在每次构造 `*model.Request` 时按需生成（见 [design-compact.md](design-compact.md)）；控制信号仅出现在 Output 流上。Compactor 的 rolling state 通过 `session.State()` 的私有 key 持久化，与协议历史正交。

Stateless mode 不读取 session history，但仍维护 turn-local transcript 以支持多 step 工具循环。Compact 只在构建 request 前运行，输入是 session 快照 + turn-local pending parts，输出必须满足 provider message invariant。

## 与红线对照

本文覆盖 r1、r3、r5、r6、r7、r8、r9、r10、r11、r12、r25、r30、r31、r32，并明确取消公开 `loop/` 包。
