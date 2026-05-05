# Blades 文档索引

> 本文档提供项目技术文档的快速导航。AgentOS 相关文档描述目标架构，允许不兼容重构。

## 开发指南

- [开发规范](./DEVELOPMENT_GUIDE.md) - AI Coding 开发范式和工作流程

## 设计文档

- [Blades AgentOS Framework 设计蓝图](./design-agent-framework.md) - AgentOS 总体目标、包边界、依赖图和迁移阶段 `[draft]`
- [Event 系统与 Agent Loop 状态机](./design-event-agent-loop.md) - Event/Message 分层、Agent Run 接口和 loop 状态机 `[draft]`
- [消息与上下文系统](./design-message-context.md) - `model/`、上下文构建、压缩管线和 PromptBuilder `[draft]`
- [工具系统](./design-tool-system.md) - `tools/` 接口、结果 DTO、流式工具执行和生命周期 `[draft]`
- [扩展与 Hook 系统](./design-hook-extension.md) - core hook 事件、观察/拦截 handler 和扩展边界 `[draft]`
- [会话与持久化](./design-session.md) - `session/`、Store、JSONL、树形分支和恢复流程 `[draft]`
- [Policy 与交互模式边界](./design-policy-mode.md) - core policy primitives 与应用层模式边界 `[draft]`
- [Agent 组合与编排](./design-agent-orchestration.md) - `flow/`、Agent-as-Tool、run manager 和 orchestrator 边界 `[draft]`
- [Memory 系统](./design-memory.md) - memory core 抽象、recall/extract 注入点和文件 memory 边界 `[draft]`
- [基础设施](./design-infra.md) - 重试、Token 计数、可观测性和 graph 定位 `[draft]`
- [迁移路径](./design-migration.md) - breaking API 迁移顺序、包职责和验收标准 `[draft]`

## 历史已实现设计

- [流式响应优化设计](./design-streaming-optimization.md) - 旧 `iter.Seq2` 流式路径的性能优化参考 `[implemented]`

## 参考文档

- [pi-agent Framework 参考设计](./reference-pi-agent-framework.md) - pi-agent 的 Agent Loop、上下文管理、Memory、Tool、Extension 分层参考 `[draft]`
- [Claude Code Agent 参考设计](./reference-claude-code-agent.md) - Claude Code 的 Agent Loop、多策略压缩、Memory、Tool、权限、Hook 系统参考 `[draft]`

## 按主题检索

### AgentOS Core

- [Blades AgentOS Framework 设计蓝图](./design-agent-framework.md)
- [Event 系统与 Agent Loop 状态机](./design-event-agent-loop.md)
- [消息与上下文系统](./design-message-context.md)
- [工具系统](./design-tool-system.md)
- [会话与持久化](./design-session.md)

### Capability

- [Policy 与交互模式边界](./design-policy-mode.md)
- [扩展与 Hook 系统](./design-hook-extension.md)
- [Memory 系统](./design-memory.md)
- [基础设施](./design-infra.md)

### Composition

- [Agent 组合与编排](./design-agent-orchestration.md)
- [迁移路径](./design-migration.md)

### Reference

- [pi-agent Framework 参考设计](./reference-pi-agent-framework.md)
- [Claude Code Agent 参考设计](./reference-claude-code-agent.md)
- [流式响应优化设计](./design-streaming-optimization.md)

## 文档状态统计

- Draft: 13
- Review: 0
- Approved: 0
- Implemented: 1
- Deprecated: 0

---

**更新说明:** 本索引需在文档创建和更新时手动维护。
