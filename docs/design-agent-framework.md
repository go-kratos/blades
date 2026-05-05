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
  - design-observability.md
  - design-graph.md
  - design-migration.md
---

# Blades AgentOS Framework 设计蓝图

## 背景与目标

Blades 的目标是成为通用 AgentOS Core Runtime。核心层负责 Agent 事件协议、运行循环、模型上下文构建、工具编排、会话持久化、策略决策、Hook 和 Memory 等基础能力，并保持 API 面向通用 Agent 场景。

应用层负责把核心能力装配成具体产品形态，包括 CLI、HTTP、微信、飞书、调度器等 channel 接入，workspace 管理，配置加载，daemon，cron，session 映射，主动通知和第三方 SDK 集成。推荐在具体应用内使用 `cmd/<app>/internal/*` 组织这些装配代码；Coding、客服、数据分析、自动化运维、研究助手等场景通过应用、recipe、examples 或 contrib preset 承接。

当前 Blades 已有 `Agent`、基于 `iter.Seq2` 的流式接口、`Invocation`、`Session`、`Middleware`、`flow/`、`graph/`、`tools/`、`skills/`、`memory/`、`recipe/` 和多 Provider 集成。本轮设计以新 API 为目标，把这些能力重组为清晰分层的 AgentOS。

本文描述的是 AgentOS 目标架构，允许不兼容重构。文中的 `event/`、`model/`、`policy/`、`hook/`、`compact/` 和 `internal/loop` 等包名是目标拆分，不表示当前仓库已经全部存在。

核心目标：

- **事件驱动**：外部应用通过 `event.Input` / `event.Output` 与 Agent 通信，channel 中直接传具体事件。
- **Event / Message 分层**：Event 是用户协议层，Message 是模型上下文协议层；Agent Loop 是唯一转换边界。
- **通用 AgentOS 核心**：核心提供 runtime、policy、session、tool、memory 等基础能力；channel、host、workspace 和 coding-specific workflow 由应用层承接。
- **应用层自持接入**：channel、workspace、配置、daemon、cron、外部平台 SDK 和产品交互由具体应用实现，推荐使用 `cmd/<app>/internal/*` 作为应用层样板。
- **包依赖可证明**：协议叶子包互相独立；上层通过接口、函数注入和 typed capability context 连接，避免循环依赖。
- **Go 惯用 API**：小接口、短包名、`package.Role` 命名、`context.Context` 取消与 trace 传播、Option 函数配置。
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

`event/` 是 Event 协议的唯一用户入口。根包不 re-export `event.Input`、`event.Output`、`event.InputPart`、`event.OutputPart`，也不提供 `Prompt`、`Steer`、`Abort` 这类 Event 构造函数。这样用户只需要理解一个 Event 包，避免同一类型同时出现在 `blades` 和 `event` 两个命名空间。

`Run` 的 channel 直接承载具体事件：

```go
input <- event.PromptText("hello")

for out := range output {
    switch e := out.(type) {
    case event.TextDelta:
        // e.Text is the streamed text delta
    case event.TurnEnd:
        // one model turn ended
    case event.Done:
        // agent lifecycle ended
    }
}
```

运行上下文使用 `context.Context` 传递取消、deadline、trace 和少量 typed capability。Session 是 Agent Loop 的核心运行能力，当前会话 ID 从 `session.Session` 自身读取：

```go
ctx = session.NewContext(ctx, sess)

sess, ok := session.FromContext(ctx)
if ok {
    sessionID := sess.ID()
}
```

Core 保留 capability-specific context helper，例如 `session.NewContext/FromContext`、Agent 内省 context 和 `tools.Context`。`AppID`、`UserID`、`ChannelID`、`WorkspaceID`、chat ID、platform ID 和 notification target 由应用层自己的映射、context key 或事件 payload 管理。`TraceID` 使用 OpenTelemetry context 传播；动态业务字段使用具体事件或 Hook payload。context 中不要放大对象、可变 map、消息历史或工具结果。

### Event 类型

```go
package event

type Input interface{ input() }
type Output interface{ output() }
type InputPart interface{ inputPart() }
type OutputPart interface{ outputPart() }
```

输入事件：

| 事件 | 用途 |
|------|------|
| `Prompt` | 用户或系统发起一个新 turn |
| `Steer` | Agent 运行中注入修正、追加上下文或继续指令 |
| `Control` | Abort / Pause / Resume 控制信号 |
| `Notification` | runtime、worker、后台任务或应用层接入注入的内部通知 |

输出事件：

| 事件 | 用途 |
|------|------|
| `TextDelta` / `ThinkingDelta` | 文本和 thinking 的常用流式输出 |
| `PartStart` / `PartDelta` / `PartEnd` | 多模态内容生命周期和高级增量输出 |
| `ToolStart` / `ToolDelta` / `ToolEnd` | 工具执行生命周期 |
| `TurnEnd` | 一个模型 turn 结束，包含 `StopReason` 和 usage |
| `Error` | 可恢复或终止错误 |
| `Done` | Agent 生命周期结束 |

输入和输出都必须支持多模态 Part。输入 Part 包括 `TextInput`、`FileInput`、`DataInput`、`JSONInput`；输出 Part 包括 `TextOutput`、`ThinkingOutput`、`FileOutput`、`DataOutput`、`JSONOutput`。普通文本输入使用 `event.PromptText` / `event.SteerText`，普通文本输出使用 `event.TextDelta`；完整最终多模态结果仍在 `PartEnd.Part` 和 `TurnEnd.Parts` 中。工具结果在 `tools/` 中也是多模态 DTO，由 Agent Loop 转成 `event.OutputPart` 和 `model.ToolResultPart`。

Event 和 Message 不合并。原因：

- Event 面向用户、应用接入、hook 和 runtime，包含 streaming、control、notification、tool lifecycle。
- Message 面向 LLM provider、session、compression，必须满足 provider message invariant。
- 两者变化频率不同，合并会导致 Event 层依赖 model 层，并把 provider 约束泄漏到用户 API。
- Agent Loop 是自然转换边界：`event.Input -> model.Message/Part -> Provider -> event.Output`。

## 总体架构

```
┌─────────────────────────────────────────────────────────────────┐
│  Application Layer（outside core / user-owned）                   │
│    cmd/<app>/internal/app       应用定义、依赖装配、运行配置         │
│    cmd/<app>/internal/channel   CLI、微信、飞书等通道                │
│    cmd/<app>/internal/workspace 工作目录、应用配置、资源治理         │
├─────────────────────────────────────────────────────────────────┤
│  blades/（根包：最小用户 API）                                     │
│    Agent, New, Option, runner helpers                             │
├─────────────────────────────────────────────────────────────────┤
│  Agent Runtime                                                    │
│    internal/loop   Event-driven Agent Loop                         │
│    flow/           Sequential / Parallel / Loop 组合原语            │
├─────────────────────────────────────────────────────────────────┤
│  Capability Layer                                                 │
│    tools/      工具接口、Resolver、Result DTO                       │
│    policy/     权限、安全、预算、速率限制、组织规则                   │
│    hook/       生命周期事件与拦截点                                  │
│    compact/    上下文压缩管线                                       │
│    memory/     Memory 加载、召回、提取                              │
│    session/    会话接口、Store、消息树、JSONL                         │
├─────────────────────────────────────────────────────────────────┤
│  Protocol Leaf Packages                                           │
│    event/      Input/Output Event + 多模态 Part，不依赖 model/tools   │
│    model/      Message/Part/Provider/Request/Response/Counter        │
│    tools/      Tool/Result/Resolver，不依赖 event/model              │
├─────────────────────────────────────────────────────────────────┤
│  Optional Systems                                                  │
│    graph/      DAG 执行器，可选工作流系统                            │
│    evaluator/  评估系统                                              │
│    recipe/     声明式 Agent 构建                                     │
├─────────────────────────────────────────────────────────────────┤
│  contrib/                                                            │
│    openai / anthropic / gemini / mcp / otel                          │
└─────────────────────────────────────────────────────────────────┘
```

## 设计原则

| 原则 | 决策 |
|------|------|
| 根包极简 | `blades/` 只放 `Agent`、`New`、Option、必要错误和纯 Agent runner helper |
| Event 是协议叶子包 | `event/` 不导入 `model/`、`tools/`、`session/`、`hook/` |
| Message 是模型协议 | `model/` 不导入 `event/` 或 `tools/`，Provider 只处理 `model.Request/Response` |
| Tool 是能力叶子包 | `tools/` 不导入 `event/` 或 `model/`，结果使用 `tools.ResultPart` |
| Runtime 做转换 | `internal/loop` 是 Event、Tool、Message 之间的唯一编排和转换边界 |
| Context 承载运行能力 | context 传递取消、deadline、trace、`session.Session`、Agent 内省和 `tools.Context` |
| Prompt 独立 | system prompt 构建放在 `prompt/`，根包不导出 `PromptBuilder` |
| Middleware 独立 | 输入/输出 middleware 放在 `middleware/`，只操作 Event channel |
| Composition 不污染根包 | `Sequential/Parallel/Loop` 放 `flow/`，读作 `flow.Sequential(...)` |
| 应用接入框架外实现 | CLI/HTTP/WebSocket/Slack/Scheduler 等属于具体应用，不作为 AgentOS 核心公开包 |
| Policy stdlib-only | `policy/` 统一承载权限、安全、预算、速率限制和组织规则；包内只依赖标准库 |
| Coding 不是核心 | `Explore/Plan/General/Verify` 不进 v1 核心；可放 examples、contrib preset 或业务 app |

## 包结构

```
blades/
├── agent.go                    Agent 接口 + New 构造函数
├── option.go                   AgentOption + WithModel/WithTools/WithSession/...
├── runner.go                   纯 Agent 运行辅助
├── errors.go
│
├── prompt/
│   ├── builder.go              Builder + Section + CacheBreakpoint
│   └── prompt.go               SystemPrompt + cache metadata
│
├── middleware/
│   └── middleware.go           InputMiddleware / OutputMiddleware
│
├── event/
│   ├── event.go                Input, Output, Control, Notification
│   ├── input.go                Prompt, Steer, PromptText/SteerText, InputPart, TextInput/FileInput/DataInput/JSONInput
│   ├── output.go               OutputPart, TextOutput/ThinkingOutput/FileOutput/DataOutput/JSONOutput
│   ├── stream.go               PartStart, TextDelta, ThinkingDelta, PartDelta, PartEnd
│   ├── tool.go                 ToolStart, ToolDelta, ToolEnd
│   └── turn.go                 Usage, StopReason, TurnEnd, Error, Done
│
├── model/
│   ├── message.go              Message, Role, Status
│   ├── part.go                 Part, TextPart, FilePart, DataPart, ToolUsePart, ToolResultPart, ThinkingPart
│   ├── provider.go             Provider
│   ├── request.go              Request, Response, ToolSpec
│   ├── token.go                TokenUsage
│   └── counter.go              Counter + CharCounter/ProviderCounter/CachedCounter
│
├── tools/
│   ├── tool.go                 Tool 核心接口 + 可选能力接口
│   ├── result.go               Result + ResultPart
│   ├── resolver.go             Resolver
│   ├── filter.go               ToolFilter + ReadOnly/AllowOnly/Disallow/And/Or
│   └── context.go              工具执行上下文辅助
│
├── flow/
│   ├── sequential.go           Sequential(...) blades.Agent
│   ├── parallel.go             Parallel(...) blades.Agent
│   ├── loop.go                 Loop(...) blades.Agent
│   └── tool.go                 AsTool(agent) tools.Tool，可选 Agent-as-Tool 适配
│
├── session/
│   ├── session.go              Session 接口 + NewMemory/Open
│   ├── store.go                Store + Writer + Header + Snapshot
│   ├── entry.go                Entry 联合类型
│   ├── tree.go                 Tree + Branch/Leaf/Path
│   ├── memory.go
│   ├── persistent.go
│   └── file.go
│
├── compact/
│   ├── pipeline.go             Pipeline
│   ├── strategy.go             Strategy
│   ├── budget.go               ToolResultBudget
│   ├── summary.go              LLM 摘要策略，Summarizer 函数注入
│   └── invariant.go            Provider invariant 保护
│
├── hook/
│   ├── event.go                hook.Event 判别联合
│   ├── registry.go             Registry
│   └── handler.go              Observe / Intercept handlers
│
├── policy/
│   ├── decision.go             Decision
│   ├── request.go              Request + ToolRequest/ModelRequest/ResourceRequest
│   ├── chain.go                Chain
│   ├── rule.go                 Rule
│   ├── safety.go               SafetyChecker
│   ├── budget.go               BudgetPolicy
│   └── rate.go                 RateLimiter
│
├── memory/
├── graph/
├── recipe/
├── evaluator/
├── internal/
│   └── loop/
└── contrib/
```

### runner.go 边界

`runner.go` 可以留在根包，因为它直接服务 `blades.Agent` 的基本运行体验。边界必须足够硬：它只能提供同步调用、drain、collect、一次性 run 等无状态 helper，帮助调用方把 `Agent.Run` 的 channel API 用得更顺手。

`runner.go` 不承载 run manager 语义，不定义 run ID、队列、daemon、cron、后台 job、主动通知、channel adapter、workspace 映射、配置加载或 session 映射。这些都属于应用接入层。

### 为什么不用根包放 Sequential / Parallel / Loop

`Sequential`、`Parallel`、`Loop` 是通用组合能力，但不是所有用户创建 Agent 都需要。放在根包会让根包承担运行时编排语义，并和 `AgentOption`、基础构造 API 混在一起。保留 `flow/` 更符合 Go 的包边界：

- `blades.Agent` 是最小接口。
- `flow.Sequential` 读作组合领域的构造函数。
- `flow/` 可以依赖根包，根包不依赖 `flow/`，无循环。
- 原有 `flow/` 可迁移保留，但只留下通用三件套；`Routing`、`Deep` 这类策略型 Agent 不进入核心。

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
event/      -> standard library only
model/      -> standard library only
tools/      -> standard library only + jsonschema

session/    -> model/
compact/    -> model/                         // Summarizer 函数由上层注入
memory/     -> model/                         // Fork/Recall 函数由上层注入
policy/     -> standard library only
hook/       -> event/, model/, tools/, policy/

blades/     -> event/, model/, tools/, session/, compact/, hook/, policy/, prompt/
flow/       -> blades/, event/, tools/

recipe/     -> blades/, tools/, model/, prompt/
contrib/*   -> model/ 或 tools/
internal/loop -> event/, model/, tools/, session/, compact/, hook/, policy/, prompt/
```

循环依赖规避规则：

- `event/`、`model/`、`tools/` 三个协议/能力叶子包互不依赖。
- `policy/` 只依赖标准库，不导入 `tools/`、`model/`、`event/`、`blades/` 或 `hook/`；工具元数据到 policy request 的映射在 Agent Loop 或应用 bridge 中完成。
- `compact/` 不依赖 Provider 或 root Agent；摘要能力通过 `func(ctx, []*model.Message) (string, error)` 注入。
- `memory/` 不依赖 root Agent；提取和召回通过函数接口注入。
- CLI、HTTP、WebSocket、Slack、Scheduler 等接入层不进入核心依赖图；具体应用自行把外部协议转换成 `event.Input` / `event.Output`。
- 后台运行、队列、drain、取消、主动通知等运行管理不进入核心依赖图；具体应用可以在 `cmd/<app>/internal` 内实现。

## 核心包导出类型

| 包 | 核心类型 | 示例 |
|----|----------|------|
| `blades` | `Agent`, `Option`, runner helpers | `blades.Agent` |
| `event` | `Input`, `Output`, `Prompt`, `Steer`, `Control`, `Notification`, `TextDelta`, `PartDelta`, `TurnEnd`, `Done` | `event.PromptText` |
| `model` | `Message`, `Part`, `Provider`, `Request`, `Response`, `ToolSpec`, `Counter` | `model.Provider` |
| `tools` | `Tool`, `Result`, `ResultPart`, `Resolver`, `ToolFilter` | `tools.Tool` |
| `prompt` | `Builder`, `Section`, `SystemPrompt`, `Breakpoint` | `prompt.Builder` |
| `flow` | `Sequential`, `Parallel`, `Loop`, `AsTool` | `flow.Parallel(a, b)` |
| `session` | `Session`, `Store`, `Writer`, `Entry`, `Tree` | `session.Session` |
| `policy` | `Chain`, `Decision`, `Rule`, `SafetyChecker`, `BudgetPolicy`, `RateLimiter` | `policy.Chain` |
| `hook` | `Event`, `Registry`, handlers | `hook.Registry` |
| `compact` | `Pipeline`, `Strategy`, `PostCompactRestorer` | `compact.Pipeline` |
| `memory` | `Store`, `Loader`, `Extractor`, `Recaller` | `memory.Store` |

## 模块详细设计

### Event 系统与 Agent Loop

Event 是用户协议层，定义 `event.Input` / `event.Output` 和多模态 `InputPart` / `OutputPart`。Event 不依赖 `model/`，也不和 `model.Message` 共享 Go 类型。Agent Loop 是状态机（Idle -> Preparing -> Streaming -> Acting），负责：

- 把 `event.Prompt` / `event.Steer` 转成 `model.Message`。
- 用 ContextBuilder 从 Session、Memory、`prompt.Builder`、ToolSpec 构建 `model.Request`。
- 调用 `model.Provider`。
- 把 `model.Response` 转成 `event.TextDelta`、`event.Part*`、`event.Tool*`、`event.TurnEnd`。
- 把工具结果同时转成 `event.OutputPart` 和 `model.ToolResultPart`。

详细定义见 [design-event-agent-loop.md](design-event-agent-loop.md)。

### Agent Runtime

Runtime 包括根包 `blades/`、`internal/loop` 和 `flow/`。

`blades.New(name, opts...)` 创建基础 Agent。基础 Agent 持有 model provider、tools resolver、session provider、prompt builder、policy chain、hook registry、compact pipeline、memory loader 等配置，但这些能力都通过接口注入。

`flow.Sequential/Parallel/Loop` 返回普通 `blades.Agent`：

```go
pipeline := flow.Sequential(researcher, planner, executor)
race := flow.Parallel(indexSearch, vectorSearch, webSearch)
iterative := flow.Loop(worker, flow.WithMaxTurns(8))
```

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

原 `permission/` 命名过窄。AgentOS 需要统一处理权限、安全、预算、速率限制和组织策略，因此核心包命名为 `policy/`。

`policy.Chain` 接收 core-sealed 的 `ToolRequest`、`ModelRequest`、`ResourceRequest`，返回 allow/deny/ask/modify。应用如果要接入额外资源类型，应先映射为 `ResourceRequest` 或在自己的 checker 中解释这些稳定 DTO；不要让 `policy/` 导入应用类型。Plan Mode、Accept Edits、Auto Mode 不作为 AgentOS 核心目标；它们是产品交互策略，应放在具体应用、examples 或 contrib 包中，基于 core policy primitives 组合实现。

### 子 Agent 与 Multi-Agent

v1 核心只保留两个通用原语：

- `flow.*`：同进程 Agent 组合。
- `flow.AsTool(agent)`：把一个 Agent 暴露为 `tools.Tool`，让另一个 Agent 调用。

不在核心内置 `BackgroundAgent`、`WorktreeAgent`、`team/Coordinator` 或 `Swarm/Team`。这些能力可以在应用层、`recipe/` 或 contrib 包中组合出来。原因：

- Background 是运行生命周期问题，应由应用层 run manager 管理，而不是改变 Agent 类型。
- Worktree 是 coding workspace 隔离策略，不适合通用 AgentOS 核心。
- Team/Swarm 是应用级协作协议，应该建立在 Agent、Tool、Session 和应用层调度之上。

如果后续需要通用多 Agent，可以新增 `orchestrator/` 包，命名为 `orchestrator.Coordinator`、`orchestrator.Team`，而不是 `team/`。`team` 太偏人类团队语义，通用 AgentOS 中 `orchestrator` 更准确。

### Session / Memory / Compact

`session.Session` 面向 Agent Loop，只提供消息历史、追加、分支和状态访问。Entry、Tree、JSONL、Store 是持久化层细节。

`compact.Pipeline` 从 Session 中取出的 `[]*model.Message` 进行预算控制和摘要，不依赖 root Agent。LLM 摘要通过 Summarizer 函数注入。

`memory/` 负责多层 Memory 加载、召回和提取。Memory 提取不要求框架内置 BackgroundAgent；应用层可以启动独立 run 或异步 job，把提取结果写回 memory store。

## 子文档职责

当前 v1 子文档按稳定边界拆分：

| 子文档 | 职责 |
|--------|------|
| [design-event-agent-loop.md](design-event-agent-loop.md) | Event 协议、Agent Loop 状态机、Event/Message 转换边界 |
| [design-model-provider.md](design-model-provider.md) | `model/` Message、Part、Provider、Request/Response、重试、token 计数 |
| [design-prompt.md](design-prompt.md) | `prompt/` Builder、Section、缓存断点和静态/动态 prompt 组织 |
| [design-compact.md](design-compact.md) | `compact/` Pipeline、Strategy、工具结果预算、不变量保护和摘要注入 |
| [design-tool-system.md](design-tool-system.md) | `tools/` Tool、Result、Resolver、Filter 和执行上下文 |
| [design-hook-extension.md](design-hook-extension.md) | core-sealed hook event、观察/拦截边界和应用事件隔离 |
| [design-session.md](design-session.md) | Session、Store、Entry、Tree 和持久化 |
| [design-policy-mode.md](design-policy-mode.md) | stdlib-only policy core 与应用交互模式边界 |
| [design-agent-orchestration.md](design-agent-orchestration.md) | `flow/` 组合、Agent-as-Tool 和多 Agent 边界 |
| [design-memory.md](design-memory.md) | Memory 加载、召回、提取与应用异步任务边界 |
| [design-observability.md](design-observability.md) | Hook 驱动的 OTel 集成和 span 生命周期 |
| [design-graph.md](design-graph.md) | 独立 DAG 子系统定位和与 Agent 的桥接方式 |
| [design-migration.md](design-migration.md) | 旧 API 到 v1 目标架构的迁移路径 |

`design-message-context.md` 与 `design-infra.md` 保留为历史聚合文档，不再作为 v1 主索引入口。`design-streaming-optimization.md` 是已实现流式优化参考，不属于 v1 子设计。

## 实现计划

### 阶段 1：协议与 Agent Loop

- [ ] 定义 `event/`：`Input` / `Output`、多模态 `InputPart` / `OutputPart`、`Prompt`、`Steer`、`PromptText`、`SteerText`、`Control`、`Notification`、`TextDelta`、`ThinkingDelta`、`Part*`、`Tool*`、`TurnEnd`、`Error`、`Done`
- [ ] 根包不 re-export Event 类型或构造函数；用户代码统一导入 `event/`
- [ ] Agent 接口改为 `Run(context.Context, <-chan event.Input) (<-chan event.Output, error)`
- [ ] 实现 Agent Loop 对 `session.Session`、Agent 内省 context 和 `tools.Context` 的读取与传递
- [ ] 实现 Event -> model.Message/Part -> Provider -> Event 转换边界
- [ ] 在 `middleware/` 实现 InputMiddleware / OutputMiddleware

### 阶段 2：model/session/compact

- [ ] 实现 `model/`：Message、Part、Provider、Request、Response、ToolSpec、Counter
- [ ] 实现 `session.Session` 与 `session.Store`
- [ ] 实现 ContextBuilder 和 streamAndRecord
- [ ] 实现 `compact.Pipeline` 与 provider invariant 保护
- [ ] 在 `prompt/` 实现 `Builder` 静态/动态 section 与 cache breakpoint

### 阶段 3：tools/policy/hook

- [ ] 精简 `tools.Tool`，定义 `tools.ResultPart`
- [ ] 实现 ToolFilter、Resolver、StreamingToolExecutor
- [ ] 实现 `policy.Chain`、SafetyChecker、BudgetPolicy、RateLimiter
- [ ] 实现 `hook.Registry` 和 Agent/Model/Tool 生命周期事件
- [ ] 在 Agent Loop 中串联 tool validation、policy、hook、execution、result conversion

### 阶段 4：应用接入样板与边界验证

- [ ] 用 `cmd/<app>/internal/channel`、`cmd/<app>/internal/app`、`cmd/<app>/internal/workspace` 展示应用层样板
- [ ] 把 CLI/HTTP/WebSocket 等外部协议保留在具体应用、examples 或 contrib，不进入 Agent core
- [ ] 用示例说明应用层如何管理 run lifecycle、session 映射、主动通知和配置加载

### 阶段 5：组合、应用与迁移

- [ ] 保留并重构 `flow.Sequential/Parallel/Loop`，去掉 Routing/Deep
- [ ] 在 `flow/` 中实现可选 `flow.AsTool(agent)`
- [ ] 迁移 contrib providers 到 `model.Provider`
- [ ] 迁移 skills 到新 `tools.Tool`
- [ ] 把 coding presets 移到 `examples/coding` 或 `contrib/preset`

## 风险与缓解

| 风险 | 影响 | 缓解 |
|------|------|------|
| Event 接口变更大 | 高 | 保持 `event/` 作为唯一入口，迁移围绕 `event.Input` / `event.Output` 和具体事件类型 |
| Event/Message 转换复杂 | 高 | 转换只允许在 `internal/loop`，增加 golden tests 覆盖多模态和工具调用 |
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
- [Observability](design-observability.md)
- [Graph 定位](design-graph.md)
- [迁移路径](design-migration.md)
- [Claude Code Agent 参考设计](reference-claude-code-agent.md)
- [pi-agent Framework 参考设计](reference-pi-agent-framework.md)
- [流式响应优化设计](design-streaming-optimization.md)（历史已实现参考，不属于 AgentOS v1 子设计）
