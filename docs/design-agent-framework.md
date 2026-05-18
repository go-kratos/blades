---
type: design
title: Blades AgentOS Framework 设计蓝图
date: 2026-05-01
status: draft
author: chenzhihui
related: [reference-claude-code-agent.md, reference-pi-agent-framework.md]
tags: [agentos, agent, framework, runtime, context-management, memory, tools, policy, hooks, session, streaming]
sub-docs:
  - design-event-agent-loop.md
  - design-model-provider.md
  - design-prompt.md
  - design-compact.md
  - design-tool-system.md
  - design-hook-extension.md
  - design-session.md
  - design-policy-mode.md
  - design-agent-orchestration.md
  - design-memory.md
---

# Blades AgentOS Framework 设计蓝图

## 背景与目标

Blades 的目标是成为通用 AgentOS Core Runtime。核心层负责 Agent 事件协议、运行循环、模型上下文构建、工具编排、会话持久化、策略决策、Hook 和 Memory 等基础能力，并保持 API 面向通用 Agent 场景。

应用层负责把核心能力装配成具体产品形态，包括 CLI、HTTP、微信、飞书、调度器等 channel 接入，workspace 管理，配置加载，daemon，cron，session 映射，主动通知和第三方 SDK 集成。推荐在具体应用内使用 `cmd/<app>/internal/*` 组织这些装配代码；Coding、客服、数据分析、自动化运维、研究助手等场景通过应用、recipe、examples 或 contrib preset 承接。

当前 Blades 已有 `Agent`、基于 `iter.Seq2` 的流式接口、`Session`、`hook/`、`flow/`、`tools/`、`memory/` 和多 Provider 集成。本轮设计以当前 API 为基线，把这些能力重组为清晰分层的 AgentOS。

本文描述的是 AgentOS 目标架构；文中的 `content/`、`event/`、`model/`、`policy/`、`hook/`、`compact/` 和 `internal/convert` 等包名对应当前核心拆分。

核心目标：

- **事件驱动**：外部应用通过 `event.Input` / `event.Output` 与 Agent 通信，channel 中直接传具体事件。
- **Event / Message 分层但模态共享**：Event 是用户协议层，Message 是模型上下文协议层；两者通过 `content.Part` 共享单一 Part union，Agent Loop 是唯一转换边界。
- **根包内置默认 LLM Agent**：默认 Agent Loop 是根包 `llmAgent` 的内部运行机制，不作为公开 `loop/` 包暴露；高级定制通过 options 替换局部策略。
- **通用 AgentOS 核心**：核心提供 runtime、policy、session、tool、memory 等基础能力；channel、host、workspace 和 coding-specific workflow 由应用层承接。
- **应用层自持接入**：channel、workspace、配置、daemon、cron、外部平台 SDK 和产品交互由具体应用实现，推荐使用 `cmd/<app>/internal/*` 作为应用层样板。
- **包依赖可证明**：协议层单向依赖 `content/`；能力层 / 运行时层只允许向下依赖，不形成环。
- **Go 惯用 API**：小接口、短包名、`package.Role` 命名、`context.Context` 取消与 trace 传播、Option 函数配置、`pkg.NewContext`/`pkg.FromContext` stdlib 风格 capability helper。
- **流式优先**：Provider streaming、工具重叠执行、运行中 steering/control 都是一等能力。

## 核心结论

### Agent 接口

```go
type Agent interface {
    Name() string
    Description() string
    Run(context.Context, <-chan event.Input) (<-chan event.Output, error)
}
```

`event/` 是 Event 协议的唯一用户入口。根包不 re-export `event.Input`、`event.Output`，也不提供 `Prompt`、`Steer`、`Abort` 这类 Event 构造函数。事件中的多模态字段直接使用 `content.Part`。这样用户只需要理解一个 Event 包，避免同一类型同时出现在 `blades` 和 `event` 两个命名空间。

`blades.NewAgent(name, opts...)` 返回默认 `llmAgent`。`llmAgent` 在根包内部实现 follow-up loop / step loop / tool wave 运行模型；用户不需要导入公开 `loop/` 包。通过 `WithHooks`、`WithPolicy`、`WithCompact`、`WithPrompt` 等 options 注入扩展能力；完全不同的 runtime 直接实现 `blades.Agent`。

`event/` 构造函数提供多模态变参便利：`event.NewPrompt(parts ...any) Prompt` / `event.NewSteer(parts ...any) Steer` 接受 `string`（自动包装为 `content.Text`）以及任意 `content.Part` 实现，无需独立事件类型。

`Run` 使用统一 channel 交互：输入 channel 承载 `event.Input`，返回的只读输出 channel 承载 `event.Output`。第二返回值只表示 Agent 无法启动或无法创建输出流；一旦 `Run` 成功返回 output channel，运行期错误也通过 `event.Error` 进入同一个输出流，最终发送 `event.Done` 后关闭 channel。

```go
input <- event.NewPrompt("hello")

output, err := agent.Run(ctx, input)
if err != nil {
    return err
}

for out := range output {
    switch e := out.(type) {
    case event.TextDelta:
        // e.Text is the streamed text delta
    case event.Error:
        return e.Err
    case event.TurnEnd:
        // one model turn ended
    case event.Done:
        // agent lifecycle ended
    }
}
```

运行上下文使用 `context.Context` 传递取消、deadline、trace 和少量 typed capability。所有 capability helper 统一采用 stdlib 风格 `pkg.NewContext(ctx, x)` / `pkg.FromContext(ctx) (X, bool)`：

```go
ctx = session.NewContext(ctx, sess)

sess, ok := session.FromContext(ctx)
if ok {
    sessionID := sess.ID()
}
```

Core 保留两类 capability context helper：

- `session.NewContext` / `session.FromContext`：当前会话。
- `blades.NewContext` / `blades.FromContext`：当前 Agent 内省（Agent、parent、root）；默认 LLM Agent 的运行 context 可直接传给 `blades.Fork`。

工具自身通过 `tools.NewContext` / `tools.FromContext` 取得每次调用的 `ToolContext`（`ID` / `Spec`），由 Agent Loop 在调用 `Tool.Handle` 前注入；`ToolContext` 仅暴露调用元数据，不承载控制信号。工具签名仍为 `Handle(ctx context.Context, input json.RawMessage)`。控制流信号通过 sentinel error 返回，由 Agent Loop 翻译为 `event.TurnEnd.Action` 上的 `event.LoopExit` / `event.Handoff`。

**可入 context 三准则（硬约束）**：任何想进入核心 context 的 capability 必须**同时**满足：

1. **runtime-scoped**：随 ctx 取消而失效，不是 long-lived global state。
2. **稳定不变**：在一次 Agent 执行期间不会被替换（替换需派生新 ctx）。
3. **多层共需**：至少跨 Loop / Tool / Hook / Policy 中的两层。

不满足任一条的字段（`AppID` / `UserID` / `ChannelID` / `WorkspaceID` / chat ID / platform ID / notification target / turn ID / tool call ID 等业务标识）**必须**由应用层用自己的 context key 管理，core 不提供 helper、不接受 PR 加 helper。`TraceID` 使用 OpenTelemetry context 传播。context 中禁止放大对象、可变 map、消息历史或工具结果。

### Event 类型

```go
package event

// sealed marker：私有方法，事件变体由 event 包统一定义。
type Input  interface{ input()  }
type Output interface{ output() }

// 多模态字段使用 content.Part。
// 例如：event.Prompt.Parts、event.TurnEnd.Parts、event.ToolEnd.Parts
// 字段类型就是 content.Part / []content.Part。
```

输入事件：

| 事件 | 用途 |
|------|------|
| `Prompt` | 用户或系统发起一个新 turn |
| `Steer` | Agent 运行中注入修正、追加上下文或继续指令 |
| `Abort` / `Pause` / `Resume` | 三种独立 Control 类型；`Abort{Reason string}` 与 `context.Cancel` 互补承载终止原因 |

输出事件：

| 事件 | 用途 |
|------|------|
| `TextDelta` / `ThinkingDelta` | 文本和 thinking 的常用流式输出 |
| `ToolStart` / `ToolDelta` / `ToolEnd` | 工具执行生命周期 |
| `TurnEnd` | 单 turn 结束（含 `Parts []content.Part`、`StopReason`、token usage 汇总） |
| `Error` | 运行期错误（实现 `Output`，与其他事件同流；`event.Error{Err error}` 用 Go 标准 error，靠 `errors.Is/As` + 包内 sentinel 判断；启动期错误走 `Run` 签名第二返回值） |
| `Done` | Run 结束 sentinel；channel 关闭前发送，便于多 channel `select` 分支区分 |

输入和输出都必须支持多模态 Part。**`content.Part` 是 AgentOS 唯一的 Part union**（sealed marker：私有 `part()` 方法），定义在 `content/` 包中（仅依赖标准库），统一覆盖用户协议与 provider 协议两类变体：

- 用户多模态变体：`Text`、`FilePart`、`FileRefPart`、`DataPart`、`Thinking`。`FilePart` 表达 URI 引用，`FileRefPart` 表达 provider-managed file ID，`DataPart` 表达 inline bytes；`Thinking` 携带 `Signature []byte` 以承载 Anthropic extended thinking / OpenAI o1 reasoning 的 provider 校验签名；JSON 通过 `DataPart{MIME:"application/json"}` 或 `Text` 表达。
- Provider/工具协议变体：`ToolUse{ID, Name, Input json.RawMessage}` 与 `ToolResult{ID, Name, Parts []content.Part, IsError bool}`。

`event` 中的多模态字段、`tools.Result.Parts`、`model.Message.Parts`、`model.Chunk.Parts` 全部直接使用 `content.Part`，三个协议包不再各自定义 Part。`content/` 不引入统一 `Metadata map[string]any`——业务扩展通过应用层嵌入业务结构体实现。

文本和多模态输入都用 `event.NewPrompt(...)` / `event.NewSteer(...)` 构造函数返回 `Prompt` / `Steer`，可混合 string 与 `content.Part`（底层调用 `content.NewParts`）。流式文本/思考输出走紧凑值类型 `event.TextDelta` / `event.ThinkingDelta`（hot path，避免 interface boxing）。其他多模态 part 当前只出现在最终 `TurnEnd.Parts`、`ToolEnd.Parts` 和 Session message 中；Blob 流式生命周期事件留给后续公开协议升级。

Agent Loop 在工具结果落点做轻量包装而非全 DTO 复制：`tools.Result{Parts: []content.Part}` → `event.ToolEnd.Parts` 直接复用同一切片，→ `model.Message.Parts` 中追加 `content.ToolResult{Parts: ...}` 同样直接复用。

Event 和 Message 不合并。原因：

- Event 面向用户、应用接入、hook 和 runtime，包含 streaming、control、tool lifecycle。
- Message 面向 LLM provider、session、compression，必须满足 provider message invariant。
- 两者变化频率不同，合并会导致 Event 层依赖 model 层，并把 provider 约束泄漏到用户 API。
- Agent Loop 是自然转换边界：`event.Input -> model.Message + []content.Part -> Provider -> event.Output`。

通用模态与工具协议变体共享同一 `content.Part` union：event 字段、`tools.Result.Parts`、`model.Message.Parts` 直接使用 `content.Part`，避免重复定义同构 DTO。

## 总体架构

```
┌─────────────────────────────────────────────────────────────────┐
│  Application Host（outside core / user-owned）                    │
│    channel / workspace / config / daemon / cron / platform SDK    │
│    CLI、HTTP、微信、飞书、调度器等外部接入与产品交互                 │
├─────────────────────────────────────────────────────────────────┤
│  Agent Interface（blades 根包：最小用户入口）                      │
│    Agent / NewAgent / Option / Runner                             │
├─────────────────────────────────────────────────────────────────┤
│  Execution Kernel                                                 │
│    llmAgent 默认 follow-up loop / step loop / tool wave 执行循环    │
│    flow/ 组合原语                                                   │
├─────────────────────────────────────────────────────────────────┤
│  Extension Layer                                                  │
│    tools / policy / hook / compact / memory / session / prompt    │
│    可插拔工具、策略、Hook、压缩、记忆、会话与 Prompt 构建能力         │
├─────────────────────────────────────────────────────────────────┤
│  Protocol Foundation                                              │
│    content / event / model / internal/convert                      │
│    内容协议、事件协议、模型协议与 Event ↔ Message 私有转换边界       │
└─────────────────────────────────────────────────────────────────┘
```

主链路是：Application Host 把外部输入转成 `event.Input`，通过 Agent Interface 进入 Execution Kernel；Kernel 驱动模型调用、工具执行、会话推进和流式输出，并在执行过程中调用 Extension Layer；Protocol Foundation 承载内容协议、事件协议、模型协议和 Event ↔ Message 私有转换边界。`contrib/openai`、`contrib/anthropic`、`contrib/gemini`、`contrib/mcp`、`contrib/otel` 和 preset 是基于 Protocol Foundation 与 Extension Layer 的集成包，不属于核心主链路。`evaluator/`、`recipe/` 是可选系统，不放入最小 Agent runtime 主链路。

## 设计原则

| 原则 | 决策 |
|------|------|
| 根包极简 | `blades/` 放 `Agent`、`NewAgent`、Option、默认 `llmAgent`、必要错误、`Runner` helper 和 `NewAgentTool` |
| 协议叶子互独立 | `content/`、`event/` 之间禁止形成循环；`model/` 单向依赖 `tools/`（ToolSpec）；`tools/` 单向依赖 `content/` |
| 多模态共享叶子 | `content/` 仅依赖标准库；`Part` 为 sealed marker（私有 `part()`）；变体 = Text/FilePart/FileRefPart/DataPart/Thinking/ToolUse/ToolResult；Thinking 含 Signature |
| Provider 协议 sealed | 三处 sealed 例外全部封闭：`content.Part`（私有 `part()`）、`event.Input`（私有 `input()`）、`event.Output`（私有 `output()`）。核心协议层无开放扩展接口；后台回流走 `event.Prompt`，应用业务事件由应用自己的 channel / event bus 承载。`hook/` 不再使用 sealed event union，改为单 `Hook` 接口（6 个生命周期方法）+ `hook.Noop` 嵌入式默认实现（详见 `design-hook-extension.md`） |
| ToolSpec 定义在 tools/ | `tools.ToolSpec` 是唯一定义点；`model.Request.Tools` 直接使用 `[]tools.ToolSpec`；`model/` 单向依赖 `tools/` |
| Runtime 根包内置 | 默认 Agent Loop 是根包 `llmAgent` 的内部机制，不暴露公开 `loop/` 包；run/turn/step/tool wave 私有控制流集中在 `agent_loop.go` |
| 无 maxSteps | Agent Loop 不设步数上限，靠 model stop reason（无 tool calls）+ `TurnEnd.Action` 工具控制信号（LoopExit/Handoff）+ ctx 取消 + Abort 事件终止循环 |
| 唯一私有转换 | 仅 `internal/convert/` 持 Event ↔ Message 转换函数；用户不得绕过 Loop 直接转换 |
| 不显式 FSM 枚举 | `llmAgent` 内部用 follow-up loop / step loop / tool wave 顺序代码 + 行为事件 hook 表达流程；不导出 `State` 枚举 |
| Context 三准则 | 入 ctx 的 capability 必须满足：runtime-scoped + Run 内不变 + 多层共需；否则下沉应用层 context key |
| Context 命名统一 | `pkg.NewContext(ctx, x)` / `pkg.FromContext(ctx) (X, bool)` stdlib 风格；`session/tools` 两处 helper 同形 |
| Policy 单边界 | `policy/` 单向依赖 `tools/`（不依赖 `event/model/content`）；v1 唯一请求是 `policy.ToolRequest{Tool tools.Tool, Input json.RawMessage}`；`Policy.Check(ctx, ToolRequest) Decision` 单方法接口；不引入模型请求/资源请求等 sealed union。模型预算与速率限制由 hook + 应用层组合实现 |
| Prompt 独立 | system prompt 构建放在 `prompt/`，根包不导出 `PromptBuilder` |
| Composition 不污染根包 | `flow.NewSequentialAgent`/`NewParallelAgent`/`NewLoopAgent`/`NewRoutingAgent`/`NewDeepAgent` 放 `flow/`，读作 `flow.NewParallelAgent(cfg)`；Agent→Tool 适配器 `NewAgentTool` 在根包 |
| 应用接入框架外实现 | CLI/HTTP/WebSocket/Slack/Scheduler 等属于具体应用，不作为 AgentOS 核心公开包 |
| Coding 不是核心 | `Explore/Plan/General/Verify` 不进 v1 核心；可放 examples、contrib preset 或业务 app |

## 包结构

```
blades/
├── agent.go                    Agent 接口 + NewAgent + llmAgent 字段与方法
├── agent_loop.go               默认 LLM Agent 私有 loop：agentLoop/input queue/turn/step/tool wave/Session commit
├── context_window.go           ContextWindow + BudgetError + ctx helper
├── context_builder.go          默认 Agent 私有 request-view 装配：session + compact + prompt + tools + budget stats
├── option.go                   AgentOption + WithModel/WithTools/WithPolicy/WithHooks/WithCompact/WithContextBudget/WithTokenCounter/WithPrompt/WithDescription/WithToolsResolver
├── tool.go                     NewAgentTool(Agent) tools.Tool —— 把 Agent 暴露为工具的根包适配器
├── runner.go                   `Runner` / `Result` + `NewRunner` / `RunnerOption`
├── errors.go
│
├── content/
│   ├── part.go                 Part 接口（sealed marker：私有 part()）
│   ├── text.go                 Text{Text string}
│   ├── blob.go                 FilePart{URI, MIME, Filename}, FileRefPart{ID, MIME}, DataPart{Bytes, MIME, Filename}
│   ├── thinking.go             Thinking{Text string, Signature []byte}
│   └── tool.go                 ToolUse{ID, Name, Input json.RawMessage}, ToolResult{ID, Name, Parts []Part, IsError bool}
│
├── event/
│   ├── event.go                Input（sealed：input()）, Output（sealed：output()）
│   ├── control.go              Abort{Reason}, Pause{}, Resume{}（Input）
│   ├── action.go               Action marker；LoopExit{Escalate}, Handoff{Agent}（作为 TurnEnd.Action）
│   ├── input.go                Prompt{Parts []content.Part}, Steer{Parts []content.Part}, NewPrompt/NewSteer 构造函数
│   ├── stream.go               TextDelta/ThinkingDelta（hot path，紧凑值类型）
│   ├── tool.go                 ToolStart{ID, Name, Input}, ToolDelta{ID, Data}, ToolEnd{ID, Name, Parts, IsError}
│   └── terminal.go             TurnEnd{Parts, StopReason, Usage, Err, Action}, Error{Err}, Done{}；StopReason/Usage 类型
│
├── model/
│   ├── message.go              Message{Role, Parts []content.Part}, type Role string（RoleUser/RoleAssistant/RoleTool）
│   ├── option.go               Option sealed interface + 内置 CacheHint/ReasoningEffort/ResponseFormat/Sampling/ParallelToolCalls；MergeOptions(defaults, request)
│   ├── provider.go             Provider 接口（Name + Generate + Stream iter.Seq2）；EmbeddingProvider 平级独立接口
│   ├── token.go                TokenCounter + TokenCount + ApproxTokenCounter request-level 计数能力
│   ├── request.go              Request{Model, System string, Messages, Tools []tools.ToolSpec, Options []Option}
│   ├── response.go             Response{Message *Message, StopReason, Usage}, Chunk{Parts []content.Part, StopReason, Usage *Usage}
│   ├── usage.go                Usage{InputTokens, OutputTokens int64}, StopReason 常量
│   └── collect.go              Collect(iter.Seq2[*Chunk, error]) (*Response, error)
│
├── tools/
│   ├── tool.go                 Tool 接口（Spec() ToolSpec + Handle(ctx, json.RawMessage) (*Result, error)）；Result{Parts []content.Part}
│   ├── spec.go                 ToolSpec{Name, Description, InputSchema, OutputSchema}（唯一定义点，model/ 依赖此类型）
│   ├── resolver.go             Resolver（List + Resolve）
│   ├── filter.go               ToolFilter + AllowOnly/Disallow/And/Or
│   ├── context.go              ToolContext（ID + Spec）+ NewContext / FromContext
│   └── errors.go               ErrLoopExit / ErrHandoff sentinel errors
│
├── prompt/
│   ├── prompt.go               Builder 接口 + Section 函数类型 + New(sections...) Builder
│   └── section.go              内置 Section 工厂：Static / Text / Memory
│
├── flow/
│   ├── sequential.go           NewSequentialAgent(SequentialConfig) blades.Agent
│   ├── parallel.go             NewParallelAgent(ParallelConfig) blades.Agent
│   ├── loop.go                 NewLoopAgent(LoopConfig) blades.Agent
│   ├── routing.go              NewRoutingAgent(RoutingConfig) (blades.Agent, error)
│   ├── deep.go                 NewDeepAgent(DeepConfig) (blades.Agent, error)
│   └── errors.go
│
├── session/
│   ├── session.go              Session 接口（6 方法：ID/Metadata/State/SetState/Append/Messages）+ NewSession + SessionOption + inMemorySession
│   └── context.go              NewContext / FromContext / Ensure
│
├── compact/
│   ├── compact.go              Compactor 接口 + Chain + WithTokenCounter
│   ├── window.go               NewWindow(opts...)
│   ├── budget.go               NewToolResultBudget(maxBytes)
│   ├── summary.go              NewBlockSummarize(opts...)
│   ├── summarizer.go           NewModelSummarizer(provider, opts...) + SummarizerOption
│   └── hint.go                 WithHint / HintShrink / GetHint
│
├── hook/
│   ├── hook.go                 Hook 接口（6 方法）+ Noop 嵌入式默认 + ToolCall/Turn/TurnSummary carrier
│   └── errors.go               ErrAbort / AbortError / Abort() / IsAbort()
│
├── policy/
│   ├── policy.go               Policy 接口（Check 单方法）+ Decision + Action + ToolRequest
│   └── builtin.go              Chain / AllowAll / DenyAll / Budget / RateLimit / SafetyCheck
│
├── memory/
│   ├── memory.go               Memory 接口（Recall + Remember + Forget）+ Entry + Query
│   └── inmemory.go             in-memory Memory 实现
│
├── internal/
│   └── convert/                Event ↔ Message 唯一私有转换（PromptToMessage / SteerToMessage / ToolResultToMessage / ChunkToOutputs / ResponseToTurnEnd）
└── contrib/                    provider/preset/observability 集成
```

### Runner 边界

`blades.Runner` 是根包提供的 **Agent 运行 helper 类型**，封装 input channel 装配、输出流收敛、错误 fan-in 和 `context.Context` 取消传播；它不改变 `blades.Agent` 协议，也不引入额外的运行管理语义。任何 `blades.Agent` 都能被 `blades.NewRunner(agent, opts...)` 包装。

```go
type Runner struct{ /* unexported */ }

type Result struct {
    event.TurnEnd
}

func (r Result) Text() string

func NewRunner(agent Agent, opts ...RunnerOption) *Runner

// 同步：内部用一次性 input channel 投递 in、消费输出直到 output channel 关闭，
// 返回最终 Result（最后一个 event.TurnEnd）。
func (r *Runner) Run(ctx context.Context, in event.Input) (Result, error)

// 流式：把单个 in 装入只读 channel 转发给 Agent，原样返回输出 channel；
// Agent 发出 event.Done 后 channel 关闭。
func (r *Runner) RunStream(ctx context.Context, in event.Input) (<-chan event.Output, error)

// 双向：调用方持续推送 Prompt / Steer / Abort / Pause / Resume，
// 输出 channel 实时返回 event.Output，调用方负责 close 输入 channel。
func (r *Runner) RunLive(ctx context.Context, in <-chan event.Input) (<-chan event.Output, error)
```

**三方法语义**：

- `Run` 适用于"输入一次、拿到最终结果"的 RPC 场景；内部消费 `<-chan event.Output>` 到 channel 关闭，记录最后一个 `event.TurnEnd`，首个 `event.Error` 会被拆出为 Go `error` 返回。最终返回 `Result`，它嵌入最终 `event.TurnEnd`；`event.TurnEnd` 和 `Result` 都提供 `Text()` 便捷访问。
- `RunStream` 适用于服务端推送 / SSE / CLI 实时渲染；运行期 `event.Error` 不被拆包，由调用方在 channel 中自行处理。
- `RunLive` 适用于交互式界面、长连接和 steering 场景；input channel 关闭表示"无更多输入"：若当前没有 active turn，Run 正常结束；若当前 turn 正在执行，则不 abort 当前 turn，待 active turn 和已排队 prompt 处理完后发出 `event.Done`。需要立即取消底层调用时使用 `ctx.Done()`；需要结束当前 turn 但继续处理后续队列时使用 `event.Abort`。

**错误传播**：`Run` 的第二返回值承载无法启动的错误、运行期 `event.Error` 以及没有最终 turn 时的 `ErrNoResult`。`RunStream` / `RunLive` 的第二返回值仅承载"无法启动"的错误（例如 Agent 自身拒绝 Run、依赖未装配）；一旦返回 output channel，运行期错误统一作为 `event.Error` 进入同一输出流，并在最终 `event.Done` 后关闭 channel。

**与 Agent 接口的关系**：Runner 是 channel I/O 的便利封装，不是协议层组件。`flow/` 等组合层应继续直接消费 `Agent.Run` 的 channel，**不要**经由 Runner 嵌套——避免双层 channel 转发与多余的 goroutine 拷贝。

run manager 语义（run ID、队列、daemon、cron、后台 job、主动通知、channel adapter、workspace 映射、配置加载、session 映射）一律不在 Runner 也不在核心，属于应用接入层。

### 为什么不用根包放组合原语

`Sequential`、`Parallel`、`Loop`、`Routing`、`Deep` 是通用组合能力，但不是所有用户创建 Agent 都需要。放在根包会让根包承担运行时编排语义，并和 `AgentOption`、基础构造 API 混在一起。保留 `flow/` 更符合 Go 的包边界：

- `blades.Agent` 是最小接口。
- `flow.NewSequentialAgent` 等读作组合领域的构造函数。
- `flow/` 可以依赖根包，根包不依赖 `flow/`，无循环。
- `flow/` 保留五件套：`NewSequentialAgent` / `NewParallelAgent` / `NewLoopAgent` / `NewRoutingAgent` / `NewDeepAgent`。Agent→Tool 适配器 `NewAgentTool` 不在 `flow/`，由根包 `tool.go` 提供（`blades.NewAgentTool`）。

### 根包 Agent Loop vs flow/ 边界

容易混淆，明确区分：

- **根包 `llmAgent`**：*单个* Agent 内部的运行循环（follow-up loop → step loop → tool wave）。无公开 `loop/` 包，`llmAgent` receiver 方法统一放在 `agent.go`，私有执行对象 `agentLoop` 的控制流集中在 `agent_loop.go`。
- **`flow/`**：*多个* Agent 之间的组合（Sequential / Parallel / Loop / Routing / Deep）。`flow.NewLoopAgent` 是把另一个 Agent 反复调用，不是单 Agent 内部的 provider/tool 循环。
- **根包 `blades.NewAgentTool`**：把一个 `blades.Agent` 暴露为 `tools.Tool`，让另一个 Agent 通过工具调用启用它；属于"Agent 是顶层 first-class 概念"语义，故落在根包 `tool.go` 而非 `flow/`。签名为 `NewAgentTool(agent Agent) tools.Tool`。

### agents/ 包的取舍

`agents.Explore()`、`agents.Plan()`、`agents.General()`、`agents.Verify()` 是 Coding Agent 语境下的预设。AgentOS v1 核心不保留 `agents/` 包，原因：

- 命名过宽，容易让用户误以为框架内置了固定角色体系。
- Explore/Plan/Verify 都强依赖软件工程场景，不适合作为通用 AgentOS 默认能力。
- 预设 Agent 应该由应用、recipe、examples 或 contrib preset 提供。

推荐替代：

- `examples/coding/`：放 coding app 示例，包含 Explore/Plan/Verify 的 recipe。
- `contrib/preset/`：可选预设包，提供 `preset.Assistant`、`preset.Researcher` 等通用模板。
- 业务项目自己定义 package，例如 `support.Agent()`、`ops.Agent()`、`coding.Explore()`。

## 依赖关系

```
content/    -> standard library only
event/      -> content/
tools/      -> content/ + jsonschema                    // ToolSpec 定义在此；Result.Parts: []content.Part
model/      -> content/ + tools/                       // Request.Tools []tools.ToolSpec

session/    -> model/
compact/    -> model/, content/, session/                // Compactor + provider-direct Summarizer
memory/     -> content/                                 // Memory.Recall 返回 []content.Part
prompt/     -> content/, memory/                        // Memory section 引用
hook/       -> content/, event/, model/, tools/
policy/     -> tools/                                   // v1 单边界 ToolRequest

flow/       -> blades/, event/

internal/convert/ -> content/, event/, model/

blades/     -> event/, content/, model/, tools/, session/, compact/, hook/, policy/, prompt/, internal/convert/

contrib/*   -> model/ 或 tools/
```

循环依赖规避规则：

- `content/` 是最底层叶子，**硬约束仅依赖标准库**（`go list -deps ./content/...` 不得出现非 stdlib 包）；任何包都可单向依赖它。
- `tools/` 依赖 `content/` + `jsonschema`；`ToolSpec` 定义在 `tools/` 中。
- `model/` 依赖 `content/` + `tools/`（仅 `tools.ToolSpec`）。`event/` 和 `model/` 互不依赖。
- `policy/` 单向依赖 `tools/`，v1 仅 `ToolRequest`。
- `compact/` 不依赖 root Agent；摘要能力通过 `Summarizer` 注入，内置 `NewModelSummarizer` 直接调用 `model.Provider.Generate`，不运行 Agent loop。
- `memory/` 依赖 `content/`（`Recall` 返回 `[]content.Part`），不依赖 `model/`。
- 根包默认 `llmAgent` 是运行时汇聚层，依赖所有协议与能力包；`internal/convert/` 是 `llmAgent` 内部专用的 Event ↔ Message 转换实现，不导出给用户。

## 核心包导出类型

| 包 | 核心类型 | 示例 |
|----|----------|------|
| `blades` (root) | `Agent`, `RunningAgent`, `NewAgent`, `AgentOption`, `NewAgentTool`, `NewContext`/`FromContext`, `ContextWindow`, `BudgetError`, `ContextWindowFrom`, `Runner`/`Result`/`NewRunner`/`RunnerOption`（`Run`/`RunStream`/`RunLive`）, `WithModel`/`WithTools`/`WithToolsResolver`/`WithPolicy`/`WithHooks`/`WithCompact`/`WithContextBudget`/`WithTokenCounter`/`WithPrompt`/`WithDescription` | `blades.Agent` |
| `content` | `Part`（sealed marker：私有 `part()`），`Text`，`TextFromParts`，`NewParts(inputs ...any) []Part`，`FilePart{URI, MIME, Filename}`，`FileRefPart{ID, MIME}`，`DataPart{Bytes, MIME, Filename}`，`Thinking{Text, Signature []byte}`，`ToolUse{ID, Name, Input}`，`ToolResult{ID, Name, Parts, IsError}` | `content.NewParts("hi", content.FilePart{...})` |
| `event` | `Input`（sealed：`input()`）, `Output`（sealed：`output()`）, `Prompt`, `Steer`, `Abort{Reason}`, `Pause`, `Resume`, `TextDelta`, `ThinkingDelta`, `ToolStart`, `ToolDelta`, `ToolEnd`, `Action`, `LoopExit{Escalate}`, `Handoff{Agent}`, `TurnEnd`（含 `Text()`）, `Error`, `Done`, `StopReason`, `Usage`；构造糖：`NewPrompt`, `NewSteer` | `event.NewPrompt("hi", content.DataPart{...})` |
| `model` | `Message{Role, Parts []content.Part}`, `Role`, `RoleUser`/`RoleAssistant`/`RoleTool`, `Provider`(Name+Generate+Stream `iter.Seq2`), `TokenCounter`/`TokenCount`/`ApproxTokenCounter`, `EmbeddingProvider`, `Request{Model, System, Messages, Tools []tools.ToolSpec, Options}`, `Response{Message, StopReason, Usage}`, `Chunk{Parts, StopReason, Usage}`, `Option` sealed（`CacheHint`/`ReasoningEffort`/`ResponseFormat`/`Sampling`/`ParallelToolCalls`）, `Usage`, `StopReason`, `Collect`, `MergeOptions` | `model.Provider` |
| `tools` | `Tool`(Spec+Handle 两方法), `ToolSpec{Name, Description, InputSchema, OutputSchema}`, `Result{Parts []content.Part}`, `Resolver`(List+Resolve), `ToolFilter`, `ToolContext`(ID+Spec), `NewContext`/`FromContext`, `ErrLoopExit`/`ErrHandoff` | `tools.Tool` |
| `prompt` | `Builder`(接口), `Section`(函数类型), `Static`/`Dynamic`/`System`/`Memory` 工厂, `New` | `prompt.Builder` |
| `flow` | `NewSequentialAgent`/`NewParallelAgent`/`NewLoopAgent`/`NewRoutingAgent`/`NewDeepAgent` 与对应 `*Config` 结构体 | `flow.NewParallelAgent(cfg)` |
| `session` | `Session`(6 方法 append-only), `NewSession`, `WithSessionID`/`WithMessages`/`WithMetadata`/`WithState`, `NewContext`/`FromContext`/`Ensure` | `session.Session` |
| `policy` | `Policy`(Check 单方法), `Decision`, `Action`(Allow/Deny/Ask/Modify), `ToolRequest`, `Chain`/`AllowAll`/`DenyAll`/`Budget`/`RateLimit`/`SafetyCheck` | `policy.Policy` |
| `hook` | `Hook`（6 方法），`Noop`，carrier `ToolCall`/`Turn`/`TurnSummary`，`Abort`/`ErrAbort`/`AbortError`/`IsAbort` | `hook.Hook` |
| `compact` | `Compactor`(单方法接口), `Chain`, `NewWindow`, `NewToolResultBudget`, `NewBlockSummarize`, `NewModelSummarizer`, `Summarizer`, `WithTokenCounter`, `WithHint`/`HintShrink`/`GetHint` | `compact.Compactor` |
| `memory` | `Memory`(Recall+Remember+Forget), `Entry`, `Query`, `NewInMemory` | `memory.Memory` |

## 模块详细设计

### Event 系统与 Agent Loop

Event 是用户协议层，定义 `event.Input` / `event.Output`，多模态字段直接使用 `content.Part`。Event 不和 `model.Message` 共享 Go 类型，但通过 `content.Part` 实现"零样板模态共享"。Agent Loop 在根包默认 `llmAgent` 内部实现，使用 follow-up loop / step loop / tool wave 顺序代码 + 行为事件 hook 表达流程（不导出 FSM 状态枚举），负责：

- 把 `event.Prompt` / `event.Steer` 转成 `model.Message`（通过 `internal/convert`）。
- 从 Session + `prompt.Builder` + ToolSpec + Compact 构建 `model.Request`。
- 调用 `model.Provider.Stream`。
- 把 `model.Chunk` 中的文本和 thinking part 转成 `event.TextDelta` / `event.ThinkingDelta`。
- 执行 tool wave：`BeforeTool` 做源顺序预处理；随后 `event.ToolStart` 按源顺序发出，policy check + Handle 并发执行，`AfterTool` / `event.ToolEnd` 按完成顺序发出；最终 `content.ToolResult` 按 assistant 源顺序追加到 Session。若希望模型一次最多返回一个工具调用，在 provider 构造时关闭 parallel tool calls。
- 每个 model step / tool wave 边界非阻塞 drain active input：`agent_loop.go` 的 input queue helper 只分类 channel 事件；`Steer` 由同一文件中的 turn commit 路径写入当前 turn 下一步，`Prompt` 排队为下一 turn 的 follow-up，`Abort` 结束当前 turn。
- turn 闲置时 blocking wait follow-up：`Prompt` 或 idle `Steer` 开启新 turn；input close 正常结束 Run。

唯一的 Event ↔ Message 转换函数留在 `internal/convert/`，用户不得绕过 Loop 直接调用。

详细定义见 [design-event-agent-loop.md](design-event-agent-loop.md)。

### Agent Runtime

Runtime 包括根包 `blades/` 与 `flow/`。

`blades.NewAgent(name, opts...)` 创建默认 `llmAgent`。Agent 持有 model provider、tools、resolver、policy、hooks、compactor、prompt builder 等配置，全部通过 Option 注入。Memory 不在根 Agent 内置（由应用层通过 `prompt.Memory` section 注入）。**根包绑定默认 LLM Agent 执行语义**，loop/step/tool wave 私有控制流集中在 `agent_loop.go`；完全不同的 runtime 直接实现 `blades.Agent` 接口。

`flow.NewSequentialAgent` / `NewParallelAgent` / `NewLoopAgent` / `NewRoutingAgent` / `NewDeepAgent` 接受对应的 `*Config` 并返回普通 `blades.Agent`：

```go
pipeline  := flow.NewSequentialAgent(flow.SequentialConfig{SubAgents: []blades.Agent{researcher, planner, executor}})
race      := flow.NewParallelAgent(flow.ParallelConfig{SubAgents: []blades.Agent{indexSearch, vectorSearch, webSearch}})
iterative := flow.NewLoopAgent(flow.LoopConfig{SubAgents: []blades.Agent{worker}, MaxIterations: 8})
```

需要把一个 Agent 当作工具供另一 Agent 调用时，使用根包 `blades.NewAgentTool(agent)`（不在 `flow/`）。

需要把 Agent 暴露为同步 RPC、单输入流式或双向 live 调用时，使用根包 `blades.NewRunner(agent, opts...)`，通过 `Run` / `RunStream` / `RunLive` 承接。Runner 是 channel I/O 的便利封装，不改变 Agent 协议；`flow/` 等组合层仍直接消费 Agent channel，不经由 Runner 嵌套。

组合原语只组合 `event.Input` / `event.Output` channel，不读取 `model.Message`。

### 应用接入层（框架外）

AgentOS 核心不提供公开 `channel/`、`host/` 或 `app/` 包。应用层负责把外部协议、配置、运行生命周期和资源治理装配到核心 Agent API 上。推荐模式是在 `cmd/<app>/internal/channel` 这类应用内部包中定义小接口，把 CLI、微信、飞书等外部消息转成一次 turn，再把 `event.Output` 或当前流式消息写回目标界面。

应用层通常负责：

- 配置文件、环境变量、默认值合并和依赖装配。
- CLI、HTTP、WebSocket、Slack、微信、飞书、Cron、Queue 等 transport 适配。
- session ID 映射、slash command、消息去重、主动通知、daemon 生命周期。
- 工作目录、文件系统边界、artifact、第三方 SDK 和资源治理。

配置不单独设计 `settings/` 包。配置结构和加载逻辑属于具体应用，例如 `cmd/<app>/internal/config`、`cmd/<app>/internal/app` 和 `cmd/<app>/internal/workspace`。核心只消费已经构造好的 Agent、Provider、Tool、Session、Policy、Hook 等依赖。

Agent 不知道自己来自 CLI、HTTP 还是消息平台；它只读 input channel 和 typed capability context。当前会话通过 `session.Session` 访问，应用级 channel、workspace、chat、user 等标识由应用层维护。

### Policy 系统

原 `permission/` 命名过窄。AgentOS 需要统一处理工具裁决（含权限、安全、预算、速率限制等组合策略），核心包命名为 `policy/`。

`policy/` 在 v1 仅承担**工具调用裁决**这一单边界：唯一请求是 `ToolRequest{Tool tools.Tool, Input json.RawMessage}`，`Policy` 是单方法接口 `Check(ctx, ToolRequest) Decision`；`Chain`/`Budget`/`RateLimit`/`SafetyCheck` 等内置实现都是返回 `Policy` 的工厂函数。`Decision.Action` 含 `Allow / Deny / Ask / Modify`。模型预算与速率限制等不属于工具裁决的策略由 hook 与应用层组合实现，不在 policy 协议层枚举（不引入模型请求/资源请求等 sealed union）。`policy/` 单向依赖 `tools/`，不依赖 `event/model/content`，避免协议环。Plan Mode、Accept Edits、Auto Mode 不作为 AgentOS 核心目标；它们是产品交互策略，应放在具体应用、examples 或 contrib 包中，基于 core policy primitives 组合实现。

### 子 Agent 与 Multi-Agent

v1 核心保留五个组合原语 + 一个 Agent→Tool 适配器：

- `flow.NewSequentialAgent` / `flow.NewParallelAgent` / `flow.NewLoopAgent` / `flow.NewRoutingAgent` / `flow.NewDeepAgent`：同进程 Agent 组合。
- `blades.NewAgentTool(agent)`：把一个 Agent 暴露为 `tools.Tool`，让另一个 Agent 调用；位于**根包 `tool.go`** 而非 `flow/`。签名 `NewAgentTool(agent Agent) tools.Tool`，无 options。

不在核心内置 `BackgroundAgent`、`WorktreeAgent`、`team/Coordinator` 或 `Swarm/Team`。这些能力可以在应用层、`recipe/` 或 contrib 包中组合出来。原因：

- Background 是运行生命周期问题，应由应用层 run manager 管理，而不是改变 Agent 类型。
- Worktree 是 coding workspace 隔离策略，不适合通用 AgentOS 核心。
- Team/Swarm 是应用级协作协议，应该建立在 Agent、Tool、Session 和应用层调度之上。

如果后续需要通用多 Agent，可以新增 `orchestrator/` 包，命名为 `orchestrator.Coordinator`、`orchestrator.Team`，而不是 `team/`。`team` 太偏人类团队语义，通用 AgentOS 中 `orchestrator` 更准确。

### Session / Memory / Compact

`session.Session` 面向 Agent Loop，提供消息历史的最小操作集（`ID/Metadata/State/SetState/Append/Messages` 6 方法 append-only），不存在 `Truncate / Replace / Checkpoint / Store` 概念。常用 fork 可由应用层用 `session.NewSession + session.WithMessages + session.WithMetadata` 组合；它不是接口的一部分。JSONL、SQLite、Redis 等持久化后端独立暴露自身 API，不在 `session/` 包内置。详见 [design-session.md](design-session.md)。

`compact.Compactor` 是一个纯函数式接口 `Compact(ctx, compact.Request) ([]*Message, error)`。Loop 在每个 model step 构建 `*model.Request` 之前**无条件**调用一次 `Compact`，由 Compactor 自身决定短路（已在预算内零成本透传）或工作（增量摘要、窗口裁剪、tool result 截断）。Compactor 永不写回 Session；其滚动状态（如 summarize 的 offset / summary 内容）通过 `Session.State()` 私有 key（`__compact_summary_offset__` / `__compact_summary_content__`）持久化。详见 [design-compact.md](design-compact.md) §触发时机 与 [design-event-agent-loop.md](design-event-agent-loop.md) §9。

**增量与迭代两个契约支撑"按需控制上下文大小"**：

- **增量压缩**：Session append-only ⇒ 消息下标稳定 ⇒ Compactor 仅需在 `Session.State()` 中维护单调递增的 `offset` 即可区分"已压缩区"与"未压缩区"，每次只对 `msgs[offset:]` 中新增的部分做工作，不会重复对已折叠的历史调用摘要 LLM。详见 [design-compact.md](design-compact.md) §增量压缩契约、[design-session.md](design-session.md) §为什么 append-only 是增量压缩的前提。
- **迭代压缩 + Hint 重试两层兜底**：单次 `Compact` 调用内部循环折叠批次直到 ① 满足预算 ② `offset` 抵达 `len(msgs) - KeepRecent` 无可压区 ③ 触发安全阀（Step 内）；provider 真实仍报 context-too-long 时由 Loop 透传 `HintShrink` 重试 1 次（Step 间），仍未严格下降则 fail-fast `event.Error`。两层正交不互相替代。详见 [design-compact.md](design-compact.md) §迭代压缩契约、[design-event-agent-loop.md](design-event-agent-loop.md) §上下文超长的两层兜底。

`contextBuilder` 是 Session / Prompt / Compact 与 `*model.Request` 之间的装配逻辑，位于根包私有实现，按有序 pipeline 装配——**先 compact 再 prompt**：`snapshot ← session.Messages(ctx)`；`view ← compactor.Compact(ctx, compact.Request{Messages: snapshot, TokenCounter: counter})`；`systemParts ← prompt.Builder.Build(ctx)`（memory 召回在此发生）；最终 `*model.Request{System: prompt.JoinText(systemParts), Messages: view, Tools}`。`WithContextBudget` 配置 input/system/messages/tools 预算，`WithTokenCounter` 显式配置完整 request 计数；未配置时使用 `model.ApproxTokenCounter`，不从 provider 自动探测；根包把 `ContextInfo` 暴露给 prompt/memory 构建期，把 `ContextStats` 暴露给 model hooks/provider 调用期。Compactor 仅看 messages、prompt builder 仅看 system，二者互不感知。详见 [design-context-management.md](design-context-management.md)。

`memory/` 提供 `Memory` 接口（`Recall + Remember + Forget` 三方法，围绕 `Entry` / `Query` 结构体），不在根 Agent 内置。应用层在 prompt builder 中调用 `memory.Recall` 注入相关记忆，在 turn 结束后调用 `memory.Remember` 写入已归一化的单条 Entry，并在用户撤回 / TTL 过期 / 人工纠错时通过 `memory.Forget(ctx, entry)` 删除条目（按 Entry.ID，空 ID 报错）。Memory 不进根 Agent 配置；保持根包极简。

**Memory 与 Compact 解耦**：Memory 召回结果通过 `prompt.Memory` section 进入 `Request.System`，与 Compactor 作用的 `Request.Messages` **完全正交**——Compactor 不会再次裁剪 memory 段；memory 体量控制（`Query.Limit`、section 内 token 估算）由 memory 实现与应用层负责，core 不做兜底。建议应用层把 provider 上下文上限三段分摊：`SystemBudget`（含 memory）/ `MessagesBudget`（compact 的 `MaxTokens`）/ `ResponseReserve`，分别注入 prompt 与 compact 配置。详见 [design-memory.md](design-memory.md) §与 Compact 的边界、[design-compact.md](design-compact.md) §与 Memory 的关系。

## 子文档职责

当前 v1 子文档按稳定边界拆分：

| 子文档 | 职责 |
|--------|------|
| [design-event-agent-loop.md](design-event-agent-loop.md) | Event 协议、Agent Loop 顺序流程与行为事件 hook、Event/Message 转换边界 |
| [design-model-provider.md](design-model-provider.md) | `model/` Message、Part、Provider、Request/Response、重试、token 计数 |
| [design-prompt.md](design-prompt.md) | `prompt/` Builder、Section、缓存断点和静态/动态 prompt 组织 |
| [design-compact.md](design-compact.md) | `compact/` Compactor 接口与内置实现（Window/ToolResultBudget/Summarize/Chain），provider invariant 保护策略 |
| [design-tool-system.md](design-tool-system.md) | `tools/` Tool、Result、Resolver、Filter 和执行上下文 |
| [design-hook-extension.md](design-hook-extension.md) | 单一 Hook 接口与应用事件隔离 |
| [design-session.md](design-session.md) | Session 接口（6 方法 append-only）、Context helper（NewContext/FromContext/Ensure）、view-only compaction 边界 |
| [design-policy-mode.md](design-policy-mode.md) | policy core（v1 单边界 ToolRequest，单向依赖 tools/）与应用交互模式边界 |
| [design-agent-orchestration.md](design-agent-orchestration.md) | `flow/` 组合、Agent-as-Tool 和多 Agent 边界 |
| [design-memory.md](design-memory.md) | `memory.Memory` 接口（Recall+Remember+Forget）、`Entry` / `Query` 数据载体、应用层注入策略与异步抽取/遗忘边界 |

## 实现状态

所有核心包已实现完成：

- [x] `content/` — Part sealed 接口 + 5 种变体
- [x] `event/` — Input/Output sealed 事件协议
- [x] `model/` — Provider + Request/Response/Chunk + Option sealed（依赖 tools.ToolSpec）
- [x] `tools/` — Tool 两方法接口 + ToolSpec（唯一定义点）+ Resolver + Filter + sentinel errors
- [x] `session/` — Session 6 方法接口 + in-memory 实现 + context helpers
- [x] `compact/` — Compactor + Window/Summary/Budget/Chain
- [x] `prompt/` — Builder + Section + Static/Text/Memory
- [x] `memory/` — Memory 接口 + NewInMemory
- [x] `policy/` — Policy + Chain/AllowAll/DenyAll/Budget/RateLimit/SafetyCheck
- [x] `hook/` — Hook 6 方法 + Noop + Abort/ErrAbort
- [x] `flow/` — Sequential/Parallel/Loop/Routing/Deep
- [x] `internal/convert/` — Event ↔ Message 转换
- [x] 根包 — Agent 接口 + llmAgent（pi-agent 风格 loop）+ Runner + NewAgentTool

## 风险与缓解

| 风险 | 影响 | 缓解 |
|------|------|------|
| Event 接口变更大 | 高 | 保持 `event/` 作为唯一入口，迁移围绕 `event.Input` / `event.Output` 和具体事件类型 |
| Event/Message 转换复杂 | 高 | 转换只允许在 `internal/convert`，增加 golden tests 覆盖多模态和工具调用 |
| context 被滥用 | 中 | context 只传取消、deadline、trace 和 typed capability；业务字段走 Event/Hook/Session 或应用层映射 |
| 包数量增加 | 中 | 保持叶子包小接口，应用层负责接入和装配，避免根包重新膨胀 |
| policy 语义过宽 | 中 | `Decision` 和请求类型保持小而稳定，模式/预算/组织规则通过可选实现扩展 |
| 子文档漂移 | 中 | 以主文档 `sub-docs` 为 v1 索引；历史聚合文档只保留 superseded 注记 |
| 去掉内置 coding presets 后示例不足 | 低 | 在 `examples/coding` 提供完整应用/recipe，不进入核心依赖路径 |
| flow 与复杂编排边界模糊 | 中 | flow 只组合 Agent channel，复杂编排先放到应用层或 `contrib/` |

## 参考资料

- [Event 系统与 Agent Loop](design-event-agent-loop.md)
- [Model 与 Provider](design-model-provider.md)
- [Prompt 系统](design-prompt.md)
- [Compact 系统](design-compact.md)
- [工具系统](design-tool-system.md)
- [Hook 系统](design-hook-extension.md)
- [Session 系统](design-session.md)
- [Policy 与交互模式边界](design-policy-mode.md)
- [Agent 组合与编排](design-agent-orchestration.md)
- [Memory 系统](design-memory.md)
- [Claude Code Agent 参考设计](reference-claude-code-agent.md)
- [pi-agent Framework 参考设计](reference-pi-agent-framework.md)
