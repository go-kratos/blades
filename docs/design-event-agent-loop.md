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

对 pi-agent 的参考结论：TS 实现把低层 loop、状态化 `Agent` wrapper、steering/follow-up queue 分开。Go 版本不照搬 class wrapper，因为 `Run(ctx, <-chan event.Input)` 已经把状态化 wrapper 的排队能力交给 channel；但保留同一个核心边界：input queue 只负责读取和分类事件，Agent Loop 负责 Session commit 与事件输出，tool wave 负责工具执行与结果归一化。`agent_loop.go` 中的 input queue helper 因此不依赖 `session.Session`，也不直接发 output。

## 2. `content.Part` 共享叶子

通用模态定义在 `content/` 包，仅依赖 Go 标准库。`content.Part` 是 sealed marker，使用私有 `part()` 方法收口扩展点，所有变体都在 `content/` 内定义。

```go
package content

type Part interface{ part() }

type Text struct {
    Text string
}

type FilePart struct {
    URI      string
    MIME     string
    Filename string
}

type FileRefPart struct {
    ID   string
    MIME string
}

type DataPart struct {
    Bytes    []byte
    MIME     string
    Filename string
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
- `Steer`：若当前 turn 正在运行，作为 current-turn steering 在下一个 step 边界消费，追加为当前 turn 的 user message，并在下一次 model step 构建 request 时生效；不打断正在 streaming 的 provider 调用，也不打断正在执行的 tool wave。若 Run 正处于 idle wait，`Steer` 与 `Prompt` 一样开启一个新 turn。
- `Abort`：若当前 turn 正在运行，结束当前 turn，并携带人类可读原因；若 Run 正处于 idle wait，结束整个 Run。
- `Pause` / `Resume`：当前 v1 保留为输入类型但默认 `llmAgent` 暂不实现暂停语义。

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

当前 v1 只实现文本与 thinking 的 hot path delta。其他多模态 part 保留在最终 `TurnEnd.Parts` / Session message 中，不单独发出 `PartStart` / `PartDelta` / `PartEnd`。后续若需要 Blob 流式生命周期事件，应作为公开 Event 协议升级单独设计。

### 4.2 Step、工具与 Turn 生命周期

工具执行由默认 `llmAgent` 编排并以事件形式公开：

```go
type ToolStart struct {
    ID    string
    Name  string
    Input json.RawMessage
}

type ToolDelta struct {
    ID   string
    Data []byte
}

type ToolEnd struct {
    ID      string
    Name    string
    Parts   []content.Part
    IsError bool
}
```

`ToolStart` 只在默认 tool wave 实际调度某个工具调用时发出。Provider stream 中的 `content.ToolUse` 是 assistant message 的一部分，Loop 会收集它来决定是否进入 tool wave，但不会把它转换成 `ToolStart`，避免"模型提出调用"和"运行时开始处理"两类事件混淆。默认 Loop 不提供本地顺序/并行开关：同一 assistant message 中的 tool wave 固定按源顺序发出 `ToolStart`，实际 `Handle` 并发执行，`ToolEnd` 按完成顺序发出，而写回模型上下文的 `content.ToolResult` 仍保持 assistant 源顺序。若 provider 通过选项约束模型一次最多返回一个 tool use，这个 wave 自然退化为单工具调用。`ToolEnd.Parts` 直接复用 `tools.Result{Parts []content.Part}`，再包装为 `content.ToolResult` 写回模型上下文；若 policy deny / ask / error 在 `ToolStart` 后阻止了 `Tool.Handle`，对应 `ToolEnd` 会带 `IsError=true`。

工具控制信号在当前公开 API 中通过 `TurnEnd.Action` 聚合给 flow 层：

```go
type Action interface{ action() }

type LoopExit struct {
    Escalate bool
}

type Handoff struct {
    Agent string
}
```

`LoopExit` / `Handoff` 不作为独立 `event.Output` 帧发出，也不进入 `Session`。它们只来自工具返回的 `tools.ErrLoopExit` / `tools.ErrHandoff` sentinel error，由默认 tool wave 翻译成 `TurnEnd.Action`。

单 turn 结束和整个 Run 结束是两个事件：

```go
type TurnEnd struct {
    Parts      []content.Part
    StopReason StopReason
    Usage      Usage
    Err        error
    Action     Action
}

func (e TurnEnd) Text() string

type Done struct{}
```

`TurnEnd` 只在整个 turn 完成时输出一次。工具中间轮次不输出 `TurnEnd`；当前 v1 也不输出 `StepEnd`。`Done` 是整个 Run 的结束 sentinel，通常在 input channel 关闭、context 取消或 fatal error 后输出一次。

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

`blades.NewAgent(name, opts...)` 返回默认 `llmAgent`。`llmAgent` 内部持有 provider、tools、resolver、prompt、compact、policy、hooks 等依赖。Run 返回的 `<-chan event.Output` 在 `event.Done` 输出后被关闭；运行期错误以 `event.Error` 写入同一通道，仅当无法启动 Run 时通过第二返回值 `error` 抛出。

默认运行时的代码在根包内按公开入口、私有 loop、适配器三类组织，而不是暴露为用户可导入包：

- `agent.go`：`Agent` 接口、`NewAgent` 构造函数和默认 `llmAgent` 配置字段与方法（`Name` / `Description` / `Run` / tool resolve）。
- `agent_loop.go`：默认 LLM Agent 的私有运行循环；包含 `agentLoop`、input queue、turn loop、request 构建、provider stream、tool wave、Session commit 与 `Done` 输出。
- `tool.go`：`NewAgentTool` 适配器，把一个 `Agent` 暴露为 `tools.Tool`。

## 6. Run / Turn / Step / Tool Wave

默认 `llmAgent` 使用四层运行模型：

1. **Run**：长生命周期事件流，消费 input channel，顺序处理多个 turn，管理 context 取消和 `Done`。
2. **Turn**：一次用户任务，从 `Prompt` 开始，到最终 assistant 响应、abort、错误或 max steps 结束。
3. **Step**：一次 model provider 调用，包含 request 构建、stream 消费和 assistant delta 收集。
4. **Tool Wave**：同一 step 中所有 `content.ToolUse` 的执行批次，产出 `ToolStart` / `ToolEnd`，并回填下一 step 的 `content.ToolResult`。

单 turn 推荐流程：

1. 收到 `Prompt` 或 idle `Steer`，触发 `Hook.BeforeTurn`，并 append 起始 user message（一次 `Session.Append`）。
2. 构建第 0 个 model step 的 `*model.Request`。内置 request 构建按以下有序 pipeline 执行——**先 compact 再 prompt**，两段产物在最后汇合：
   ```
   snapshot := session.Messages(ctx)            // 1. 全量原始消息（append-only 快照）
   view     := compactor.Compact(ctx, snapshot) // 2. 仅作用于 messages 段；
                                                //    Compactor 自身决定短路 / 增量 / 迭代折叠
   system   := prompt.Builder.Build(ctx)        // 3. 构建 system 段；memory.Recall 在此处发生
   request  := &model.Request{
       System:   systemTextFrom(system),        // memory 召回结果在 system 内
       Messages: view,                          // compact 后的 Session 快照
       Tools, Options,
   }
   ```
   关键约束：Compactor 仅看 `Messages`、prompt builder 仅看 `System`，二者互不感知；预算分摊由应用层三段（System / Messages / Response）控制，core 不内置裁剪兜底。详见 [design-compact.md](design-compact.md) §与 Memory 的关系。
3. 触发 `Hook.BeforeModel`，调用 `model.Provider.Stream(ctx, req)`。
4. 消费 provider stream，将文本与思考增量转为 `event.Output`；多模态 part 和 tool use 保留在最终 assistant message 中，tool use 只用于触发 tool wave。
5. 触发 `Hook.AfterModel`。
6. 若存在 tool use，先按 assistant 源顺序触发 `Hook.BeforeTool` 完成输入改写；随后同一 assistant message 中的 tool wave 并发执行，并由 Agent Loop 在每次调用真正调度 / 完成时发出 `ToolStart` / `ToolEnd`；识别 `ErrLoopExit` / `ErrHandoff` sentinel，并记录到 `TurnEnd.Action`。是否允许模型一次返回多个 tool use 由 provider 选项控制。
7. 将本 step 的 `assistant` 消息与本轮 tool wave 的 `tool` 结果消息合并为一个语义组，调用一次 `Session.Append(ctx, assistantMsg, toolMsg)`。
8. 在 model step 或 tool wave 完成后的边界非阻塞消费 input：`agent_loop.go` 的 input queue helper 先分类事件；turn commit 路径再把 `Steer` 追加为当前 turn 的 user message 并进入下一 step；`Prompt` 缓存为下一 turn 的 follow-up；`Abort` 结束当前 turn；input channel close 不 abort 当前 turn。
9. 若没有 tool use 且没有 step-boundary steering，或收到 abort/error/tool action，输出 `TurnEnd`，触发 `Hook.AfterTurn`。`LoopExit` / `Handoff` 不进 `Session`（Session 只承载 `model.Message`）。

Hook 回调位置固定为六个生命周期边界：`BeforeModel` / `AfterModel`（每个 model step 前后）、`BeforeTool` / `AfterTool`（每个工具调用前后）、`BeforeTurn` / `AfterTurn`（每个 turn 前后）。详见 `design-hook-extension.md`。

## 7. 扩展点

不公开独立 `loop/` 包，也不导出 `loop.Builder` 或 `loop.Orchestrator` 这类运行时包接口。当前已实现的定制点是：

- `WithHooks(...hook.Hook)`：观察和拦截 turn / model / tool 生命周期。
- `WithPolicy(policy.Policy)`：裁决工具调用。
- `WithCompact(compact.Compactor)`：在每次 request 构建前压缩 Session 快照。
- `WithPrompt(prompt.Builder)`：构建 system prompt。
- `WithTools` / `WithToolsResolver`：配置静态或动态工具集。

`RequestBuilder`、`ToolExecutor`、`WithMaxSteps` 目前不是公开 API。需要完全特殊的运行时或工具编排时，直接实现 `blades.Agent`；未来若要开放这些局部替换点，应作为独立协议变更补充测试和文档。

## 8. Event ↔ Message 转换边界

Event 面向用户协议，Message 面向 provider 协议。二者通过 `content.Part` 共享模态叶子，但不共享顶层结构。

唯一转换边界在 `internal/convert/`：

- `event.Prompt` / `event.Steer` 转为 `model.Message{Role: model.RoleUser, Parts: ...}`。
- provider 文本响应转为 `event.TextDelta`。
- provider 思考响应转为 `event.ThinkingDelta`。
- provider 返回的 `content.ToolUse` 保留为 assistant message part，用于触发 tool wave，不直接转为 output。
- `tools.Result.Parts` 包装为 `content.ToolResult` 并复用同一 `[]content.Part`。

用户代码不应直接依赖 `internal/convert/`。需要完全不同的 runtime 时，实现 `blades.Agent`。

## 9. Session 写入规则

Session 历史只追加 protocol-only 的 `model.Message`，并以"语义组"为原子单元写入：

1. **turn 起始**：append 起始 `Prompt` 或 idle `Steer` 转换出的 user message（一次 `Append(ctx, userMsg)`）。
2. **每个 model step + tool wave 完成后**：将本 step 的 `assistant` 消息与同 step 的 `tool` 结果消息作为一组，调用一次 `Append(ctx, assistantMsg, toolMsg)`。该组写入是 step 级原子单元，避免崩溃留下"有 tool_call 但无 tool_result"的半截历史。
3. **step / tool-wave 边界输入**：active turn 中收到的 `Steer` append 为 user message，并触发同一 turn 的下一次 model step；active turn 中收到的 `Prompt` 只排队为下一 turn 的 follow-up。
4. final assistant message 完成且无新工具调用后输出 `TurnEnd`。

输入队列本身不写 Session。`nextTurnStart` / `drainStepBoundaryInputs` 只把 channel 事件分类为"开启 turn"、"current-turn steering"、"follow-up prompt"或"abort"；所有 `model.Message` 创建、hook 执行和 `Session.Append` 都集中在 `agent_loop.go` 的 turn / step commit 路径。这样不会出现 queue helper 持有 `session.Session`、又悄悄改变 transcript 的隐式副作用。

**不写回 Session 的内容**：compact view、summary、被截断的 tool result 视图、以及 `event.LoopExit` / `event.Handoff` 等运行时控制信号。控制信号只出现在 `TurnEnd.Action`。Compactor 的 rolling state 通过 `session.State()` 的私有 key（保留前缀 `__compact_*__`，参见 [design-session.md](design-session.md) §State 键命名空间）持久化，与协议历史正交，不会出现在 `Session.Messages()` 中。

Stateless mode 不读取 session history，但仍维护 turn-local transcript 以支持多 step 工具循环。Compact 只在构建 request 前运行，输入是 session 快照 + turn-local pending parts，输出必须满足 provider message invariant。

### 上下文超长的两层兜底

provider 真实 token 计费与 Compactor 的 `TokenCounter` 估算之间总会存在偏差，因此在 Compactor 自身的[迭代压缩契约](design-compact.md#迭代压缩契约)之上，Loop 再提供一层 step 间的 hint 重试：

| 层级 | 触发主体 | 触发条件 | 行为 |
|------|----------|----------|------|
| Step 内迭代 | Compactor 自身 | 当前视图估算超 `MaxTokens` | 在单次 `Compact` 调用内循环折叠批次（推进 offset / 调 Summarize LLM）直到 ① 满足预算 ② offset 抵达 `len(msgs) - KeepRecent` 无可压区 ③ 触发安全阀 |
| Step 间 hint | Agent Loop | provider 实际返回 context-too-long 类错误 | Loop 透传 `compact.WithHint(ctx, HintShrink)` 重新进入**同一 step 的第二次**请求构造；Compactor 在 hint 模式下必须返回 token 严格单调下降的视图。最大重试 1 次；仍未下降 → `event.Error` fail-fast 终止 turn |

两层是正交关系，不互相替代：step 内迭代解决"按预算逼近"，step 间 hint 解决"估算与真实账本之间的最后一公里"。Loop 不在估算阶段做任何阈值判断（保持 [触发时机](design-compact.md#触发时机) 中"Loop 无条件调用、Compactor 自适应"的契约）。

## 与红线对照

本文覆盖 r1、r3、r5、r6、r7、r8、r9、r10、r11、r12、r25、r30、r31、r32，并明确取消公开 `loop/` 包。
