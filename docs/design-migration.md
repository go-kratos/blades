---
type: design
title: 迁移路径
parent: design-agent-framework.md
date: 2026-05-01
status: draft
modules: [module-13]
---

# 迁移路径

### 从现有代码迁移

虽然设计目标是"不考虑向后兼容"，但现有代码量不小（flow/、graph/、contrib/、skills/），需要明确迁移路径。

### 13.1 核心接口迁移

| 现有 | 新 | 迁移方式 |
|------|---|---------|
| `Agent.Run(ctx, *Invocation) Generator[*Message, error]` | `Agent.Run(ctx, <-chan InputEvent) (<-chan OutputEvent, error)` | 重写签名，内部逻辑迁移到 Agent Loop 状态机 |
| `*Invocation` | 去掉 | Session 通过 context 传递，配置在 NewAgent 时确定 |
| `Generator[*Message, error]` | `<-chan OutputEvent` | 消费端从 `for m, err := range gen` 改为 `for event := range output` |
| `Middleware func(Handler) Handler` | `InputMiddleware` / `OutputMiddleware` | 按方向拆分，重写签名 |

### 13.2 各包迁移

**flow/ 包**：3 种组合模式迁入 `blades/` 根包，2 种废弃。
- `Sequential`（原 `SequentialAgent`）：迁入 `blades/sequential.go`，内部 channel 串联
- `Parallel`（原 `ParallelAgent`）：迁入 `blades/parallel.go`，fan-out/fan-in OutputEvent channel
- `Loop`（原 `LoopAgent`）：迁入 `blades/loop.go`，内循环消费 OutputEvent，检查 TurnEndEvent 而非 `ActionLoopExit`
- `RoutingAgent`：**废弃**，功能由 `team.Coordinator` 模式替代（system prompt 驱动的任务分发）
- `DeepAgent`：**废弃**，功能由 Coordinator + TaskList 替代

迁移后 `flow/` 包整体废弃，不再保留。

**blades/ 根包**（新增文件）：
- `spawn.go`：`Spawn()` 子 Agent 创建（共享 cache 前缀）
- `agent_tool.go`：`Tool(agent)` Agent → Tool 适配器
- `sequential.go` / `parallel.go` / `loop.go`：从 flow/ 迁入的组合原语

**agents/ 包**（新增，预设 Agent）：
- `explore.go`：`Explore()` 快速只读代码搜索 Agent
- `plan.go`：`Plan()` 架构设计与实现规划 Agent
- `general.go`：`General()` 全能力通用 Agent
- `verify.go`：`Verify()` 对抗性验证 Agent

**team/ 包**（新增，多 Agent 协调）：
- `coordinator.go`：Coordinator 模式（新增，无需迁移）
- `team.go` / `mailbox.go` / `task.go`：Swarm/Team 模式（新增，无需迁移）
- `bridge.go`：PermissionBridge 权限桥接（新增，无需迁移）

**tools/ 包**：
- `filter.go`：`ToolFilter` + 组合函数（`ReadOnlyTools`/`AllowOnly`/`Disallow`/`And`/`Or`），从原有 agent/ 规划迁入

**contrib/ 包**：实现 `model.Provider` 接口，各自内部处理格式转换。
- `contrib/anthropic`：将现有 `applyEphemeralCache` 和 tool message 拆分逻辑保留在包内部
- `contrib/openai`：将 function_call 格式转换保留在包内部
- `contrib/gemini`：实现 `model.Provider`，将 Gemini 特有的 FunctionCall/FunctionResponse 格式转换保留在包内部
- `contrib/mcp`：MCP 工具桥接迁移，将 MCP tool schema 映射到新的 `tools.Tool` 接口，保留 SSE/stdio transport 逻辑
- `contrib/otel`：从 Middleware 迁移到 Hook 系统集成

**skills/ 包**：接口基本不变，`Toolset.ComposeTools` 需要适配新的 `tools.Tool` 接口（精简版）。

**graph/ 包**：保持独立，作为可选子系统。不再通过 flow/ 包桥接，用户可直接使用 graph 包构建 DAG 工作流，或通过 `blades.Sequential`/`blades.Parallel` 组合实现等效逻辑。

### 13.3 根包精简迁移

| 现有文件 | 去向 | 说明 |
|---------|------|------|
| `message.go` | `model/message.go` + `model/part.go` | Message/Part 属于模型交互层 |
| `model.go` | `model/provider.go` + `model/request.go` | Provider 接口和请求类型 |
| `session.go` | `session/session.go` | Session 管理独立包 |
| `state.go` | `session/state.go` | `session.State`（`map[string]any`）只通过 `Session.State()` 使用 |
| `compressor.go` | 删除 | 被 `compact/` + `model/counter.go` 替代 |
| `mime.go` | `model/mime.go` | MIME 类型属于消息内容层 |
| `core.go` (Invocation) | 删除 Invocation | 被 Event 系统替代，Generator 保留 |
| `agent.go` | **保留** | Agent 接口 + NewAgent() 是用户 API |
| `runner.go` | **保留** | Runner 是便捷包装 |
| `middleware.go` | **保留**（重构） | 拆分为 InputMiddleware/OutputMiddleware |
| `context.go` | **保留** | context 辅助函数 |
| `errors.go` | **保留** | 公共错误 |
| `event.go` | **新增** | InputEvent/OutputEvent 接口 + 所有 Event 类型 |

### 13.4 Coordinator / Swarm 模式

Coordinator 和 Swarm/Team 均为新增模块，无需迁移现有代码。

**Coordinator 模式**：基于 `team.NewCoordinator()` 创建，内部复用 `blades.Tool(agent)` + `blades.Spawn()` 基础设施。现有使用 `RoutingAgent` 做任务分发的代码，可迁移到 Coordinator 模式——将路由逻辑从代码硬编码改为 system prompt 驱动。

**Swarm/Team 模式**：基于 `TeamCreateTool` + `Mailbox` + `TaskList` 构建。现有无对应功能，属于全新能力。实现依赖 `session/` 包提供的持久化基础设施。

### 13.5 新增子系统（无需迁移）

以下子系统均为新增，无需迁移现有代码：

| 子系统 | 包/类型 | 说明 |
|--------|---------|------|
| Session Memory | `memory.SessionMemory` | 会话级摘要，用于 compact 捷径。阈值触发更新，跳过 LLM 调用直接复用摘要 |
| Agent Memory | `memory.AgentMemory` + `memory.InitializeFromSnapshot` | 每种 agent 类型的持久化 Memory（user/project/local 三作用域）+ 快照分发 |
| Relevant Memory Recall | `memory.Recaller` | 轻量模型查询选择 top-5 相关 Memory，替代全量注入 |
| Session Memory Compact | `compact.SessionMemoryCompactStrategy` | 压缩管线第 4 策略，跳过 LLM 调用直接使用 session memory 作为摘要 |
| API 不变量保护 | `compact.AdjustKeepBoundary` | 压缩切割时保护 tool_use/tool_result 配对完整性 |
| 压缩后状态恢复 | `compact.PostCompactRestorer` | 全量压缩后恢复最近文件、plan/skill 状态、延迟工具声明 |
| Settings System | `settings.Loader` + `settings.Schema` + `settings.Watch` | 5 级优先级配置合并（policy > flag > project > local > user）+ 文件系统热重载 |
| Stop Hooks | `hook.StopHookHandler` + `hook.StopHookResult` | Agent Loop 终止时的统一后处理，支持 ContinueLoop 注入 follow-up |
| 会话作用域 Hooks | `hook.WithHookSession` + `hook.ClearSessionHooks` | 随 agent 生命周期自动清理的 hooks |
| Bubble 权限模式 | `permission.ModeBubble` + `permission.BubbleEscalator` | 子 agent 权限决策委托给父 agent |
| DontAsk/Bypass 模式 | `permission.ModeDontAsk` / `permission.ModeBypassPermissions` | Headless/CI 场景：ASK→DENY 或 ASK→ALLOW，SafetyChecker 仍生效 |
| Implicit Fork | `agent.ForkAgent`（`_fork` 内置 Role） | 省略 subagent_type 时继承父级完整上下文，使用 ModeBubble |
| Scratchpad | `agent.ScratchpadConfig` + `agent.ScratchpadTool` | Coordinator 跨 worker 知识共享 |
| Effort Level | `agent.EffortLevel` | ForkConfig 中的 low/medium/high 级别，控制子 agent 成本/质量权衡 |
| Lite Reader | `session.LiteReader` | 64KB head+tail 窗口快速读取会话元数据 |
| Batch Writer | `session.BatchWriter` | 累积 entries 批量刷盘，减少 I/O 开销 |
| Content Replacement | `session.ContentReplacementData` | 工具结果存根替换，大结果持久化后用存根替代原始内容 |

### 13.6 agent/ 包新增文件

| 文件 | 来源 | 说明 |
|------|------|------|
| `coordinator.go` | 新增 | Coordinator 模式（system prompt 驱动任务分发） |
| `team.go` | 新增 | Swarm/Team 模式 |
| `mailbox.go` | 新增 | Agent 间消息传递 |
| `task.go` | 新增 | 共享任务列表 |
| `permission_bridge.go` | 新增 | 权限桥接（Bubble 模式实现） |
| `spawn.go` | 新增 | Spawn 便捷函数 |
| `tool.go` | 新增 | AgentTool（统一子 Agent 入口，6 级路由） |

### 13.7 迁移顺序建议

新增子系统与现有代码迁移可并行推进。建议顺序：

1. **先迁移核心接口**（13.1）— Agent.Run 签名、Event 系统、Middleware 拆分
2. **再迁移各包**（13.2-13.3）— flow/ → agent/、contrib/ 适配、根包精简
3. **最后集成新增子系统**（13.5）— Session Memory、Settings、Stop Hooks 等

新增子系统之间的依赖关系：
- `compact.SessionMemoryCompactStrategy` 依赖 `memory.SessionMemory`（通过 `SessionMemoryProvider` 接口解耦）
- Stop Hooks 依赖 `hook.HookRegistry`（阶段 5）
- Bubble 权限模式依赖 `permission.Chain`（阶段 6）
- Agent Memory 快照依赖 `memory.Loader`（阶段 8）
- Relevant Memory Recall 依赖 `memory.Loader` + `model.Provider`（阶段 8）
- Settings 热重载依赖 `hook.HookRegistry`（通过 `HookConfigChange` 事件通知）
