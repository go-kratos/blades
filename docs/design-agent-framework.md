---
type: design
title: Blades Agent Framework 设计蓝图
date: 2026-05-01
status: draft
author: chenzhihui
related: [reference-claude-code-agent.md, reference-pi-agent-framework.md]
tags: [agent, framework, architecture, context-management, memory, tools, permissions, hooks, session, streaming]
sub-docs:
  - design-event-agent-loop.md
  - design-message-context.md
  - design-tool-system.md
  - design-hook-extension.md
  - design-session.md
  - design-permission-mode.md
  - design-subagent.md
  - design-memory.md
  - design-infra.md
  - design-migration.md
---

# Blades Agent Framework 设计蓝图

## 背景与动机

### 现状

Blades 是一个基于 Go 构建的 Agent 框架，当前已具备：

- `Agent` 接口 + `Generator[T,E]`（`iter.Seq2`）流式原语
- `Invocation` 调用上下文 + `Session` 会话管理
- `Middleware` 洋葱模型（agent/tool/graph 三层）
- `flow/` 组合模式（Sequential、Parallel、Loop、Routing、Deep）
- `graph/` DAG 执行器（编译时验证）
- `tools/` 工具系统（`NewFunc[I,O]` 泛型构造 + `Resolver` 动态发现）
- `skills/` 技能系统（SKILL.md + frontmatter）
- `memory/` 内存存储（InMemoryStore + 子串搜索）
- `recipe/` 声明式 YAML 构建
- `contrib/` 多 Provider（Anthropic、OpenAI、Gemini、MCP、OTel）

### 问题

通过对 Claude Code Agent 和 pi-agent 两个成熟框架的深入分析，发现当前 Blades 在以下方面存在差距：

1. **Agent Loop 过于简单** — 当前 `handle()` 是平坦的迭代循环，缺少 steering/follow-up 注入、多策略压缩、事件发射
2. **无上下文压缩管线** — 仅有单一 `ContextCompressor` 接口，缺少 Claude Code 的多策略分层压缩
3. **工具执行无流式重叠** — 必须等模型完成才执行工具，无法在流式输出时提前启动并发安全工具
4. **无 Hook/事件系统** — 仅有 Middleware 洋葱模型，缺少生命周期事件订阅
5. **无权限系统** — 仅有 `Confirm` 中间件，缺少分层权限决策链
6. **会话无持久化** — 仅有内存实现，无 JSONL 持久化、无分支、无树形结构
7. **Memory 系统原始** — 仅有简单的内存存储和子串搜索，缺少层级 Memory、自动提取
8. **消息类型扩展机制待定** — `Part` 是密封接口，后续可考虑开放注册（本次设计暂不处理）

### 目标

- 设计一个融合 Claude Code 和 pi-agent 最佳实践的全新 Agent 框架
- 保持 Go 惯用风格（接口、iter.Seq2、context.Context、Option 函数）
- 不考虑向后兼容，以最新框架理念为主
- 流式优先、缓存感知、可扩展、可组合

---

## 方案设计

### 整体架构

```
┌─────────────────────────────────────────────────────────────────┐
│  应用层                                                          │
│    ├── CLI / REPL          终端交互入口                          │
│    ├── SDK                 程序化调用入口                        │
│    └── Recipe              声明式 YAML 构建                      │
├─────────────────────────────────────────────────────────────────┤
│  blades/（根包：用户 API）                                       │
│    ├── Agent 接口           Run(ctx, <-chan InputEvent) ...      │
│    ├── InputEvent          用户输入事件（Prompt/Steer/Control）  │
│    └── OutputEvent         Agent 输出事件（Text/Tool/Turn/Done） │
├─────────────────────────────────────────────────────────────────┤
│  Agent Loop（状态机，根包内部实现）                               │
│    ├── 状态转换            Idle → Preparing → Streaming → Acting │
│    ├── TurnState           不可变每轮状态                        │
│    └── Steer Queue         中途指令队列（FIFO）                  │
├─────────────────────────────────────────────────────────────────┤
│  Internal Service Layer（Agent Loop 私有实现）                    │
│    ├── ContextBuilder      Session → 压缩 → 过滤 → model.Request│
│    └── streamAndRecord     Provider Stream → Event + Session     │
├─────────────────────────────────────────────────────────────────┤
│  Capability Service Layer（用户可配置能力层）                     │
│    ├── Compression         7 策略分层压缩管线                    │
│    ├── Tool Orchestrator   流式执行 + 并发分区                   │
│    ├── Permission Chain    分层权限决策                           │
│    ├── Hook Registry       生命周期事件订阅                      │
│    ├── Retry Policy        API 错误处理与重试                    │
│    ├── Sub-Agent Manager   Fork/Background/Worktree              │
│    └── Role Registry       内置角色 + 用户自定义角色              │
├─────────────────────────────────────────────────────────────────┤
│  基础设施层                                                      │
│    ├── model.*             Message + Provider + Request/Response + Counter │
│    ├── session.*           Session 接口 + Store（Open 模式）+ 消息树 │
│    ├── memory.Store        5 层 Memory 层级 + 自动提取           │
│    ├── prompt.Builder      缓存感知构建（静态前缀 + 动态后缀）   │
│    └── settings.Loader     多级优先级配置合并 + 热重载           │
├─────────────────────────────────────────────────────────────────┤
│  Provider 实现层（contrib/）                                     │
│    ├── contrib/anthropic   实现 model.Provider + 内部格式转换    │
│    ├── contrib/openai      实现 model.Provider + 内部格式转换    │
│    ├── contrib/gemini      实现 model.Provider + 内部格式转换    │
│    ├── contrib/mcp         MCP 协议工具桥接                      │
│    └── contrib/otel        OpenTelemetry 可观测性                │
└─────────────────────────────────────────────────────────────────┘
```

### 设计原则

| 原则 | 说明 | 参考来源 |
|------|------|---------|
| 极简根包 | 根包只放 Agent 接口 + Event 类型，Message/Provider 下沉到 model/ 包 | 原创设计 |
| Event 类型安全边界 | 用户侧只接触 InputEvent/OutputEvent，Message 是 model/ 包的内部类型 | 原创设计 |
| 双向实时流 | `<-chan InputEvent` 进、`<-chan OutputEvent` 出，Agent 运行中可注入指令 | 原创设计 |
| 显式状态机 | Agent Loop 通过 AgentState + 转换规则驱动，可声明可测试 | Claude Code query loop + pi-agent 双循环 |
| 不可变轮次 | 每轮创建新 TurnState，不原地修改消息数组 | Claude Code State 对象 |
| 缓存感知 | System Prompt 分静态/动态两段，工具按名称排序保证缓存稳定 | Claude Code 静态/动态分界 |
| 极简核心 | 核心只做 Loop + Event + Tool 执行，权限/MCP/Memory 全部可插拔 | pi-agent 极简核心哲学 |
| 双层 Service Layer | Internal Service Layer（私有实现）与 Capability Service Layer（用户可配置）分离 | 原创设计 |
| model/ 类型与计数包 | model/ 放类型定义、接口和 Token 计数（Counter 接口 + CharCounter/ProviderCounter/CachedCounter），适配/转换逻辑在使用侧 | 原创设计 + Go io 包先例 |
| 消息边界 | 应用层 Event 与 LLM 层 model.Message 通过 ContextBuilder 内部转换，不暴露独立接口 | pi-agent convertToLlm |
| 渐进式扩展 | 从 Prompt 模板到 Skill 到 Extension 到 Package，复杂度渐进 | pi-agent 四层扩展 |
| Role 工厂模式 | Role 是 ForkConfig 的上层抽象（工厂），不是 Agent 本身。ForkAgent 始终是创建子 Agent 的唯一机制 | Claude Code built-in agents |
| 组合模式统一 | Sequential/Parallel/Loop 组合模式与 Role 统一在 agent/ 包，RoutingAgent 被 LLM 自主路由替代 | Claude Code AgentTool 路由 |
| 三层 Multi-Agent | L1 SubAgent → L2 Coordinator → L3 Swarm/Team，每层基于前一层构建，用户按需选择复杂度 | Claude Code 三层 multi-agent 体系 |
| Fail-Closed 默认值 | 工具默认非并发安全、非只读；权限默认 ask；熔断器默认启用 | Claude Code fail-closed defaults |
| Prompt Cache 纪律 | 静态/动态分界、section 级缓存标记、fork agent 复用父级 prompt 字节 | Claude Code prompt cache discipline |
| 熔断器模式 | AutoCompact 连续 3 次失败后禁用；DenialTracker 降级到 default 模式 | Claude Code circuit breakers |

### 命名规范

遵循 Go 惯用的 `package.Role` 模式：包名是名词（领域），类型名是角色（动作者）。与 Go 标准库一致：`io.Reader`、`http.Handler`、`sql.Scanner`。

```
model.Message
model.Provider
model.Request
model.Response
model.TextPart
session.Store
session.Session
prompt.Builder
tools.Resolver
compact.Pipeline
graph.Checkpointer
hook.Registry
model.Counter
permission.Chain
agent.Registry
agent.Role
agent.ToolFilter
agent.Sequential
```

---

## Event 系统设计

Event 系统是整个框架的顶层架构。核心思想：**类型安全的双向 Event 通信，Agent 是纯函数**。

### 三层架构

```
┌──────────────────────────────────────────────────────────┐
│  User Layer（blades/ 根包）                               │
│    <-chan InputEvent  ──→  Agent  ──→  <-chan OutputEvent  │
│    输入 channel                       输出 channel        │
├──────────────────────────────────────────────────────────┤
│  Agent Loop（状态机，根包内部实现）                        │
│    States: Idle → Preparing → Streaming → Acting          │
│    从 input channel 读取，向 output channel 写入          │
│    编排 Service Layer 完成具体工作                        │
├──────────────────────────────────────────────────────────┤
│  Internal Service Layer（Agent Loop 私有实现）             │
│    ContextBuilder:    Session → 压缩 → 过滤 → model.Request│
│    streamAndRecord:   Provider Stream → Event + Session    │
├──────────────────────────────────────────────────────────┤
│  Capability Service Layer（用户可配置能力层）              │
│    Compression:       7 策略分层压缩管线                  │
│    ToolOrchestrator:  流式执行 + 并发分区                 │
│    PermissionChain:   分层权限决策                         │
│    HookRegistry:      生命周期事件订阅                     │
│    RetryPolicy:       API 错误处理与重试                   │
├──────────────────────────────────────────────────────────┤
│  model/（纯类型包：Message + Provider 接口 + Request/Response）
│    model.Provider.NewStreaming(ctx, *model.Request)        │
├──────────────────────────────────────────────────────────┤
│  contrib/（Provider 实现，各自处理格式转换）               │
│    Anthropic / OpenAI / Gemini 实现 model.Provider        │
└──────────────────────────────────────────────────────────┘
```

### Agent 接口

```go
type Agent interface {
    Name() string
    Description() string
    Run(context.Context, <-chan InputEvent) (<-chan OutputEvent, error)
}
```

### Event 类型

```go
type InputEvent interface{ inputEvent() }
type OutputEvent interface{ outputEvent() }
```

- **输入方向**：`PromptEvent`（发送消息）、`SteerEvent`（中途注入指令）、`ControlEvent`（Abort/Pause/Resume）
- **输出方向**：`TextEvent`、`ThinkingEvent`、`ToolStartEvent`、`ToolEndEvent`、`TurnEndEvent`、`ErrorEvent`、`DoneEvent`

> 详细的 Event 类型定义、使用示例、Middleware、Service Layer 设计、数据流、Agent Loop 状态机与双循环实现 → [design-event-agent-loop.md](design-event-agent-loop.md)

---

## 包结构设计

### 现有结构的问题

根包 `blades/` 承载了 Agent、Message、Session、Runner、Middleware、State、Invocation、Compressor 等所有核心类型。Go 不允许循环依赖，互相引用的类型被迫放在同一个包，导致根包职责过重。

新设计引入 Event 作为核心类型、去掉 Invocation，是重新组织包结构的好时机。

### 设计原则

1. **根包只放用户 API** — `Agent` 接口、`InputEvent`/`OutputEvent`、`NewAgent()`
2. **model/ 是纯类型包** — Message、Provider、Request/Response，不含适配逻辑
3. **依赖方向单一** — 上层依赖下层，不反向，无循环
4. **`package.Role` 命名** — 包名是名词，类型名是角色

### 包结构

```
blades/                         根包：用户 API（Agent + Event）
├── agent.go                    Agent 接口 + NewAgent() 构造函数
├── event.go                    InputEvent / OutputEvent + 所有 Event 类型
├── errors.go                   公共错误
│
├── model/                      LLM 模型层（类型定义 + 接口 + Token 计数）
│   ├── message.go              Message, Role, Status, 构造函数
│   ├── part.go                 Part 接口, TextPart, FilePart, DataPart, ToolPart
│   ├── provider.go             Provider 接口
│   ├── request.go              Request, Response
│   ├── token.go                TokenUsage
│   ├── counter.go              Counter 接口
│   ├── counter_char.go         CharCounter 字符估算实现（1 token ≈ 4 chars）
│   └── counter_provider.go     ProviderCounter / CachedCounter
│
├── session/                    会话持久化
│   ├── session.go              Session 接口 + NewMemory/New/Open + context 辅助
│   ├── store.go                Store 接口 + Writer 接口 + Header + Snapshot + StoreOption
│   ├── entry.go                Entry + EntryType + MessageData/CompactionData/ConfigChangeData
│   ├── option.go               Option + WithID/WithCWD/WithTitle/WithState
│   ├── tree.go                 Tree + TreeNode + Path/Branch/Leaf/Add/Rebuild
│   ├── memory.go               memorySession（纯内存，内部维护 Tree）
│   ├── persistent.go           persistentSession（Store + 内存缓存 + Writer）
│   ├── file.go                 fileStore（JSONL 读写 + flock）
│   └── state.go                GetState[T] 泛型辅助
│
├── tools/                      工具系统（不依赖 model/）
│   ├── tool.go                 tools.Tool 核心接口 + 可选能力接口
│   ├── handler.go              tools.Handler
│   ├── resolver.go             tools.Resolver
│   ├── context.go              tools.Context
│   └── exit.go                 tools.ExitTool
│
├── memory/                     Memory 系统
│   ├── store.go                memory.Store 接口
│   ├── loader.go               memory.Loader
│   ├── entry.go                memory.Entry / memory.Type
│   ├── extractor.go            memory.Extractor（自动提取）
│   ├── section.go              memory.Section（prompt 注入）
│   ├── session_memory.go       memory.SessionMemory（会话级摘要）
│   ├── agent_memory.go         memory.AgentMemory（每 agent 类型持久化）
│   ├── snapshot.go             memory.InitializeFromSnapshot（快照分发）
│   └── recaller.go             memory.Recaller（选择性召回）
│
├── prompt/                     System Prompt 构建
│   ├── builder.go              prompt.Builder
│   └── section.go              prompt.Section / prompt.Breakpoint
│
├── compact/                    上下文压缩
│   ├── pipeline.go             compact.Pipeline
│   ├── strategy.go             compact.Strategy 接口
│   ├── snip.go                 Snip 策略
│   ├── window.go               滑动窗口策略
│   ├── summary.go              LLM 摘要策略（接受 Summarizer 函数）
│   ├── budget.go               工具结果预算策略
│   ├── auto.go                 自动压缩策略
│   ├── session_memory.go       SessionMemoryCompactStrategy
│   ├── restore.go              PostCompactRestorer（压缩后状态恢复）
│   └── invariant.go            AdjustKeepBoundary（API 不变量保护）
│
├── hook/                       Hook 系统
│   ├── event.go                hook.Event 类型（Agent/Model/Tool 核心事件）
│   ├── registry.go             hook.Registry
│   └── handler.go              hook.ObserveHandler / 拦截型 Handler
│
├── permission/                 权限系统
│   ├── chain.go                permission.Chain
│   ├── rule.go                 permission.Rule
│   └── mode.go                 permission.Mode
│
├── retry/                      API 错误处理与重试
│   ├── policy.go               retry.Policy
│   └── backoff.go              retry.Backoff
│
├── skills/                     技能系统
│   ├── skill.go                skills.Skill 接口
│   ├── loader.go               skills.Loader
│   ├── toolset.go              skills.Toolset
│   └── models.go               skills.Frontmatter
│
├── agent/                      Agent 角色 + 组合模式 + Multi-Agent
│   ├── role.go                 Role + ConfigContext + RoleOptions + Source
│   ├── registry.go             Registry（Register/Resolve/List/DefaultRegistry）
│   ├── filter.go               ToolFilter + ReadOnlyTools/AllowOnly/Disallow/And/Or
│   ├── builtin.go              4 种内置角色（explore/plan/general/verify）
│   ├── spawn.go                Spawn 便捷函数
│   ├── tool.go                 AgentTool（统一入口，处理 subagent_type）
│   ├── sequential.go           Sequential（从 flow/ 迁移）
│   ├── parallel.go             Parallel（从 flow/ 迁移）
│   ├── loop.go                 Loop（从 flow/ 迁移）
│   ├── coordinator.go          Coordinator 模式（system prompt + 工作流分相）
│   ├── team.go                 Team + TeamConfig + Member + TeamStore
│   ├── mailbox.go              Mailbox 文件式消息通信
│   ├── task.go                 TaskList + Task + TaskStatus
│   └── permission_bridge.go    PermissionBridge（InProcess / Mailbox）
│
├── graph/                      DAG 执行器（可选子系统）
│   ├── graph.go                graph.Graph
│   ├── executor.go             graph.Executor
│   ├── task.go                 graph.Task
│   └── checkpoint.go           graph.Checkpointer
│
├── middleware/                  中间件
│   ├── retry.go                middleware.Retry（Agent 级重试）
│   ├── logging.go              middleware.Logging
│   └── otel.go                 middleware.OTel（可观测性集成）
│
├── recipe/                     声明式构建
│   ├── spec.go                 recipe.Spec
│   ├── builder.go              recipe.Builder
│   └── registry.go             recipe.Registry
│
├── evaluator/                  评估器
│   ├── evaluator.go            evaluator.Evaluator
│   └── criteria.go             evaluator.Criteria
│
├── contrib/                    Provider 实现（各自实现 model.Provider + 内部格式转换）
│   ├── anthropic/              contrib/anthropic.Claude
│   ├── openai/                 contrib/openai.Chat
│   ├── gemini/                 contrib/gemini.Gemini
│   ├── mcp/                    contrib/mcp.Resolver
│   └── otel/                   contrib/otel.Tracing
│
├── stream/                     通用 iter.Seq2 工具函数（Just/Error/Filter/Map/Merge）
│
├── settings/                   多级优先级配置合并
│   ├── loader.go               settings.Loader
│   ├── schema.go               settings.Schema
│   └── watch.go                settings.Watch（文件系统热重载）
│
├── internal/                   内部实现
│   └── loop/                   Agent Loop 状态机实现（state, context_builder, stream）
│
├── cmd/blades/                 CLI 入口
└── examples/                   示例
```

### 与现有结构的变化

| 变化 | 原因 |
|------|------|
| 根包精简为 Agent + Event | Message/Provider 下沉到 model/ 包，根包只保留用户 API |
| 新增 `model/` | 合并 Message + Provider 为纯类型包，是依赖图的叶子节点 |
| `context/` → `compact/` | 避免与标准库 `context` 冲突，且与文档术语（AutoCompact、CompactionData）一致 |
| Session 接口移到 `session/` | 根包不再承载 Session，session/ 包定义 Session 接口（7 方法）+ Store 接口（Open 模式）+ 双实现 |
| Token 计数合入 `model/` | Token 计数从 internal/counter 提升为 model/ 包的公开类型（Counter 接口 + 多实现），不再独立成包 |
| 新增 `retry/` | API 错误处理与重试策略独立为包 |
| 去掉根包 `model.go`、`message.go`、`session.go` | 这些类型分别移到 model/ 和 session/ 包 |
| `flow/` 去掉，新增 `agent/` | 组合模式（Sequential/Parallel/Loop）迁入 agent/ 包，与 Role 系统统一。RoutingAgent 被 Role + AgentTool 路由替代，DeepAgent 的 todo/task 能力内化为框架内置工具。Coordinator 模式和 Swarm/Team 模式也在 agent/ 包中实现 |
| `graph/` 降级为可选子系统 | DAG 执行器操作 State map，不是 Agent 接口，更适合数据管道场景，不在核心依赖路径上 |

### 依赖关系

```
model/（叶子包：Message + Provider + Request/Response，不依赖任何 blades 子包）
  ↑
  ├── session/（依赖 model/：存储 []*model.Message）
  ├── compact/（依赖 model/：压缩 []*model.Message）
  ├── tools/（独立，不依赖 model/）
  ├── hook/（独立）
  ├── permission/（独立）
  ├── prompt/（独立）
  ├── retry/（独立）
  ├── memory/（独立）
  ├── settings/（独立）
  ↑
  ├── skills/（依赖 tools/）
  ↑
  ├── blades/（根包：依赖 model/, session/, compact/, tools/, hook/, permission/, prompt/, retry/, settings/）
  │   └── Agent Loop 内部实现 ContextBuilder（含消息过滤）和 streamAndRecord
  ↑
  ├── agent/（依赖 blades/ 根包 + tools/ + permission/ + model/ + session/：Role + 组合模式 + Coordinator + Swarm）
  ├── middleware/（依赖 blades/ 根包）
  ├── recipe/（依赖 blades/, tools/, agent/, model/）
  ├── evaluator/（依赖 blades/）
  ├── graph/（可选子系统，独立于 Agent 接口）
  ↑
  ├── contrib/*（依赖 model/：实现 model.Provider，各自处理格式转换）
  ↑
  └── cmd/blades/（依赖所有）
```

依赖方向严格单向，无循环。model/ 是叶子包，不依赖任何 blades 子包。
根包依赖 model/ 是向下依赖。compact/ 通过 `Summarizer` 函数注入避免对根包的循环依赖。

### 各包的核心导出类型

| 包 | 核心类型 | 读作 |
|---|---------|------|
| `blades` | `Agent`, `InputEvent`, `OutputEvent` | `blades.Agent` |
| `model` | `Message`, `Provider`, `Request`, `Response`, `Part`, `TokenUsage`, `Counter` | `model.Provider` |
| `session` | `Session`, `Store`, `Writer`, `Entry`, `Snapshot`, `Tree`, `Header` | `session.Session` |
| `tools` | `Tool`, `ConcurrentTool`, `ReadOnlyTool`, `Handler`, `Resolver` | `tools.Tool` |
| `compact` | `Pipeline`, `Strategy`, `PostCompactRestorer` | `compact.Pipeline` |
| `hook` | `Event`, `Registry`, `ObserveHandler` | `hook.Registry` |
| `permission` | `Chain`, `Rule`, `Mode`, `ModeManager`, `SafetyChecker`, `AcceptEditsChecker`, `Classifier`, `AutoModeController` | `permission.Chain` |
| `prompt` | `Builder`, `Section`, `SystemPrompt` | `prompt.Builder` |
| `retry` | `Policy`, `Backoff` | `retry.Policy` |
| `memory` | `Store`, `Loader`, `Entry`, `Extractor`, `SessionMemory`, `AgentMemory`, `Recaller`, `SnapshotState` | `memory.Store` |
| `agent` | `Role`, `Registry`, `ToolFilter`, `Sequential`, `Parallel`, `Loop`, `CoordinatorConfig`, `Team`, `Mailbox`, `TaskList` | `agent.Registry` |
| `graph` | `Graph`, `Executor`, `Checkpointer`（可选子系统） | `graph.Executor` |
| `middleware` | `Retry`, `Logging`, `OTel` | `middleware.Retry` |
| `settings` | `Loader`, `Schema`, `Source` | `settings.Loader` |

---

## 模块详细设计

### [Event 系统与 Agent Loop](design-event-agent-loop.md)

Event 是整个框架的顶层架构，定义类型安全的双向通信协议（`InputEvent` / `OutputEvent`）。Agent Loop 是 Event 驱动的状态机实现（Idle → Preparing → Streaming → Acting），采用双循环结构（外层等待输入，内层处理 steering + tool 执行）。包含 TurnState 不可变状态、Steer 队列、InputMiddleware / OutputMiddleware、ContextBuilder 和 streamAndRecord 两个 Internal Service。

### [消息与上下文系统](design-message-context.md)

`model/` 包定义 7 种内置 Part 类型（TextPart、FilePart、DataPart、ToolUsePart、ToolResultPart、ThinkingPart、CompactionSummaryPart）。7 策略分层压缩管线（ToolResultBudget → Snip → MicroCompact → AutoCompact → ReactiveCompact → SessionMemoryCompact → PostCompactRestore）按成本从低到高排列，token 降到预算内即短路。`prompt.Builder` 将 system prompt 分为静态可缓存前缀和动态后缀，配合 Provider 的 prompt cache 机制。

### [工具系统](design-tool-system.md)

核心 `Tool` 接口保持精简（4 方法），扩展能力通过可选接口（`ConcurrentTool`、`ReadOnlyTool`、`DestructiveTool`、`PromptContributor`、`BudgetedTool`）实现。`StreamingToolExecutor` 在模型流式输出时提前启动并发安全工具。`partitionToolCalls` 自动按并发模式分组。工具执行完整生命周期：参数校验 → BeforeToolHook → 权限检查 → Handle → AfterToolHook → ToolResultBudget → 事件发射。

### [扩展与 Hook 系统](design-hook-extension.md)

类型化 `HookEvent` 判别联合（Agent/Model/Tool 核心事件），`HookRegistry` 支持观察型（`ObserveHandler[E]`）和拦截型（`PreToolUseHandler`、`PostToolUseHandler`、`BeforeModelHandler`）两类 Handler。两层渐进式扩展：Prompt 模板（`.blades/prompts/`）和 Skill（Markdown + YAML frontmatter）。

### [会话与持久化](design-session.md)

`Session` 接口 7 方法（ID/State/SetState/Append/History/Leaf/Branch），面向 Agent Loop。`Store` 接口采用 Open 模式返回 `Writer` handle。`Entry` 联合类型（message/compaction/config_change/custom）通过 ParentID 链构成消息树。JSONL 追加写入天然并发安全。双实现：`memorySession`（纯内存）和 `persistentSession`（Store + 内存缓存）。

### [权限与交互模式系统](design-permission-mode.md)

7 层权限决策链：安全检查（bypass-immune）→ 规则匹配 → 模式决策 → Hook 拦截 → 工具自声明 → 默认决策 → 后处理。`ModeManager` 状态机管理 4 种核心模式（default/accept_edits/plan/auto）。Plan Mode 完整生命周期（EnterPlanModeTool → 只读探索 → WritePlanTool → ExitPlanModeTool）。Auto Mode 使用可插拔 `Classifier` 接口 + `DenialTracker` 熔断器。`SafetyChecker` 双级别（Block/Confirm）。

### [子 Agent 系统](design-subagent.md)

`ForkAgent` 通过共享静态 system prompt 前缀命中父 Agent 的 prompt cache。`BackgroundAgent` 支持 fire-and-forget 后台执行（Memory 提取、任务摘要）。`WorktreeConfig` 支持 git worktree 隔离。`QuerySource` 标记 fork 来源（user/sub_agent/compact/extract_memory/task_summary/skill），用于权限链和 Hook 的行为区分。`agent.Role` 是 ForkConfig 的上层抽象——可复用的 Agent 角色模板，框架内置 4 种角色（explore/plan/general/verify），用户可注册自定义角色。`ToolFilter` 函数类型支持 ReadOnlyTools/AllowOnly/Disallow 等可组合过滤器。组合模式（Sequential/Parallel/Loop）从 flow/ 迁入 agent/ 包。三层 Multi-Agent 体系：L1 SubAgent（单个子 agent）→ L2 Coordinator（主线程变调度器，worker 结果以 notification 回流）→ L3 Swarm/Team（显式团队实体，共享任务列表，mailbox 通信，权限桥接）。

### [Memory 系统](design-memory.md)

5 层层级 Memory（Managed → User → Project → Local → Auto），`Loader` 发现和加载所有来源的 Memory 文件，支持 `@include` 指令（最大深度 5）和 `globs` 条件注入。`Extractor` 在每轮结束后 fire-and-forget 运行，从对话中提取持久性事实写入 `~/.blades/memories/`。

### [基础设施（重试、Token 计数、可观测性、Graph）](design-infra.md)

`retry.Policy` 感知 Provider 错误类型（Fatal/Retryable/RateLimit/Overloaded/AuthExpired），支持 529 模型过载自动降级和 401 认证刷新。`model.Counter` 接口支持 Provider 原生 / 字符估算 / 缓存包装三级降级。可观测性通过 Hook 系统集成 OpenTelemetry。`graph/` 保持为可选独立子系统。

### [迁移路径](design-migration.md)

核心接口迁移：`Agent.Run` 签名从 `Generator[*Message, error]` 改为 `(<-chan OutputEvent, error)`，`*Invocation` 去掉，`Middleware` 拆分为 `InputMiddleware` + `OutputMiddleware`。各包迁移：flow/ 中 Sequential/Parallel/Loop 迁入 agent/ 包并适配新 Event 签名，RoutingAgent 和 DeepAgent 不再保留，contrib/ 实现 `model.Provider` 内部处理格式转换，skills/ 适配新 `tools.Tool` 接口，graph/ 保持为可选独立子系统。新增 Coordinator 模式和 Swarm/Team 模式（无需迁移现有代码）。

---

## 实现计划

### 阶段 1：Event 系统 + Agent Loop（基础）

- [ ] 定义 `InputEvent` / `OutputEvent` 接口和所有 Event 类型
- [ ] 实现 `TurnState` 不可变状态管理
- [ ] 实现 Agent Loop 双循环状态机
- [ ] 实现 `InputMiddleware` / `OutputMiddleware`
- [ ] 迁移现有测试到新 Event 接口

### 阶段 2：Session 持久化（Agent Loop 依赖）

- [ ] 定义 `session.Session` 接口（7 方法：ID/State/SetState/Append/History/Leaf/Branch）
- [ ] 定义 `session.Store` 接口（Open 模式返回 Writer）+ `session.Writer` 接口
- [ ] 定义 `session.Entry` 联合类型（4 种：message/compaction/config_change/custom）
- [ ] 实现 `session.Tree`（Rebuild/Path/Branch/Leaf/Add）
- [ ] 实现 `session.memorySession`（纯内存，内部维护 Tree）
- [ ] 实现 `session.fileStore`（JSONL 读写 + flock + 崩溃恢复）
- [ ] 实现 `session.persistentSession`（Store + 内存缓存 + Writer 写穿）
- [ ] 实现会话恢复流程（Tree 重建 + Compaction 回放 + State 回放）
- [ ] 实现 `GetState[T]` 泛型辅助

### 阶段 3：消息与上下文

- [ ] 实现 `model/` 包（Message, Part 内置类型, Provider, Request, Response, TokenUsage）
- [ ] 实现 ContextBuilder（含 `filterForProvider` 消息过滤）
- [ ] 实现 `streamAndRecord` 私有方法
- [ ] 实现 `model.Counter` 接口和多实现（CharCounter、ProviderCounter、CachedCounter）
- [ ] 实现 `compact.Pipeline` 和 7 种内置策略（Summarizer 函数注入）
- [ ] 实现 `prompt.Builder`（静态/动态分段）
- [ ] 集成 Anthropic Provider 的 cache_control

### 阶段 4：工具系统增强

- [ ] 精简 `Tool` 核心接口 + 可选能力接口（ConcurrentTool、ReadOnlyTool 等）
- [ ] 实现 `partitionToolCalls` 自动分区
- [ ] 实现 `StreamingToolExecutor`
- [ ] 实现 `ToolResultBudget`

### 阶段 5：扩展与 Hook

- [ ] 定义 `HookEvent` 判别联合类型（Agent/Model/Tool 核心事件）
- [ ] 实现 `HookRegistry`（观察型 + 拦截型 Handler）
- [ ] 增强 Skill frontmatter（hooks、mcpServers、model）
- [ ] 实现 Stop Hooks（StopHookHandler + StopHookResult）
- [ ] 实现会话作用域 Hooks（WithHookSession + ClearSessionHooks）
- [ ] 扩展事件类型到 20+（Session/Compact/Permission/Config/Filesystem/Task/Notification）

### 阶段 6：权限与交互模式系统

- [ ] 定义权限类型（Decision、Mode、Rule）
- [ ] 实现 `ModeManager`（ModeState + 转换规则表 + OnChange）
- [ ] 实现 `SafetyChecker` 接口 + `DefaultSafetyChecker`（路径遍历 + 敏感文件）
- [ ] 实现 `AcceptEditsChecker`（工作目录边界 + 路径遍历防护）
- [ ] 实现 `PermissionChain` 7 层决策链
- [ ] 实现 `PermissionMiddleware` 集成
- [ ] 定义 `Classifier` 接口 + `ClassifyRequest/ClassifyResult`
- [ ] 实现 `AutoModeController`（快速路径 + 分类器调用）
- [ ] 实现 `DenialTracker` 熔断器
- [ ] 实现 `EnterPlanModeTool` + `ExitPlanModeTool` + `WritePlanTool`
- [ ] 实现 `PlanModePromptSection`（system prompt 动态注入）
- [ ] 实现 `FilterToolsForMode`（plan 模式工具过滤）
- [ ] 集成 ModeManager 到 Agent Loop（ContextBuilder + prompt.Builder）

### 阶段 7：子 Agent 系统 + Agent 角色

- [ ] 实现 `ForkAgent`（共享缓存前缀）
- [ ] 实现 `BackgroundAgent`（fire-and-forget + Drain）
- [ ] 实现 `CreateWorktreeAgent`（git worktree 隔离）
- [ ] 实现 `QuerySource` 行为区分
- [ ] 定义 `Role` + `ConfigContext` + `RoleOptions`（agent/role.go）
- [ ] 实现 `ToolFilter` + 预定义过滤器 ReadOnlyTools/AllowOnly/Disallow（agent/filter.go）
- [ ] 实现 `Registry`（Register/Resolve/List/DefaultRegistry）（agent/registry.go）
- [ ] 实现 4 种内置角色 explore/plan/general/verify（agent/builtin.go）
- [ ] 实现 `Spawn` 便捷函数（agent/spawn.go）
- [ ] 实现 `AgentTool`（agent/tool.go，统一入口）
- [ ] 迁移 Sequential/Parallel/Loop 到 agent/ 包
- [ ] 集成 AgentTool 的 `subagent_type` 参数
- [ ] 去掉 flow/ 包（RoutingAgent、DeepAgent 不再保留）
- [ ] graph/ 降级为可选子系统

### 阶段 7b：Coordinator + Swarm/Team

- [ ] 实现 `CoordinatorConfig` + `NewCoordinator`（agent/coordinator.go）
- [ ] 实现 `CoordinatorSystemPrompt` 生成（角色定义 + worker 列表 + 工作流分相 + 约束规则）
- [ ] 实现 `TaskNotificationEvent` 回流机制
- [ ] 实现 `Team` + `TeamConfig` + `TeamStore`（agent/team.go）
- [ ] 实现 `NewFileTeamStore`（JSON 文件持久化）
- [ ] 实现 `Mailbox`（agent/mailbox.go，文件锁 + 并发安全）
- [ ] 实现 `TaskList`（agent/task.go，共享任务平面）
- [ ] 实现 `PermissionBridge`（agent/permission_bridge.go，InProcess + Mailbox 双轨）
- [ ] 实现 `TeamCreateTool` + `SendMessageTool` + `TaskCreateTool` + `TaskUpdateTool` + `TaskListTool` + `TaskStopTool`
- [ ] 实现 Teammate 拓扑约束（禁止嵌套 spawn、强制注入协作工具）

### 阶段 8：Memory 系统

- [ ] 实现 `memory.Loader`（5 层发现 + @include 解析）
- [ ] 实现 Memory 文件处理管线
- [ ] 实现 `memory.Extractor`（后台 Fork Agent）
- [ ] 实现 `memory.Section`（条件注入 System Prompt）
- [ ] 迁移现有 `memory/` 包到新架构
- [ ] 实现 `memory.SessionMemory`（会话级摘要 + 阈值更新）
- [ ] 实现 `memory.AgentMemory`（三作用域持久化）
- [ ] 实现 `memory.InitializeFromSnapshot`（快照分发）
- [ ] 实现 `memory.Recaller`（轻量模型选择性召回 top-5）
- [ ] 实现 `compact.SessionMemoryCompactStrategy`（跳过 LLM 调用）
- [ ] 实现 `compact.PostCompactRestorer`（压缩后状态恢复）
- [ ] 实现 `compact.AdjustKeepBoundary`（API 不变量保护）

### 阶段 3.5：Settings System

- [ ] 定义 `settings.Schema`（Permissions、Hooks、MCPServers、Language 等）
- [ ] 实现 `settings.Loader`（5 级优先级合并：policy > flag > project > local > user）
- [ ] 实现文件系统 Watch 热重载
- [ ] 集成到 permission.Chain（规则注入）和 hook.Registry（hook 配置注入）

### 阶段 9：错误处理与可观测性

- [ ] 实现 `retry.Policy` 和 `ErrorClassifier`
- [ ] 实现 OTel Hook 集成
- [ ] 迁移现有 `contrib/otel` 到 Hook 系统

### 阶段 10：迁移与集成

- [ ] 迁移 `agent/` 3 种组合 Agent 到新 Event 接口
- [ ] 迁移 `contrib/` Provider 实现（实现 model.Provider，内部处理格式转换）
- [ ] 迁移 `skills/` 到新 Tool 接口
- [ ] Coordinator + Swarm/Team 为新增模块，无需迁移

---

## 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| InputEvent/OutputEvent 接口变更影响所有消费者 | 高 | 根包精简为 Agent + Event 类型，变更面可控 |
| 7 策略压缩管线复杂度 | 中 | 每个策略独立实现和测试，管线按需组合 |
| StreamingToolExecutor 并发安全 | 高 | 充分的并发测试 + race detector |
| JSONL 文件膨胀（append-only） | 中 | 定期 GC 清理废弃分支（后续工作） |
| 自动 Memory 提取质量 | 中 | 节流 + 互斥 + 人工审核机制 |
| Hook 系统交互路径多 | 中 | 类型化事件 + 编译时检查减少运行时错误 |
| Output channel 背压 | 中 | buffer 大小可配置（默认 16），context 取消时 goroutine 清理 |
| 现有代码迁移工作量 | 高 | 阶段 10 专门处理迁移，agent/contrib/skills 逐包迁移 |
| Auto Mode 分类器质量不可控 | 高 | 纯接口设计，框架不承担分类器质量；DenialTracker 熔断器降级到 default |
| Plan Mode 工具过滤遗漏 | 中 | ReadOnlyTool 默认 false（安全默认值），权限链第 3 层双重保障 |
| 模式转换竞态 | 中 | ModeManager 内部 sync.RWMutex 保护 |
| AcceptEdits 路径遍历 | 高 | filepath.Rel 检查 + 符号链接解析 |
| agent/ 包名与根包 Agent 接口混淆 | 中 | agent 包导出 Role/Registry/组合 Agent，根包导出 Agent 接口，职责清晰 |
| 去掉 RoutingAgent 后路由能力下降 | 低 | AgentTool + subagent_type 提供更灵活的路由，由 LLM 自行决策 |
| 去掉 DeepAgent 后复杂任务编排能力下降 | 中 | TaskCreate/TaskUpdate 内置工具 + agent 组合模式覆盖相同场景 |
| Coordinator 模式 worker 失控 | 中 | MaxWorkers 限制 + worker 不能 spawn worker 约束 + TaskStop 统一 kill switch |
| Swarm mailbox 文件锁竞争 | 中 | 锁超时 + 重试机制，单 inbox 文件写入频率低 |
| Swarm 权限桥接延迟 | 中 | InProcessBridge 零延迟（同进程），MailboxBridge 有轮询延迟但可配置间隔 |
| Teammate 拓扑失控 | 高 | 硬约束：teammate 不能 spawn teammate，in-process teammate 不能启动 background agent |
| Session Memory 陈旧 | 中 | 阈值更新策略（minTokens=10K, toolCalls=3）+ 自然断点检测 |
| 相关 Memory 召回质量 | 中 | 可配置模型 + 回退到全量注入 + maxRecall 可调 |
| Settings 热重载竞态 | 中 | 原子快照交换（atomic snapshot swap）+ sync.RWMutex |
| AutoCompact 熔断器误触发 | 低 | 仅计连续失败，成功即重置；熔断仅影响当前会话 |

---

## Streaming 背压与生命周期

### Channel Buffer 策略

`output := make(chan OutputEvent, 16)` 的 buffer 大小通过 `AgentOption` 可配置：

```go
func WithOutputBuffer(size int) AgentOption
```

默认 16 足以覆盖大多数场景（一次 streaming 的 text delta 通常不会积压超过 16 个）。
如果消费者处理慢导致 channel 满，Agent Loop 的 `output <-` 会阻塞，自然形成背压。

### Context 取消清理

Agent Loop 的 goroutine 必须在 context 取消时正确清理：

```go
func (a *agent) loop(ctx context.Context, input <-chan InputEvent, output chan<- OutputEvent) {
    defer close(output)
    // ... 所有 output <- 操作都需要检查 ctx.Done()：
    select {
    case output <- event:
    case <-ctx.Done():
        return
    }
}
```

### Pause/Resume 实现

`ControlEvent{Action: ActionPause/Resume}` 通过 Agent Loop 内部的 `paused` 标志实现：
- Pause：Agent Loop 停止从 Provider stream 读取，但不断开连接
- Resume：恢复读取
- 如果 Provider stream 有自己的超时，Pause 时间过长可能导致连接断开，此时自动重试

---

## 参考资料

- [Claude Code Agent 参考设计](reference-claude-code-agent.md) — 核心 Agent Loop、多策略压缩、权限系统、Hook 系统、Memory 系统
- [pi-agent Framework 参考设计](reference-pi-agent-framework.md) — 极简核心哲学、双循环模型、扩展系统、convertToLlm 边界
- [Blades 现有代码](https://github.com/go-kratos/blades) — 当前 Agent/Tool/Session/Flow/Graph 实现
- [Go iter.Seq2 规范](https://pkg.go.dev/iter) — Generator 流式原语
- [Anthropic Prompt Caching](https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching) — 缓存感知 System Prompt 设计依据
