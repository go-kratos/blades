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
  - design-message-context.md
  - design-tool-system.md
  - design-hook-extension.md
  - design-session.md
  - design-policy-mode.md
  - design-agent-orchestration.md
  - design-memory.md
  - design-infra.md
  - design-migration.md
  - design-streaming-optimization.md
---

# Blades AgentOS Framework 设计蓝图

## 背景与目标

Blades 的目标不是专用 Coding Agent，而是一个通用 AgentOS Runtime Platform：它提供 Agent 的事件协议、运行循环、上下文构建、工具编排、会话持久化、策略决策、Hook、Memory、Host 与 Channel 接入能力。Coding、客服、数据分析、自动化运维、研究助手等都应是运行在 AgentOS 上的应用或预设，而不是核心架构的默认假设。

当前 Blades 已有 `Agent`、基于 `iter.Seq2` 的流式接口、`Invocation`、`Session`、`Middleware`、`flow/`、`graph/`、`tools/`、`skills/`、`memory/`、`recipe/` 和多 Provider 集成。新设计不考虑向后兼容，目标是把这些能力重组为清晰分层的 AgentOS。

核心目标：

- **事件驱动**：外部只通过 `event.Input` / `event.Output` 与 Agent 通信，channel 中直接传具体事件，不做 `Input{Event: ...}` 这类二次封装。
- **Event / Message 分层**：Event 是用户协议层，Message 是模型上下文协议层；`event/` 不依赖 `model/`，Agent Loop 是唯一转换边界。
- **通用 AgentOS**：核心只提供通用 runtime、channel、workspace、policy、session、tool、memory 等能力，不内置 coding-specific workflow。
- **包依赖可证明**：协议叶子包不互相依赖；上层通过接口、函数注入和 context scope 连接，避免循环依赖。
- **Go 惯用 API**：小接口、短包名、`package.Role` 命名、`context.Context` 取消与 trace 传播、Option 函数配置。
- **流式优先**：Provider streaming、工具重叠执行、输出背压、运行中 steering/control 都是一等能力。

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

稳定运行信息通过 context 传递，不塞进每个事件：

```go
package scope

type Scope struct {
    RunID       string
    AppID       string
    AgentID     string
    SessionID   string
    UserID      string
    ChannelID   string
    WorkspaceID string
}

func NewContext(ctx context.Context, s Scope) context.Context
func FromContext(ctx context.Context) (Scope, bool)
```

`Scope` 在一次 `Run` 内保持稳定。`TraceID` 使用 OpenTelemetry context 传播；动态业务字段使用具体事件或 Hook payload，不放入 context。context 中不要放大对象、可变 map、消息历史或工具结果。

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
| `Control` | Abort / Pause / Resume 等控制信号 |
| `Notification` | runtime、worker、后台任务或 channel 注入的内部通知 |

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

- Event 面向用户、channel、hook 和 runtime，包含 streaming、control、notification、tool lifecycle。
- Message 面向 LLM provider、session、compression，必须满足 provider message invariant。
- 两者变化频率不同，合并会导致 Event 层依赖 model 层，并把 provider 约束泄漏到用户 API。
- Agent Loop 是自然转换边界：`event.Input -> model.Message/Part -> Provider -> event.Output`。

## 总体架构

```
┌─────────────────────────────────────────────────────────────────┐
│  AgentOS Application Layer                                       │
│    app/       应用定义、依赖装配、运行配置                         │
│    channel/   CLI、HTTP、WebSocket、Slack、Scheduler 等通道          │
│    host/      Run 管理、Agent 生命周期、channel 接入、资源治理       │
├─────────────────────────────────────────────────────────────────┤
│  blades/（根包：最小用户 API）                                     │
│    Agent, New, Option, PromptBuilder                              │
├─────────────────────────────────────────────────────────────────┤
│  Agent Runtime                                                    │
│    internal/loop   Event-driven Agent Loop                         │
│    flow/           Sequential / Parallel / Loop 组合原语            │
├─────────────────────────────────────────────────────────────────┤
│  Capability Layer                                                 │
│    tools/      工具接口、Resolver、Result DTO                       │
│    policy/     权限、安全、模式、预算、速率限制                       │
│    hook/       生命周期事件与拦截点                                  │
│    compact/    上下文压缩管线                                       │
│    memory/     Memory 加载、召回、提取                              │
│    session/    会话接口、Store、消息树、JSONL                         │
│    workspace/  工作目录、文件系统边界、artifact、环境                 │
├─────────────────────────────────────────────────────────────────┤
│  Protocol Leaf Packages                                           │
│    event/      Input/Output Event + 多模态 Part，不依赖 model/tools   │
│    model/      Message/Part/Provider/Request/Response/Counter        │
│    tools/      Tool/Result/Resolver，不依赖 event/model              │
├─────────────────────────────────────────────────────────────────┤
│  Optional Systems                                                  │
│    graph/      DAG 执行器，可选工作流系统                            │
│    evaluator/  评估系统                                              │
│    recipe/     声明式应用构建                                        │
├─────────────────────────────────────────────────────────────────┤
│  contrib/                                                            │
│    openai / anthropic / gemini / mcp / otel / channel adapters       │
└─────────────────────────────────────────────────────────────────┘
```

## 设计原则

| 原则 | 决策 |
|------|------|
| 根包极简 | `blades/` 只放 `Agent`、`New`、Option、`PromptBuilder` 和通用运行辅助 |
| Event 是协议叶子包 | `event/` 不导入 `model/`、`tools/`、`session/`、`hook/` |
| Message 是模型协议 | `model/` 不导入 `event/` 或 `tools/`，Provider 只处理 `model.Request/Response` |
| Tool 是能力叶子包 | `tools/` 不导入 `event/` 或 `model/`，结果使用 `tools.ResultPart` |
| Runtime 做转换 | `internal/loop` 是 Event、Tool、Message 之间的唯一编排和转换边界 |
| Scope 走 context | `SessionID/UserID/ChannelID/WorkspaceID` 这类稳定信息放 `scope.Scope` |
| Composition 不污染根包 | `Sequential/Parallel/Loop` 放 `flow/`，读作 `flow.Sequential(...)` |
| 应用接入独立 | CLI/HTTP/WebSocket/Slack/Scheduler 等属于 `channel/` 和 `host/`，不是 Agent 接口的一部分 |
| Policy 大于 Permission | `policy/` 统一承载权限、安全检查、交互模式、预算、速率限制和组织规则 |
| Coding 不是核心 | `Explore/Plan/General/Verify` 不进 v1 核心；可放 examples、contrib preset 或业务 app |

## 包结构

```
blades/
├── agent.go                    Agent 接口 + New 构造函数
├── option.go                   AgentOption + WithModel/WithTools/WithSession/...
├── prompt.go                   PromptBuilder + Section + CacheBreakpoint
├── middleware.go               InputMiddleware / OutputMiddleware
├── runner.go                   简单运行辅助
├── errors.go
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
├── scope/
│   └── scope.go                Scope + context helper
│
├── flow/
│   ├── sequential.go           Sequential(...) blades.Agent
│   ├── parallel.go             Parallel(...) blades.Agent
│   ├── loop.go                 Loop(...) blades.Agent
│   └── tool.go                 AsTool(agent) tools.Tool，可选 Agent-as-Tool 适配
│
├── app/
│   ├── app.go                  App 定义，装配 agent/channel/policy
│   ├── config.go               Config 结构、默认值、合并规则
│   └── builder.go              应用构建辅助
│
├── host/
│   ├── host.go                 Host 管理 Run 生命周期
│   ├── run.go                  Run handle、取消、drain、状态查询
│   └── scheduler.go            可选调度入口
│
├── channel/
│   ├── channel.go              Channel 接口，Event 与外部协议桥接
│   ├── cli/
│   ├── http/
│   └── websocket/
│
├── workspace/
│   ├── workspace.go            Workspace、PathPolicy、ArtifactStore
│   └── fs.go                   文件系统边界与路径规范化
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
│   ├── chain.go                Chain
│   ├── rule.go                 Rule
│   ├── mode.go                 ModeManager
│   ├── safety.go               SafetyChecker
│   ├── budget.go               BudgetPolicy
│   └── rate.go                 RateLimiter
│
├── memory/
├── graph/
├── recipe/
├── evaluator/
├── middleware/
├── internal/
│   └── loop/
└── contrib/
```

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
- 预设 Agent 应该由 app、recipe、examples 或 contrib preset 提供。

推荐替代：

- `examples/coding/`：放 coding app 示例，包含 Explore/Plan/Verify 的 recipe。
- `contrib/preset/`：可选预设包，提供 `preset.Assistant`、`preset.Researcher` 等通用模板。
- 业务项目自己定义 package，例如 `support.Agent()`、`ops.Agent()`、`coding.Explore()`。

## 依赖关系

```
event/      -> standard library only
model/      -> standard library only
tools/      -> standard library only + jsonschema
scope/      -> context only

session/    -> model/
compact/    -> model/                         // Summarizer 函数由上层注入
memory/     -> model/                         // Fork/Recall 函数由上层注入
workspace/  -> standard library
policy/     -> tools/, workspace/             // 不依赖 blades/event/model
hook/       -> event/, model/, tools/, policy/

blades/     -> event/, model/, tools/, session/, compact/, hook/, policy/, scope/
flow/       -> blades/, event/, tools/

channel/    -> event/, scope/
host/       -> blades/, channel/, scope/, session/, workspace/
app/        -> blades/, host/, channel/, policy/
recipe/     -> app/, blades/, tools/, model/
contrib/*   -> model/ 或 channel/ 或 tools/
internal/loop -> event/, model/, tools/, session/, compact/, hook/, policy/, scope/
```

循环依赖规避规则：

- `event/`、`model/`、`tools/` 三个协议/能力叶子包互不依赖。
- `policy/` 不依赖 `blades/`，否则工具权限会和 Agent 构造形成循环。
- `compact/` 不依赖 Provider 或 root Agent；摘要能力通过 `func(ctx, []*model.Message) (string, error)` 注入。
- `memory/` 不依赖 root Agent；提取和召回通过函数接口注入。
- `channel/` 只做外部协议与 Event 的转换，不直接调用 Provider。
- `host/` 负责装配 context scope、channel、agent run 和 lifecycle，是 AgentOS 运行层。

## 核心包导出类型

| 包 | 核心类型 | 示例 |
|----|----------|------|
| `blades` | `Agent`, `Option`, `PromptBuilder` | `blades.Agent` |
| `event` | `Input`, `Output`, `Prompt`, `Steer`, `Control`, `Notification`, `TextDelta`, `PartDelta`, `TurnEnd`, `Done` | `event.PromptText` |
| `model` | `Message`, `Part`, `Provider`, `Request`, `Response`, `ToolSpec`, `Counter` | `model.Provider` |
| `tools` | `Tool`, `Result`, `ResultPart`, `Resolver`, `ToolFilter` | `tools.Tool` |
| `scope` | `Scope`, `NewContext`, `FromContext` | `scope.Scope` |
| `flow` | `Sequential`, `Parallel`, `Loop`, `AsTool` | `flow.Parallel(a, b)` |
| `app` | `App`, `Builder`, `Config` | `app.New(...)` |
| `host` | `Host`, `Run` | `host.Start(ctx, app)` |
| `channel` | `Channel`, `Envelope`, adapters | `channel.Channel` |
| `workspace` | `Workspace`, `PathPolicy`, `ArtifactStore` | `workspace.Workspace` |
| `session` | `Session`, `Store`, `Writer`, `Entry`, `Tree` | `session.Session` |
| `policy` | `Chain`, `Decision`, `Rule`, `ModeManager`, `SafetyChecker` | `policy.Chain` |
| `hook` | `Event`, `Registry`, handlers | `hook.Registry` |
| `compact` | `Pipeline`, `Strategy`, `PostCompactRestorer` | `compact.Pipeline` |
| `memory` | `Store`, `Loader`, `Extractor`, `Recaller` | `memory.Store` |

## 模块详细设计

### Event 系统与 Agent Loop

Event 是用户协议层，定义 `event.Input` / `event.Output` 和多模态 `InputPart` / `OutputPart`。Event 不依赖 `model/`，也不和 `model.Message` 共享 Go 类型。Agent Loop 是状态机（Idle -> Preparing -> Streaming -> Acting），负责：

- 把 `event.Prompt` / `event.Steer` 转成 `model.Message`。
- 用 ContextBuilder 从 Session、Memory、PromptBuilder、ToolSpec 构建 `model.Request`。
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

### AgentOS Host / Channel

`host/` 是 AgentOS 的运行入口，负责：

- 为每次运行创建 `scope.Scope` 并写入 context。
- 连接一个或多个 `channel.Channel`。
- 管理 Agent Run 生命周期、取消、drain、错误归档。
- 注入 workspace、session、policy、配置好的 Agent 依赖和 telemetry。

配置不单独设计 `settings/` 包。`app.Config` 是应用装配输入，负责承载模型、工具、policy、channel、workspace、session 等声明式配置；文件加载、环境变量覆盖和默认值合并可以放在 `app.LoadConfig` 或具体 CLI 中。`host/` 只消费已经构造好的 Agent、Channel 和 Option，不读取配置文件。

`channel/` 是外部协议适配层。CLI、HTTP、WebSocket、Slack、Cron、Queue 都实现 Channel，把外部 envelope 转为 `event.Input`，再把 `event.Output` 转回目标协议。

Agent 不知道自己来自 CLI 还是 HTTP；它只读 input channel 和 context scope。

### Policy 系统

原 `permission/` 命名过窄。AgentOS 需要统一处理权限、安全、模式、预算、速率限制、组织策略和 workspace 边界，因此核心包命名为 `policy/`。

`policy.Chain` 接收 tool call、workspace operation、model request 或 channel action 的决策请求，返回 allow/deny/ask/modify。Plan Mode、Accept Edits、Auto Mode 可以作为 policy mode 的实现，但不作为 AgentOS 核心目标；它们是交互策略，不是 Agent 接口的一部分。

### 子 Agent 与 Multi-Agent

v1 核心只保留两个通用原语：

- `flow.*`：同进程 Agent 组合。
- `flow.AsTool(agent)`：把一个 Agent 暴露为 `tools.Tool`，让另一个 Agent 调用。

不在核心内置 `BackgroundAgent`、`WorktreeAgent`、`team/Coordinator` 或 `Swarm/Team`。这些能力可以在 `host/`、`workspace/`、`channel/queue`、`recipe/` 或 contrib 包中组合出来。原因：

- Background 是运行生命周期问题，应由 host/run handle 管理，而不是改变 Agent 类型。
- Worktree 是 coding workspace 隔离策略，不适合通用 AgentOS 核心。
- Team/Swarm 是应用级协作协议，应该建立在 Agent、Tool、Session、Channel 之上。

如果后续需要通用多 Agent，可以新增 `orchestrator/` 包，命名为 `orchestrator.Coordinator`、`orchestrator.Team`，而不是 `team/`。`team` 太偏人类团队语义，通用 AgentOS 中 `orchestrator` 更准确。

### Session / Memory / Compact

`session.Session` 面向 Agent Loop，只提供消息历史、追加、分支和状态访问。Entry、Tree、JSONL、Store 是持久化层细节。

`compact.Pipeline` 从 Session 中取出的 `[]*model.Message` 进行预算控制和摘要，不依赖 root Agent。LLM 摘要通过 Summarizer 函数注入。

`memory/` 负责多层 Memory 加载、召回和提取。Memory 提取不要求框架内置 BackgroundAgent；host 可以启动独立 run 或异步 job，把提取结果写回 memory store。

## 实现计划

### 阶段 1：协议与 Agent Loop

- [ ] 定义 `event/`：`Input` / `Output`、多模态 `InputPart` / `OutputPart`、`Prompt`、`Steer`、`PromptText`、`SteerText`、`Control`、`Notification`、`TextDelta`、`ThinkingDelta`、`Part*`、`Tool*`、`TurnEnd`、`Error`、`Done`
- [ ] 根包不 re-export Event 类型或构造函数；用户代码统一导入 `event/`
- [ ] Agent 接口改为 `Run(context.Context, <-chan event.Input) (<-chan event.Output, error)`
- [ ] 实现 `scope/` 包和 Agent Loop context scope 读取
- [ ] 实现 Event -> model.Message/Part -> Provider -> Event 转换边界
- [ ] 实现 InputMiddleware / OutputMiddleware

### 阶段 2：model/session/compact

- [ ] 实现 `model/`：Message、Part、Provider、Request、Response、ToolSpec、Counter
- [ ] 实现 `session.Session` 与 `session.Store`
- [ ] 实现 ContextBuilder 和 streamAndRecord
- [ ] 实现 `compact.Pipeline` 与 provider invariant 保护
- [ ] 实现 `PromptBuilder` 静态/动态 section 与 cache breakpoint

### 阶段 3：tools/policy/hook

- [ ] 精简 `tools.Tool`，定义 `tools.ResultPart`
- [ ] 实现 ToolFilter、Resolver、StreamingToolExecutor
- [ ] 实现 `policy.Chain`、SafetyChecker、ModeManager、BudgetPolicy
- [ ] 实现 `hook.Registry` 和 Agent/Model/Tool 生命周期事件
- [ ] 在 Agent Loop 中串联 tool validation、policy、hook、execution、result conversion

### 阶段 4：AgentOS host 层

- [ ] 实现 `scope.Scope`
- [ ] 实现 `workspace.Workspace`、PathPolicy、ArtifactStore
- [ ] 实现 `channel.Channel` 和 CLI channel
- [ ] 实现 `host.Host` 和 Run handle
- [ ] 实现 `app.Config`，在 app 层处理默认值、文件加载与环境覆盖
- [ ] 把 CLI/HTTP/WebSocket 等外部协议从 Agent core 移出

### 阶段 5：组合、应用与迁移

- [ ] 保留并重构 `flow.Sequential/Parallel/Loop`，去掉 Routing/Deep
- [ ] 在 `flow/` 中实现可选 `flow.AsTool(agent)`
- [ ] 实现 `app.App` 和 `recipe/` 构建
- [ ] 迁移 contrib providers 到 `model.Provider`
- [ ] 迁移 skills 到新 `tools.Tool`
- [ ] 把 coding presets 移到 `examples/coding` 或 `contrib/preset`

## 风险与缓解

| 风险 | 影响 | 缓解 |
|------|------|------|
| Event 接口变更大 | 高 | 保持 `event/` 作为唯一入口，迁移围绕 `event.Input` / `event.Output` 和具体事件类型 |
| Event/Message 转换复杂 | 高 | 转换只允许在 `internal/loop`，增加 golden tests 覆盖多模态和工具调用 |
| context 被滥用 | 中 | `scope.Scope` 只放稳定 ID，其他信息必须走 Event/Hook/Session |
| 包数量增加 | 中 | 保持叶子包小接口，Host/App 层负责装配，避免根包重新膨胀 |
| policy 语义过宽 | 中 | `Decision` 和请求类型保持小而稳定，模式/预算/组织规则通过可选实现扩展 |
| 去掉内置 coding presets 后示例不足 | 低 | 在 `examples/coding` 提供完整 app/recipe，不进入核心依赖路径 |
| flow 与 graph 边界模糊 | 中 | flow 只组合 Agent channel，graph 负责 DAG/checkpoint/condition |

## 参考资料

- [Event 系统与 Agent Loop](design-event-agent-loop.md)
- [消息与上下文系统](design-message-context.md)
- [工具系统](design-tool-system.md)
- [Hook 系统](design-hook-extension.md)
- [Session 系统](design-session.md)
- [迁移路径](design-migration.md)
- [Claude Code Agent 参考设计](reference-claude-code-agent.md)
- [pi-agent Framework 参考设计](reference-pi-agent-framework.md)
