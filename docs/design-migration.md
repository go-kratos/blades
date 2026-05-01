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

**flow/ 包**：5 种组合 Agent 需要适配新的 `<-chan InputEvent` / `<-chan OutputEvent` 签名。
- `SequentialAgent`：内部 channel 串联
- `ParallelAgent`：fan-out/fan-in OutputEvent channel
- `LoopAgent`：内循环消费 OutputEvent，检查 TurnEndEvent 而非 `ActionLoopExit`
- `RoutingAgent`：从 OutputEvent 中提取 handoff 信号
- `DeepAgent`：保持不变（已通过 graph 桥接）

**contrib/ 包**：实现 `model.Provider` 接口，各自内部处理格式转换。
- `contrib/anthropic`：将现有 `applyEphemeralCache` 和 tool message 拆分逻辑保留在包内部
- `contrib/openai`：将 function_call 格式转换保留在包内部
- `contrib/gemini`：实现 `model.Provider`，将 Gemini 特有的 FunctionCall/FunctionResponse 格式转换保留在包内部
- `contrib/mcp`：MCP 工具桥接迁移，将 MCP tool schema 映射到新的 `tools.Tool` 接口，保留 SSE/stdio transport 逻辑
- `contrib/otel`：从 Middleware 迁移到 Hook 系统集成

**skills/ 包**：接口基本不变，`Toolset.ComposeTools` 需要适配新的 `tools.Tool` 接口（精简版）。

**graph/ 包**：保持独立，通过 `flow/graph.go` 桥接。
