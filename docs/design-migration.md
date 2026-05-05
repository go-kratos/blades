---
type: design
title: 迁移路径
parent: design-agent-framework.md
date: 2026-05-01
status: draft
modules: [module-13]
---

# 迁移路径

本次迁移不追求兼容旧 API，目标是把 Blades 重组为通用 AgentOS Runtime。迁移顺序应先稳定协议叶子包和 Agent Loop，再迁移 Provider、Tool、Session，最后用 `cmd/blades` 和 examples 展示应用层接入模式。

## 13.1 核心接口迁移

| 现有 | 新 | 迁移方式 |
|------|----|----------|
| `Agent.Run(ctx, *Invocation) iter.Seq2[*Message, error]` | `Agent.Run(ctx, <-chan event.Input) (<-chan event.Output, error)` | 去掉 Invocation，改为 Event channel 驱动 |
| `*Invocation` | 删除 | Prompt 进入 `event.Prompt`，Session 通过 typed context 传递，Agent 配置进入构造参数 |
| `iter.Seq2[*Message, error]` | `<-chan event.Output` | 消费端从 message stream 改为 event stream |
| `Message` 暴露在根包 | `model.Message` | Message 只属于模型上下文层 |
| `Middleware func(Handler) Handler` | `InputMiddleware` / `OutputMiddleware` / Hook | Event 流处理与生命周期 Hook 分离 |

`event.Input` 和 `event.Output` 是直接接口联合，channel 中直接传 `event.Prompt`、`event.Steer`、`event.TextDelta`、`event.PartDelta`、`event.ToolEnd` 等具体事件，不增加 `Input{Event: ...}` / `Output{Event: ...}` 包装层。

普通文本路径迁移到文本一等公民 API：

- 文本输入优先使用 `event.PromptText("hello")` 和 `event.SteerText("continue")`；多模态输入使用 `event.NewPrompt(event.Text("hello"), event.FileInput{...})` 或 struct literal。
- 文本流式输出从“`PartDelta` 携带 `TextOutput`”迁移为 `event.TextDelta{Text: ...}`。
- thinking 流式输出从“`PartDelta` 携带 `ThinkingOutput`”迁移为 `event.ThinkingDelta{Text: ...}`。
- `event.PartDelta` 只保留给非文本、非 thinking 的高级多模态增量；最终完整内容仍从 `event.PartEnd.Part` 或 `event.TurnEnd.Parts` 读取。

## 13.2 根包迁移

根包保留最小用户 API：

| 现有文件 | 去向 | 说明 |
|----------|------|------|
| `agent.go` | 保留并重构 | 定义 `Agent`、`New`、基础 agent 实现 |
| `message.go` | `model/message.go` + `model/part.go` | Message/Part 下沉到模型协议层 |
| `model.go` | `model/provider.go` + `model/request.go` | Provider、Request、Response、ToolSpec 进入 `model/` |
| `session.go` | `session/session.go` | Session 独立为 Agent Loop 面向接口 |
| `state.go` | `session/state.go` 或移除 | 状态随 Session 管理 |
| `compressor.go` | `compact/` | 压缩管线独立，并通过 Summarizer 函数注入模型能力 |
| `core.go` / `Invocation` | 删除 | Event + Session typed context + Agent 构造配置替代 Invocation |
| `middleware.go` | 保留并拆分 | Input/Output middleware 只处理 Event |
| `context.go` | `session/context.go` + capability-specific helper | Session、Agent 内省和 Tool 执行信息分别由各自 context helper 承载 |
| `event.go` | 删除 | 根包不保留 Event 类型别名或构造函数，用户统一导入 `event/` |

根包不放 `Sequential/Parallel/Loop`，这些组合原语保留在 `flow/`，读作 `flow.Sequential(...)`。根包也不内置 `Spawn`、`BackgroundAgent`、`WorktreeAgent`、`Team` 等应用级/场景级能力。

## 13.3 新包与职责

| 包 | 动作 | 说明 |
|----|------|------|
| `event/` | 新增 | 用户协议层：`Input`、`Output`、`PromptText` / `TextDelta`、多模态 `InputPart` / `OutputPart` |
| `model/` | 新增 | 模型协议层：Message、Part、Provider、Request、Response、Counter |
| `tools/` | 重构 | 工具接口、Resolver、Result DTO；不依赖 `event/`、`model/`、`hook/` 或 `policy/` |
| `policy/` | 新增，替代核心 `permission/` | 权限、安全、预算、速率限制和组织规则统一决策 |
| `compact/` | 新增 | Context 压缩管线 |
| `hook/` | 新增 | 生命周期事件与拦截点 |
| `flow/` | 保留并精简 | 保留 `Sequential/Parallel/Loop`，并提供 `AsTool` 适配 helper |
| `graph/` | 保留为可选系统 | DAG/checkpoint/condition 工作流，不强行并入 Agent Loop |

不新增 `retry/` 包。Provider 调用重试是 Agent Loop 的模型调用策略，类型放在 `model/` 或根包 Option 中，由 `blades.WithRetryPolicy(...)` 注入。

不新增 `settings/`、`app/`、`host/`、`channel/` 或 `workspace/` 核心包。配置文件、环境变量、默认值合并、transport 接入、run lifecycle、workspace 边界都属于应用装配职责，放在具体应用的 `internal/` 包中，例如 `cmd/<app>/internal/app`、`cmd/<app>/internal/channel` 和 `cmd/<app>/internal/workspace`。

## 13.4 flow/ 迁移

`flow/` 不删除，但只保留通用组合原语：

- `flow.Sequential`：串联多个 Agent 的 Event channel。
- `flow.Parallel`：fan-out 输入，fan-in 输出。
- `flow.Loop`：按 `event.TurnEnd` / 策略条件进行重复执行。

以下类型不进入 AgentOS 核心：

- `RoutingAgent`：路由是应用策略，可由应用、recipe、policy、`flow.AsTool` 或 orchestrator 实现。
- `DeepAgent`：coding-specific 复杂任务编排，移到 `examples/coding` 或后续 `contrib/orchestrator`。

## 13.5 agents/team 迁移决策

不新增核心 `agents/` 包。`Explore/Plan/General/Verify` 是 Coding Agent 预设，不适合作为通用 AgentOS 核心。

推荐去向：

- `examples/coding/`：完整展示 coding app 如何装配 Explore/Plan/Verify。
- `contrib/preset/`：可选通用预设，如 `preset.Assistant`、`preset.Researcher`。
- 用户业务包：例如 `support.Agent()`、`ops.Agent()`、`coding.Explore()`。

不新增核心 `team/` 包。Coordinator/Swarm/Team 属于应用级多 Agent 协议，后续如需要可放入 `orchestrator/` 或 `contrib/orchestrator`，构建在 `flow/`、`event/`、`session/` 和应用层 run manager 之上。Agent-as-Tool 适配由 `flow.AsTool` 提供，不单独新增 `agenttool/` 包。

## 13.6 contrib 迁移

Provider 集成统一实现 `model.Provider`：

- `contrib/anthropic`：内部保留 Anthropic message/tool/cache_control 转换。
- `contrib/openai`：内部处理 OpenAI content part、tool call 和 response 格式。
- `contrib/gemini`：内部处理 FunctionCall/FunctionResponse。
- `contrib/mcp`：MCP schema 映射到 `tools.Tool`，transport 逻辑保留在 contrib。
- `contrib/otel`：优先基于 `hook.Registry` 集成；应用层可自行包装 run manager 或 channel tracing。

Provider 包只依赖 `model/`，不依赖 `event/`。Event 到 Message 的转换只发生在 Agent Loop。

## 13.7 skills/recipe/graph 迁移

- `skills/`：适配新的 `tools.Tool` 和 `tools.ResultPart`，不直接构造 `model.Message`。
- `recipe/`：保持声明式 Agent 构建能力，可声明 agent、model、tools、policy 等核心依赖；channel、workspace 和配置装配留在应用层。
- `graph/`：继续作为独立 DAG 系统；如果要桥接 Agent，桥接代码放 `flow/graph` 或 `contrib/graphagent`，不让 `graph/` 依赖 `blades/`。

## 13.8 推荐迁移顺序

1. 定义 `event/`、`model/`、`tools.ResultPart` 和 Session typed context helper。
2. 改造 `Agent.Run` 与 Agent Loop，完成 Event/Message 转换。
3. 迁移 Provider 到 `model.Provider`。
4. 迁移 Session 和 Compact。
5. 迁移 Tools、Policy、Hook。
6. 精简 `flow/`，移除 Routing/Deep 核心路径。
7. 用 `cmd/<app>/internal/*` 和 examples 展示应用层如何接入 channel、workspace、配置、run lifecycle 和主动通知。
8. 迁移 recipe、skills、examples、contrib。

## 13.9 验收标准

- `event/`、`model/`、`tools/` 三个叶子包互不依赖。
- 根包不暴露 `model.Message`。
- `Agent.Run` 只使用 `event.Input` / `event.Output`。
- Event channel 没有二次 wrapper。
- 核心 context 采用 capability-specific helper；session identity 来自 `session.Session`，应用层 ID 留在应用层映射或 payload。
- Coding presets 不在核心依赖路径上。
- `go list` 依赖图无循环，且 contrib provider 不依赖 root `blades`。
