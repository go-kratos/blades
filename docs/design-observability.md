---
type: design
title: Observability
parent: design-agent-framework.md
date: 2026-05-01
status: draft
modules: [module-11]
---

# Observability

核心包不内置 tracing 配置、exporter 或全局 meter。可观测性通过 `hook/` 生命周期事件接入，OpenTelemetry 集成放在 `contrib/otel`。

## Hook 集成

`contrib/otel` 注册 core hook handler：

- `AgentStart` / `AgentEnd` 创建和结束 agent run span。
- `TurnStart` / `TurnEnd` 标记 turn span、stop reason 和 token usage。
- `BeforeModelRequest` / `AfterModelResponse` 创建 model span，记录 model、source、usage、error。
- `PreToolUse` / `PostToolUse` 创建 tool span，记录 tool name、error 和耗时。
- `PolicyRequest` / `PolicyDecision` 记录 policy decision，但不记录敏感输入全文。
- `PreCompact` / `PostCompact` 记录 tokens before/after 和 compact strategy。

Hook 只拿 snapshot，不拿 raw request。OTel handler 不应修改 Agent 行为；需要采样、脱敏、span 命名和 exporter 配置时，由应用在注册 contrib hook 时提供 option。

## Span 生命周期

Agent Loop 必须保证每个开始的 span 都在同一生命周期边界结束。Model span 在 provider stream 完成或失败时结束；Tool span 在工具返回、panic recovery 或 ctx cancellation 后结束；Compact span 在策略返回后结束。

错误标记规则：

- 可恢复错误记录为 span event，并在 `event.Error` 中继续流出。
- 终止错误设置 span status error，并跟随 `event.Done{ReasonError}`。
- ctx cancellation 使用 cancellation status，不包装成 provider fatal error。

## 边界

- `blades/` 不导入 OTel SDK。
- `hook/` 不导入 OTel SDK。
- `contrib/otel` 可以依赖 `hook/`、OTel API/SDK 和 semantic conventions。
- 应用负责 exporter、resource、采样率、敏感字段脱敏和 trace propagation 接入。

## 设计决策

1. **Hook 驱动而非核心内置**：保持 core 无可观测性 vendor 依赖。
2. **快照优先**：避免 tracing handler 修改 model/tool/policy 请求。
3. **应用负责治理**：采样、脱敏和导出目的地都是产品策略。
