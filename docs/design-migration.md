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

**flow/ 包**：3 种组合模式迁入 `agent/` 包，2 种废弃。
- `Sequential`（原 `SequentialAgent`）：迁入 `agent/sequential.go`，内部 channel 串联
- `Parallel`（原 `ParallelAgent`）：迁入 `agent/parallel.go`，fan-out/fan-in OutputEvent channel
- `Loop`（原 `LoopAgent`）：迁入 `agent/loop.go`，内循环消费 OutputEvent，检查 TurnEndEvent 而非 `ActionLoopExit`
- `RoutingAgent`：**废弃**，功能由 Coordinator 模式替代（system prompt 驱动的任务分发）
- `DeepAgent`：**废弃**，功能由 Coordinator + TaskList 替代

迁移后 `flow/` 包整体废弃，不再保留。

**agent/ 包**（新增）：
- `role.go`：Role + RoleOptions + Source（原 `AgentType` 重命名）
- `registry.go`：Registry（Register/Resolve/List/Default）
- `filter.go`：ToolFilter + 组合函数
- `builtin.go`：4 种内置角色（Explore/Plan/General/Verify）
- `spawn.go`：Spawn 便捷函数
- `tool.go`：AgentTool（统一子 Agent 入口）
- `sequential.go`/`parallel.go`/`loop.go`：从 flow/ 迁入的组合模式
- `coordinator.go`：Coordinator 模式（新增，无需迁移）
- `team.go`/`mailbox.go`/`task.go`：Swarm/Team 模式（新增，无需迁移）
- `permission_bridge.go`：权限桥接（新增，无需迁移）

**contrib/ 包**：实现 `model.Provider` 接口，各自内部处理格式转换。
- `contrib/anthropic`：将现有 `applyEphemeralCache` 和 tool message 拆分逻辑保留在包内部
- `contrib/openai`：将 function_call 格式转换保留在包内部
- `contrib/gemini`：实现 `model.Provider`，将 Gemini 特有的 FunctionCall/FunctionResponse 格式转换保留在包内部
- `contrib/mcp`：MCP 工具桥接迁移，将 MCP tool schema 映射到新的 `tools.Tool` 接口，保留 SSE/stdio transport 逻辑
- `contrib/otel`：从 Middleware 迁移到 Hook 系统集成

**skills/ 包**：接口基本不变，`Toolset.ComposeTools` 需要适配新的 `tools.Tool` 接口（精简版）。

**graph/ 包**：保持独立，作为可选子系统。不再通过 `flow/graph.go` 桥接，用户可直接使用 graph 包构建 DAG 工作流，或通过 `agent.Sequential`/`agent.Parallel` 组合实现等效逻辑。

### 13.3 根包精简迁移

| 现有文件 | 去向 | 说明 |
|---------|------|------|
| `message.go` | `model/message.go` + `model/part.go` | Message/Part 属于模型交互层 |
| `model.go` | `model/provider.go` + `model/request.go` | Provider 接口和请求类型 |
| `session.go` | `session/session.go` | Session 管理独立包 |
| `state.go` | `session/state.go` | State 只通过 Session.State() 使用 |
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

**Coordinator 模式**：基于 `agent.NewCoordinator()` 创建，内部复用 AgentTool + ForkAgent 基础设施。现有使用 `RoutingAgent` 做任务分发的代码，可迁移到 Coordinator 模式——将路由逻辑从代码硬编码改为 system prompt 驱动。

**Swarm/Team 模式**：基于 `TeamCreateTool` + `Mailbox` + `TaskList` 构建。现有无对应功能，属于全新能力。实现依赖 `session/` 包提供的持久化基础设施。
