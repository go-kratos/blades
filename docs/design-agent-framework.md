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

当前 Blades 已有 `Agent`、基于 `iter.Seq2` 的流式接口、`Invocation`、`Session`、`Middleware`、`flow/`、`graph/`、`tools/`、`skills/`、`memory/`、`recipe/` 和多 Provider 集成。本轮设计以新 API 为目标，把这些能力重组为清晰分层的 AgentOS。

本文描述的是 AgentOS 目标架构，允许不兼容重构。文中的 `content/`、`event/`、`model/`、`policy/`、`hook/`、`compact/` 和 `internal/convert` 等包名是目标拆分，不表示当前仓库已经全部存在。

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

`blades.NewAgent(name, opts...)` 返回默认 `llmAgent`。`llmAgent` 在根包内部实现 Run / Turn / Step / Tool Wave 四层运行模型；用户不需要导入公开 `loop/` 包。需要局部定制时使用 `WithRequestBuilder`、`WithToolExecutor`、`WithHooks`、`WithPolicy` 等 options；完全不同的 runtime 直接实现 `blades.Agent`。

`event/` 文本糖通过构造函数提供：`event.NewPromptText(s string) Prompt` / `event.NewSteerText(s string) Steer`，无需独立事件类型。

`Run` 使用统一 channel 交互：输入 channel 承载 `event.Input`，返回的只读输出 channel 承载 `event.Output`。第二返回值只表示 Agent 无法启动或无法创建输出流；一旦 `Run` 成功返回 output channel，运行期错误也通过 `event.Error` 进入同一个输出流，最终发送 `event.Done` 后关闭 channel。

```go
input <- event.NewPromptText("hello")

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
- `agent.NewContext` / `agent.FromContext`：当前 Agent 内省（name、parent、depth）。

工具自身通过 `tools.NewContext` / `tools.FromContext` 取得每次调用的 `ToolContext`（`ID` / `Name`），由 ToolExecutor 在调用 `Tool.Handle` 前注入；`ToolContext` 仅暴露调用元数据，不承载控制信号。Resolver、Allowed 列表等 Agent 级运行时能力通过 `agent.FromContext(ctx)` 暴露的运行时上下文获取。工具签名仍为 `Handle(ctx context.Context, input json.RawMessage)`。控制流信号通过 sentinel error 返回，由 ToolExecutor 翻译为专用 sealed Output 帧 `event.LoopExit` / `event.Handoff`，紧跟在产生它的 `event.ToolEnd` 之后发出（参见 [design-tool-system.md](design-tool-system.md) §6 与 [design-event-agent-loop.md](design-event-agent-loop.md) §4.2）。

**可入 context 三准则（硬约束）**：任何想进入核心 context 的 capability 必须**同时**满足：

1. **runtime-scoped**：随 ctx 取消而失效，不是 long-lived global state。
2. **稳定不变**：在一次 Agent 执行期间不会被替换（替换需派生新 ctx）。
3. **多层共需**：至少跨 Loop / Tool / Hook / Middleware 中的两层。

不满足任一条的字段（`AppID` / `UserID` / `ChannelID` / `WorkspaceID` / chat ID / platform ID / notification target / turn ID / tool call ID 等业务标识）**必须**由应用层用自己的 context key 管理，core 不提供 helper、不接受 PR 加 helper。`TraceID` 使用 OpenTelemetry context 传播。context 中禁止放大对象、可变 map、消息历史或工具结果。

### Event 类型

```go
package event

// 开放 marker：导出方法，contrib 与应用可贡献新变体。Loop 的 type switch 必须带 default。
type Input  interface{ input()  }
type Output interface{ output() }

// 多模态字段使用 content.Part。
// 例如：event.Prompt.Parts 和 event.PartStart.Part 字段类型就是 content.Part / []content.Part。
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
| `PartStart` / `PartDelta` / `PartEnd` | 多模态内容生命周期和高级增量输出 |
| `ToolStart` / `ToolDelta` / `ToolEnd` | 工具执行生命周期 |
| `TurnEnd` | 单 turn 结束（含 `Parts []content.Part`、`StopReason`、token usage 汇总） |
| `Error` | 运行期错误（实现 `Output`，与其他事件同流；`event.Error{Err error}` 用 Go 标准 error，靠 `errors.Is/As` + 包内 sentinel 判断；启动期错误走 `Run` 签名第二返回值） |
| `Done` | Run 结束 sentinel；channel 关闭前发送，便于多 channel `select` 分支区分 |

输入和输出都必须支持多模态 Part。**`content.Part` 是 AgentOS 唯一的 Part union**（sealed marker：私有 `part()` 方法），定义在 `content/` 包中（仅依赖标准库），统一覆盖用户协议与 provider 协议两类变体：

- 用户多模态变体：`Text`、`Blob`、`Thinking`。`Blob{MIME, Source}` 用 sealed `BlobSource` 表达 inline bytes / URI / FileID；`Thinking` 携带 `Signature []byte` 以承载 Anthropic extended thinking / OpenAI o1 reasoning 的 provider 校验签名；JSON 通过 `Blob{MIME:"application/json"}` 或 `Text` 表达。
- Provider/工具协议变体：`ToolUse{ID, Name, Input json.RawMessage}` 与 `ToolResult{ID, Name, Parts []content.Part, IsError bool}`。

`event` 中的多模态字段、`tools.Result.Parts`、`model.Message.Parts`、`model.Chunk.Parts` 全部直接使用 `content.Part`，三个协议包不再各自定义 Part。`content/` 不引入统一 `Metadata map[string]any`——业务扩展通过应用层嵌入业务结构体实现。

文本输入用 `event.NewPromptText(s)` / `event.NewSteerText(s)` 构造函数返回 `Prompt` / `Steer`；多模态输入直接构造 `Prompt{Parts: []content.Part{...}}` / `Steer{Parts: ...}`。流式文本/思考输出走紧凑值类型 `event.TextDelta` / `event.ThinkingDelta`（hot path，避免 interface boxing）；其他模态的流式增量走 `event.PartStart` / `event.PartDelta` / `event.PartEnd`（cold path，承载 Blob 流式 / 自定义 Part）。两条 delta 路径不重叠，由 Loop 按 part 模态分发。完整最终多模态结果在 `PartEnd.Part` 和 `TurnEnd.Parts` 中。

Agent Loop 在工具结果落点做轻量包装而非全 DTO 复制：`tools.Result{Parts: []content.Part}` → `event.ToolEnd.Result` 直接复用同一切片，→ `model.Message.Parts` 中追加 `content.ToolResult{Parts: ...}` 同样直接复用。

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
│    Agent / NewAgent / Option / Collect / Drain                    │
├─────────────────────────────────────────────────────────────────┤
│  Execution Kernel                                                 │
│    llmAgent 默认 Run / Turn / Step / Tool Wave 执行循环            │
│    flow/ 组合原语；middleware/ 输入输出管线                         │
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

主链路是：Application Host 把外部输入转成 `event.Input`，通过 Agent Interface 进入 Execution Kernel；Kernel 驱动模型调用、工具执行、会话推进和流式输出，并在执行过程中调用 Extension Layer；Protocol Foundation 承载内容协议、事件协议、模型协议和 Event ↔ Message 私有转换边界。`contrib/openai`、`contrib/anthropic`、`contrib/gemini`、`contrib/mcp`、`contrib/otel` 和 preset 是基于 Protocol Foundation 与 Extension Layer 的集成包，不属于核心主链路。`graph/`、`evaluator/`、`recipe/` 是可选系统，不放入最小 Agent runtime 主链路。

## 设计原则

| 原则 | 决策 |
|------|------|
| 根包极简 | `blades/` 放 `Agent`、`NewAgent`、Option、默认 `llmAgent`、必要错误和纯 Agent runner helper |
| 协议叶子互独立 | `content/`、`event/`、`model/`、`tools/` 之间禁止形成循环；`event/tools` 单向依赖 `content/` |
| 多模态共享叶子 | `content/` 仅依赖标准库；`Part` 为 sealed marker（私有 `part()`）；变体 = Text/Blob/Thinking/ToolUse/ToolResult；`Blob.Source` sealed（私有 `blobSource()`）覆盖 InlineBytes/URI/FileID；Thinking 含 Signature |
| Provider 协议 sealed | 四处 sealed 例外全部封闭：`content.Part`（私有 `part()`）、`event.Input`（私有 `input()`）、`event.Output`（私有 `output()`）、`hook.Event`（runtime 契约不允许外部扩展）。核心协议层无开放扩展接口；后台回流走 `event.Prompt`，应用业务事件由应用自己的 channel / event bus 承载 |
| Tool 是能力叶子包 | `tools/` 单向依赖 `content/`；`tools.Result.Parts []content.Part` |
| Runtime 根包内置 | 默认 Agent Loop 是根包 `llmAgent` 的内部机制，不暴露公开 `loop/` 包；通过 `WithRequestBuilder` / `WithToolExecutor` 等 options 替换局部策略 |
| 唯一私有转换 | 仅 `internal/convert/` 持 Event ↔ Message 转换函数；用户不得绕过 Loop 直接转换 |
| 不显式 FSM 枚举 | `llmAgent` 内部用 Run / Turn / Step / Tool Wave 顺序代码 + 行为事件 hook 表达流程；不导出 `State` 枚举 |
| Context 三准则 | 入 ctx 的 capability 必须满足：runtime-scoped + Run 内不变 + 多层共需；否则下沉应用层 context key |
| Context 命名统一 | `pkg.NewContext(ctx, x)` / `pkg.FromContext(ctx) (X, bool)` stdlib 风格；`session/agent/tools` 三处 helper 同形 |
| Policy 单边界 | `policy/` 单向依赖 `tools/`（不依赖 `event/model/content`）；v1 唯一请求是 `policy.ToolRequest{Tool tools.Tool, Input json.RawMessage}`；`Policy.Check(ctx, ToolRequest) Decision` 单方法接口；不引入 sealed `Request` / `ModelRequest` / `ResourceRequest`。模型预算与速率限制由 hook + 应用层组合实现 |
| Prompt 独立 | system prompt 构建放在 `prompt/`，根包不导出 `PromptBuilder` |
| Middleware 独立 | 输入/输出 middleware 放在 `middleware/`，只操作 Event channel |
| Composition 不污染根包 | `flow.NewSequentialAgent`/`NewParallelAgent`/`NewLoopAgent`/`NewRoutingAgent`/`NewDeepAgent` 放 `flow/`，读作 `flow.NewParallelAgent(cfg)`；Agent→Tool 适配器 `NewAgentTool` 在根包 |
| 应用接入框架外实现 | CLI/HTTP/WebSocket/Slack/Scheduler 等属于具体应用，不作为 AgentOS 核心公开包 |
| Coding 不是核心 | `Explore/Plan/General/Verify` 不进 v1 核心；可放 examples、contrib preset 或业务 app |

## 包结构

```
blades/
├── agent.go                    Agent 接口 + NewAgent 构造函数
├── option.go                   AgentOption + WithModel/WithTools/WithSession/...
├── agent_run.go                llmAgent Run 生命周期、input 消费、Done 输出
├── agent_turn.go               单 turn 执行、Prompt/Steer/Abort/Pause/Resume
├── agent_step.go               model step：request 构建、provider stream、delta 收集
├── agent_tools.go              tool wave 执行、tool result 回填
├── tool.go                     NewAgentTool(Agent) tools.Tool —— 把 Agent 暴露为工具的根包适配器
├── runner.go                   纯 Agent 运行辅助（Collect/Drain 等）
├── errors.go
│
├── content/
│   ├── part.go                 Part 接口（sealed marker：私有 part()）
│   ├── text.go                 Text{Text string}
│   ├── blob.go                 Blob{MIME, Source}, BlobSource（sealed：blobSource()）；type InlineBytes []byte / type URI string / type FileID string
│   ├── thinking.go             Thinking{Text string, Signature []byte}
│   └── tool.go                 ToolUse{ID, Name, Input json.RawMessage}, ToolResult{ID, Name, Parts []Part, IsError bool}
│
├── event/
│   ├── event.go                Input（sealed：input()）, Output（sealed：output()）
│   ├── control.go              Abort{Reason string}, Pause{}, Resume{}（三种独立 Control 类型）
│   ├── input.go                Prompt{Parts []content.Part}, Steer{Parts []content.Part}, NewPromptText/NewSteerText 构造函数
│   ├── output.go               输出事件公共类型（字段类型为 content.Part）
│   ├── stream.go               TextDelta/ThinkingDelta（hot path，紧凑值类型），PartStart/PartDelta/PartEnd（cold path：字段类型为 content.Part）
│   ├── tool.go                 ToolStart, ToolDelta, ToolEnd
│   ├── terminal.go             StepEnd, Usage, StopReason, TurnEnd{Parts []content.Part, StopReason, Usage, Err}, Error{Err error}, Done{}
│
├── model/
│   ├── message.go              Message{Role, Parts []content.Part}, type Role string（命名常量 RoleUser/RoleAssistant/RoleTool；system 走 Request.System）
│   ├── option.go               Option sealed interface + 内置 CacheHint/ReasoningEffort/ResponseFormat/Sampling；MergeOptions(defaults, request)
│   ├── provider.go             Provider 接口（Name + Generate(ctx,*Request) (*Response,error) + Stream(ctx,*Request) iter.Seq2[*Chunk,error]）；TokenCounter 独立接口（Count(ctx,*Request) (Usage,error)）；EmbeddingProvider 平级独立接口
│   ├── request.go              Request{Model, System string, Messages, Tools, Options []Option}, Response{Message *Message, StopReason, Usage}, Chunk{Parts []content.Part, StopReason, Usage *Usage}, ToolSpec, Usage, StopReason
│   └── collect.go              Collect(seq) (*Response, error) 累加流式为完整响应（非流式 sugar，stream-only adapter 用）
│
├── tools/
│   ├── tool.go                 Tool 核心接口（Spec()ToolSpec + Handle(ctx,input)(*Result,error) 两方法）；可选能力接口仅 3 个：ReadOnlyTool / DestructiveTool / StreamingTool
│   ├── result.go               Result{Parts []content.Part}（错误走 Handle error 第二返回值，IsError 由 Loop 在 err!=nil 时设置）
│   ├── resolver.go             Resolver
│   ├── filter.go               ToolFilter（纯集合操作，留在 tools/）+ ReadOnly/AllowOnly/Disallow/And/Or
│   ├── context.go              ToolContext 接口（ID/Name，per-invocation 元数据，不含 Actions/SetAction）+ NewContext / FromContext 标准 stdlib 风格 helper
│   └── errors.go               sentinel error（ErrLoopExit / ErrHandoff）；ToolExecutor 翻译为 `event.LoopExit` / `event.Handoff` 专用 Output 帧
│
├── prompt/
│   ├── prompt.go               Builder 接口 + Section 函数类型（func(ctx) ([]content.Part, error)）+ New(sections...) Builder
│   └── section.go              内置 Section 工厂：Static / Dynamic / System(text) / Memory(m, query, ...RecallOption)
│
├── middleware/
│   └── middleware.go           InputMiddleware / OutputMiddleware
│
├── flow/
│   ├── sequential.go           NewSequentialAgent(SequentialConfig) blades.Agent
│   ├── parallel.go             NewParallelAgent(ParallelConfig) blades.Agent
│   ├── loop.go                 NewLoopAgent(LoopConfig) blades.Agent
│   ├── routing.go              NewRoutingAgent(RoutingConfig) (blades.Agent, error)
│   └── deep.go                 NewDeepAgent(DeepConfig) (blades.Agent, error)
│   // NewAgentTool 不在 flow/，由根包 tool.go 提供（blades.NewAgentTool）
│
├── session/
│   ├── session.go              Session 接口（6 方法：ID/Metadata/State/SetState/Append/Messages，append-only）+ Fork helper（NewSession+WithMessages 的薄包装）+ NewContext/FromContext/Ensure
│   └── inmemory.go             基于内存的默认实现；JSONL/SQLite/Redis 等持久化由具体后端独立暴露，不在 session/ 内置
│
├── compact/
│   ├── compact.go              Compactor 接口（Compact(ctx, msgs) -> msgs）+ Chain + Window
│   ├── budget.go               ToolResultBudget(maxBytes) Compactor（截断超大 tool result）
│   └── summary.go              Summarize(provider, ...) Compactor（LLM 摘要）
│
├── hook/
│   ├── event.go                hook.Event（sealed marker：私有 hookEvent()）+ 核心 events 全集（PreModelCall/PostModelCall/PreToolCall/PostToolCall/TurnStart/TurnEnd/LoopExit/Handoff，共 8 类）；LoopExit/Handoff 内嵌对应 event.LoopExit/event.Handoff，与 Output 流同源同步触发
│   └── hook.go                 单一 Hook 接口（Handle(ctx, Event) error）+ 类型安全 helper（OnPreModelCall/OnPostToolCall/...）
│
├── policy/
│   ├── policy.go               Policy 接口（Check(ctx, ToolRequest) Decision）+ Decision/Action（Allow/Deny/Ask/Modify）
│   ├── request.go              ToolRequest{Tool tools.Tool, Input json.RawMessage}（v1 唯一请求；不引入 sealed Request）
│   └── builtin.go              Chain/Budget/RateLimit/SafetyCheck 工厂函数（均返回 Policy）
│
├── memory/
│   ├── memory.go               Memory 接口（Recall+Remember+Forget 三方法，全部 variadic option）+ Entry
│   └── store.go                可选 Store 后端抽象（Put/Search/Delete，对称 Memory 三方法）
├── graph/                      声明式 DAG 调度（节点+边+条件路由），与命令式 flow/ 互补不重叠
│
│   非核心（保留代码但不属于 14 个核心包）：
├── recipe/                     预设模板（应用层）
├── evaluator/                  评测工具（开发期）
│
├── internal/
│   └── convert/                Event ↔ Message 唯一私有转换
└── contrib/                    provider/preset/observability 集成（含 contrib/otel）
```

### Runner helper 边界

同步调用 / drain / collect 等执行 helper 放在根包：`blades.Collect(ctx, agent, input)` 同步收集所有 Output，`blades.Drain(...)` 仅消耗 stream。根包是唯一运行入口：`Agent`、`NewAgent`、Option、`With*`、runner helpers 和默认 `llmAgent`。

run manager 语义（run ID、队列、daemon、cron、后台 job、主动通知、channel adapter、workspace 映射、配置加载、session 映射）一律不在核心，属于应用接入层。

### 为什么不用根包放组合原语

`Sequential`、`Parallel`、`Loop`、`Routing`、`Deep` 是通用组合能力，但不是所有用户创建 Agent 都需要。放在根包会让根包承担运行时编排语义，并和 `AgentOption`、基础构造 API 混在一起。保留 `flow/` 更符合 Go 的包边界：

- `blades.Agent` 是最小接口。
- `flow.NewSequentialAgent` 等读作组合领域的构造函数。
- `flow/` 可以依赖根包，根包不依赖 `flow/`，无循环。
- `flow/` 保留五件套：`NewSequentialAgent` / `NewParallelAgent` / `NewLoopAgent` / `NewRoutingAgent` / `NewDeepAgent`。Agent→Tool 适配器 `NewAgentTool` 不在 `flow/`，由根包 `tool.go` 提供（`blades.NewAgentTool`）。

### 根包 Agent Loop vs flow/ 边界

容易混淆，明确区分：

- **根包 `llmAgent`**：*单个* Agent 内部的运行循环（Run → Turn → Step → Tool Wave）。局部策略通过 `WithRequestBuilder` / `WithToolExecutor` 替换，无公开 `loop/` 包。
- **`flow/`**：*多个* Agent 之间的组合（Sequential / Parallel / Loop / Routing / Deep）。`flow.NewLoopAgent` 是把另一个 Agent 反复调用，不是单 Agent 内部的 provider/tool 循环。
- **根包 `blades.NewAgentTool`**：把一个 `blades.Agent` 暴露为 `tools.Tool`，让另一个 Agent 通过工具调用启用它；属于"Agent 是顶层 first-class 概念"语义，故落在根包 `tool.go` 而非 `flow/`。签名为 `NewAgentTool(agent Agent) tools.Tool`，无 options（与现有 `tool.go` 命名风格保持一致）。

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
model/      -> content/                                 // Message.Parts: []content.Part；Chunk.Parts: []content.Part
tools/      -> content/ + jsonschema                    // Result.Parts: []content.Part

session/    -> model/
compact/    -> model/                                   // Summarizer 函数由上层注入
memory/     -> stdlib only                              // Memory 接口；具体后端通过 contrib 实现
prompt/     -> content/, model/, memory/                // Memory section 引用
hook/       -> content/, event/, model/, tools/
policy/     -> tools/                                   // v1 单边界 ToolRequest，不入 model/event/content

middleware/ -> event/
flow/       -> blades/, event/, tools/

internal/convert/ -> content/, event/, model/, tools/

blades/     -> event/, content/, model/, tools/, session/, compact/, hook/, policy/, prompt/, internal/convert/

recipe/     -> blades/, tools/, model/, prompt/
contrib/*   -> model/ 或 tools/
```

循环依赖规避规则：

- `content/` 是最底层叶子，**硬约束仅依赖标准库**（`go list -deps ./content/...` 不得出现非 stdlib 包）；任何包都可单向依赖它。
- `event/`、`model/`、`tools/` 三个协议包都依赖 `content/`，但互不依赖。
- `policy/` 单向依赖 `tools/`，v1 仅 `ToolRequest`；不引入 sealed `Request` 抽象。模型预算/速率限制由 hook + 应用层组合实现，不在 policy 协议层枚举。
- `compact/` 不依赖 Provider 或 root Agent；摘要能力通过 `func(ctx, []*model.Message) (string, error)` 注入。
- `memory/` 不依赖 root Agent 也不依赖 model；仅暴露 `Memory` 接口供应用层在 prompt section 中调用。
- 根包默认 `llmAgent` 是运行时汇聚层，依赖所有协议与能力包；`internal/convert/` 是 `llmAgent` 内部专用的 Event ↔ Message 转换实现，不导出给用户。
- CLI、HTTP、WebSocket、Slack、Scheduler 等接入层不进入核心依赖图；具体应用自行把外部协议转换成 `event.Input` / `event.Output`。
- 后台运行、队列、drain、取消、主动通知等运行管理不进入核心依赖图；具体应用可以在 `cmd/<app>/internal` 内实现。

## 核心包导出类型

| 包 | 核心类型 | 示例 |
|----|----------|------|
| `blades` (root) | `Agent`, `NewAgent`, `Option`, `RequestBuilder`, `ToolExecutor`, `NewAgentTool`, `Collect`/`Drain`, `WithModel`/`WithTools`/`WithSession`/`WithPolicy`/`WithHooks`/`WithCompact`/`WithPrompt`/`WithRequestBuilder`/`WithToolExecutor`/`WithMaxSteps` | `blades.Agent` |
| `content` | `Part`（sealed marker：私有 `part()`），`Text`，`Blob{MIME, Source}`，`BlobSource`（sealed：`blobSource()`），`InlineBytes`，`URI`，`FileID`，`Thinking{Text, Signature []byte}`，`ToolUse{ID, Name, Input}`，`ToolResult{ID, Name, Parts, IsError}` | `content.Text{Text: "hi"}` |
| `event` | `Input`（sealed：`input()`）, `Output`（sealed：`output()`）, `Prompt`, `Steer`, `Abort{Reason}`, `Pause`, `Resume`, `TextDelta`, `ThinkingDelta`, `PartStart`, `PartDelta`, `PartEnd`, `ToolStart`, `ToolDelta`, `ToolEnd`, `LoopExit{ToolID,ToolName,Escalate}`, `Handoff{ToolID,ToolName,Agent,Carry}`, `TurnEnd`, `Error`, `Done`；构造糖：`NewPromptText`, `NewSteerText` | `event.NewPromptText("hi")` |
| `model` | `Message{Role, Parts []content.Part}`, `Role`, `RoleUser`/`RoleAssistant`/`RoleTool`, `Provider`(Name+Generate+Stream，Stream 返回 `iter.Seq2[*Chunk,error]`), `TokenCounter`(Count→Usage，按能力探测), `EmbeddingProvider`, `Request{Model, System string, Messages []*Message, Tools []ToolSpec, Options []Option}`, `Response{Message *Message, StopReason, Usage}`, `Chunk{Parts []content.Part, StopReason, Usage *Usage}`, `Option` sealed（`CacheHint`/`ReasoningEffort`/`ResponseFormat`/`Sampling`）, `ToolSpec`, `Usage`, `StopReason`, `Collect`, `MergeOptions` | `model.Provider` |
| `tools` | `Tool`(Spec+Handle 两方法), `ToolSpec`(=model.ToolSpec), `Result`(`Parts: []content.Part`), `ReadOnlyTool` / `DestructiveTool` / `StreamingTool`（仅 3 个可选能力接口）, `Resolver`(List+Resolve), `ToolFilter`, `ToolContext`(ID+Name), `NewContext`, `FromContext`, `ErrLoopExit`, `ErrHandoff`（sentinel 由 ToolExecutor 翻译为 `event.LoopExit` / `event.Handoff`） | `tools.Tool` |
| `prompt` | `Builder`(接口), `Section`(函数类型), `Static`/`Dynamic`/`System`/`Memory` 工厂, `New` | `prompt.Builder` |
| `flow` | `NewSequentialAgent`/`NewParallelAgent`/`NewLoopAgent`/`NewRoutingAgent`/`NewDeepAgent` 与对应 `*Config` 结构体（不含 `NewAgentTool`，`NewAgentTool` 由根包提供） | `flow.NewParallelAgent(cfg)` |
| `middleware` | `InputMiddleware`, `OutputMiddleware` | `middleware.InputMiddleware` |
| `session` | `Session`(6 方法 append-only), `Fork` helper, `NewSession`, `WithSessionID`/`WithMessages`/`WithMetadata`/`WithState`, `NewContext`, `FromContext`, `Ensure` | `session.Session` |
| `policy` | `Policy`(Check 单方法), `Decision`, `Action`(Allow/Deny/Ask/Modify), `ToolRequest`, `Chain`/`Budget`/`RateLimit`/`SafetyCheck` 工厂函数 | `policy.Policy` |
| `hook` | `Event`(sealed), `Hook`(Handle 单方法), `OnPreModelCall`/`OnPostModelCall`/`OnPreToolCall`/`OnPostToolCall`/`OnTurnStart`/`OnTurnEnd` 返回 `Hook` 的类型安全 helpers | `hook.Hook` |
| `compact` | `Compactor`(单方法接口), `Chain`, `Window`, `ToolResultBudget`, `Summarize`, `WithHint`/`HintShrink`（ctx hint 注入与常量） | `compact.Compactor` |
| `memory` | `Memory`(Recall+Remember+Forget，全部 variadic option), `Entry`, `Store`(可选后端：Put/Search/Delete) | `memory.Memory` |

## 模块详细设计

### Event 系统与 Agent Loop

Event 是用户协议层，定义 `event.Input` / `event.Output`，多模态字段直接使用 `content.Part`。Event 不和 `model.Message` 共享 Go 类型，但通过 `content.Part` 实现"零样板模态共享"。Agent Loop 在根包默认 `llmAgent` 内部实现，使用 Run / Turn / Step / Tool Wave 顺序代码 + 行为事件 hook 表达流程（不导出 FSM 状态枚举），负责：

- 把 `event.Prompt` / `event.Steer` 转成 `model.Message`。
- 通过 `RequestBuilder` 从 Session、`prompt.Builder`、ToolSpec、Compact 构建 `model.Request`。Memory 不直接被默认 Agent Loop 感知；如需注入由应用通过 `prompt.Memory` section 实现。
- 调用 `model.Provider`。
- 把 `model.Response` 转成 `event.TextDelta`、`event.Part*`、`event.StepEnd`、`event.Tool*`、`event.TurnEnd`。
- 把工具结果（`tools.Result.Parts: []content.Part`）零拷贝包装为 `event.ToolEnd.Result`，并将 `content.ToolResult{Parts: ...}` 直接追加到 `model.Message.Parts` 中。

唯一的 Event ↔ Message 转换函数留在 `internal/convert/`，用户不得绕过 Loop 直接调用。

详细定义见 [design-event-agent-loop.md](design-event-agent-loop.md)。

### Agent Runtime

Runtime 包括根包 `blades/`、`flow/` 与 `middleware/`。

`blades.NewAgent(name, opts...)` 创建默认 `llmAgent`。Agent 持有 model provider、tools resolver、session provider、prompt builder、policy、hooks、compact、request builder、tool executor 等配置，全部通过接口注入。Memory 不在根 Agent 内置（由应用层通过 prompt section 注入）。**根包绑定默认 LLM Agent 执行语义**，但不把 Loop 做成公开包；高级定制通过 `WithRequestBuilder` 或 `WithToolExecutor` 替换局部策略，完全特殊场景直接实现 `blades.Agent`。

`flow.NewSequentialAgent` / `NewParallelAgent` / `NewLoopAgent` / `NewRoutingAgent` / `NewDeepAgent` 接受对应的 `*Config` 并返回普通 `blades.Agent`：

```go
pipeline := flow.NewSequentialAgent(flow.SequentialConfig{Sub: []blades.Agent{researcher, planner, executor}})
race     := flow.NewParallelAgent(flow.ParallelConfig{Sub: []blades.Agent{indexSearch, vectorSearch, webSearch}})
iterative := flow.NewLoopAgent(flow.LoopConfig{Sub: worker, MaxIterations: 8})
```

需要把一个 Agent 当作工具供另一 Agent 调用时，使用根包 `blades.NewAgentTool(agent)`（不在 `flow/`）。

组合原语只组合 `event.Input` / `event.Output` channel，不读取 `model.Message`。如果需要复杂 DAG、checkpoint 或条件边，用 `graph/`，不要把所有工作流语义塞进 Agent 接口。

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

`policy/` 在 v1 仅承担**工具调用裁决**这一单边界：唯一请求是 `ToolRequest{Tool tools.Tool, Input json.RawMessage}`，`Policy` 是单方法接口 `Check(ctx, ToolRequest) Decision`；`Chain`/`Budget`/`RateLimit`/`SafetyCheck` 等内置实现都是返回 `Policy` 的工厂函数。`Decision.Action` 含 `Allow / Deny / Ask / Modify`。模型预算与速率限制等不属于工具裁决的策略由 hook 与应用层组合实现，不在 policy 协议层枚举（不引入 sealed `Request` / `ModelRequest` / `ResourceRequest`）。`policy/` 单向依赖 `tools/`，不依赖 `event/model/content`，避免协议环。Plan Mode、Accept Edits、Auto Mode 不作为 AgentOS 核心目标；它们是产品交互策略，应放在具体应用、examples 或 contrib 包中，基于 core policy primitives 组合实现。

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

`session.Session` 面向 Agent Loop，提供消息历史的最小操作集（`ID/Metadata/State/SetState/Append/Messages` 6 方法 append-only），不存在 `Truncate / Replace / Checkpoint / Store` 概念。常用 fork 由根包 helper `blades.Fork(ctx, src, opts...)` 提供（薄包装 `NewSession + WithMessages + WithMetadata`），不是接口的一部分。JSONL、SQLite、Redis 等持久化后端独立暴露自身 API，不在 `session/` 包内置。详见 [design-session.md](design-session.md)。

`compact.Compactor` 是一个纯函数式接口 `Compact(ctx, []*Message) ([]*Message, error)`。Loop 在每个 model step 构建 `*model.Request` 之前**无条件**调用一次 `Compact`，由 Compactor 自身决定短路（已在预算内零成本透传）或工作（增量摘要、窗口裁剪、tool result 截断）。Compactor 永不写回 Session；其滚动状态（如 summarize 的 offset / summary 内容）通过 `Session.State()` 私有 key（`__compact_summary_offset__` / `__compact_summary_content__`）持久化。provider 返回 context-too-long 时 Loop 通过 `compact.WithHint(ctx, HintShrink)` 透传 hint，要求 Compactor 返回严格单调下降视图；不下降则 fail-fast。详见 [design-compact.md](design-compact.md) §触发时机 与 [design-event-agent-loop.md](design-event-agent-loop.md) §9。

`RequestBuilder` 是 Session / Prompt / Compact 与 `*model.Request` 之间的**唯一边界**：`snapshot ← session.Messages(ctx)`；`systemParts ← prompt.Builder.Build(ctx)`；`view ← compactor.Compact(ctx, snapshot)`；最终 `*model.Request{System: systemTextFrom(systemParts), Messages: view + turn-local pending, Tools, Options}`。应用层可通过 `WithRequestBuilder` 替换默认实现。

`memory/` 提供 `Memory` 接口（`Recall + Remember + Forget` 三方法，全部使用 variadic option），不在根 Agent 内置。应用层在 prompt builder 中调用 `memory.Recall` 注入相关记忆，在 turn 结束后调用 `memory.Remember` 抽取写入，并在用户撤回 / TTL 过期 / 人工纠错时调用 `memory.Forget` 删除条目（必须显式 IDs 或 Filter，禁止无参全清）。Memory 不进根 Agent 配置；保持根包极简。

## 子文档职责

当前 v1 子文档按稳定边界拆分：

| 子文档 | 职责 |
|--------|------|
| [design-event-agent-loop.md](design-event-agent-loop.md) | Event 协议、Agent Loop 顺序流程与行为事件 hook、Event/Message 转换边界 |
| [design-model-provider.md](design-model-provider.md) | `model/` Message、Part、Provider、Request/Response、重试、token 计数 |
| [design-prompt.md](design-prompt.md) | `prompt/` Builder、Section、缓存断点和静态/动态 prompt 组织 |
| [design-compact.md](design-compact.md) | `compact/` Compactor 接口与内置实现（Window/ToolResultBudget/Summarize/Chain），provider invariant 保护策略 |
| [design-tool-system.md](design-tool-system.md) | `tools/` Tool、Result、Resolver、Filter 和执行上下文 |
| [design-hook-extension.md](design-hook-extension.md) | core-sealed hook event、单一 Hook 接口与应用事件隔离 |
| [design-session.md](design-session.md) | Session 接口（6 方法 append-only）、`blades.Fork` helper、Context helper（NewContext/FromContext/Ensure）、view-only compaction 边界 |
| [design-policy-mode.md](design-policy-mode.md) | policy core（v1 单边界 ToolRequest，单向依赖 tools/）与应用交互模式边界 |
| [design-agent-orchestration.md](design-agent-orchestration.md) | `flow/` 组合、Agent-as-Tool 和多 Agent 边界 |
| [design-memory.md](design-memory.md) | `memory.Memory` 接口（Recall+Remember+Forget，全部 variadic option）、`Entry` 数据载体、应用层注入策略与异步抽取/遗忘边界 |

## 实现计划

### 阶段 1：协议叶子（content/event）与 Agent Loop

- [ ] 定义 `content/`：`Part`（sealed marker：私有 `part()`）、`Text`、`Blob{MIME, Source}`（`BlobSource` sealed 私有 marker `blobSource()`：`InlineBytes` / `URI` / `FileID`）、`Thinking{Text, Signature []byte}`、`ToolUse{ID, Name, Input json.RawMessage}`、`ToolResult{ID, Name, Parts []Part, IsError bool}`
- [ ] 定义 `event/`：`Input`（sealed：`input()`） / `Output`（sealed：`output()`），多模态字段使用 `content.Part`、`Prompt{Parts []content.Part}`、`Steer{Parts []content.Part}`、`NewPromptText` / `NewSteerText` 构造函数、`Abort{Reason string}`、`Pause{}`、`Resume{}`、`TextDelta`、`ThinkingDelta`（hot path）、`PartStart`/`PartDelta`/`PartEnd`（cold path 多模态）、`Tool*`、`TurnEnd`、`Error`、`Done`
- [ ] 根包不 re-export Event 类型或构造函数；用户代码统一导入 `event/`
- [ ] Agent 接口改为 `Run(context.Context, <-chan event.Input) (<-chan event.Output, error)`
- [ ] 在根包实现默认 `llmAgent` 内置运行时：Run / Turn / Step / Tool Wave 四层模型，不导出公开 `loop/` 包
- [ ] 定义根包 `RequestBuilder` 与 `ToolExecutor` 扩展点，并提供 `WithRequestBuilder` / `WithToolExecutor` / `WithMaxSteps`
- [ ] 实现 Agent Loop 对 `session.Session`、Agent 内省 context（`agent.NewContext`/`agent.FromContext`）和 `tools.FromContext` 的读取与传递
- [ ] 在 `internal/convert/` 实现唯一的 Event ↔ Message 转换函数
- [ ] 在 `middleware/` 实现 InputMiddleware / OutputMiddleware

### 阶段 2：model/session/compact

- [ ] 实现 `model/`：`Message{Role, Parts []content.Part}`、`Provider` 接口（Name + Generate + Stream，Stream 返回 `iter.Seq2[*Chunk, error]`，避免 model 依赖根包）、`TokenCounter` 独立接口（Count→Usage，按能力探测）、`EmbeddingProvider` 平级独立接口、`Request{Model, System string, Messages []*Message, Tools []ToolSpec, Options []Option}`、`Response{Message *Message, StopReason, Usage}`、`Chunk{Parts []content.Part, StopReason, Usage *Usage}`、`Option` sealed union（`CacheHint`/`ReasoningEffort`/`ResponseFormat`/`Sampling`）、`ToolSpec`、`Usage`、`StopReason`、`Collect` 流式累加 helper、`MergeOptions(defaults, request)` 默认值合并
- [ ] 实现 `session.Session` 与 `session.Store`，提供 `session.NewContext`/`session.FromContext`
- [ ] 实现默认 `RequestBuilder` 与 streamAndRecord
- [ ] 实现 `compact.Compactor` 内置组合（Window/ToolResultBudget/Summarize/Chain）与 provider invariant 保护
- [ ] 在 `prompt/` 实现 `Builder` 静态/动态 section 与 cache breakpoint

### 阶段 3：tools/policy/hook

- [ ] 精简 `tools.Tool` 为两方法接口（`Spec() ToolSpec` + `Handle(ctx, input json.RawMessage) (*Result, error)`），`ToolSpec` 与 `model.ToolSpec` 同构；保留 3 个可选能力接口 `ReadOnlyTool` / `DestructiveTool` / `StreamingTool`；`tools.Result.Parts: []content.Part` 直接复用 content/，错误走 Handle error 第二返回值
- [ ] 实现 ToolFilter、Resolver、StreamingToolExecutor；`tools/context.go` 提供 `ToolContext` 接口（`ID` / `Name`）与 `NewContext` / `FromContext`；`tools/errors.go` 提供 `ErrLoopExit`/`ErrHandoff` sentinel（ToolExecutor 翻译为 `event.LoopExit` / `event.Handoff` 专用 Output 帧）
- [ ] 实现 `policy.Policy` 单方法接口 + 内置工厂（Chain/Budget/RateLimit/SafetyCheck），v1 唯一请求为 `ToolRequest`（不引入 sealed `Request`）
- [ ] 实现 SafetyChecker、BudgetPolicy、RateLimiter
- [ ] 实现 `hook.Hook`：sealed hook events 全集、单一 Hook 接口、`OnPreModelCall`/`OnPostToolCall` 等返回 `Hook` 的泛型 helper；Registry 如有需要由应用层实现并作为一个 `hook.Hook` 注入
- [ ] 在 Agent Loop 中串联 tool validation、policy、hook、execution、result conversion

### 阶段 4：应用接入样板与边界验证

- [ ] 用 `cmd/<app>/internal/channel`、`cmd/<app>/internal/app`、`cmd/<app>/internal/workspace` 展示应用层样板
- [ ] 把 CLI/HTTP/WebSocket 等外部协议保留在具体应用、examples 或 contrib，不进入 Agent core
- [ ] 用示例说明应用层如何管理 run lifecycle、session 映射、主动通知和配置加载

### 阶段 5：组合、应用与迁移

- [ ] 实现 `flow.NewSequentialAgent`/`flow.NewParallelAgent`/`flow.NewLoopAgent`/`flow.NewRoutingAgent`/`flow.NewDeepAgent`（5 个组合原语）
- [ ] 在根包 `tool.go` 中实现 `blades.NewAgentTool(agent)`（不在 `flow/`）
- [ ] 迁移 contrib providers 到 `model.Provider`
- [ ] 迁移 skills 到新 `tools.Tool`
- [ ] 把 coding presets 移到 `examples/coding` 或 `contrib/preset`

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
| flow 与 graph 边界模糊 | 中 | flow 只组合 Agent channel，graph 负责 DAG/checkpoint/condition |

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
