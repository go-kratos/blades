# Blades 文档索引

> 本文档提供项目技术文档的快速导航。AgentOS 相关文档描述目标架构，允许不兼容重构。

## 开发指南

- [开发规范](./DEVELOPMENT_GUIDE.md) - AI Coding 开发范式和工作流程

## 设计文档

- [Blades AgentOS Framework 设计蓝图](./design-agent-framework.md) - AgentOS 总体目标、包边界、依赖图和迁移阶段 `[draft]`
- [Event 系统与 Agent Loop](./design-event-agent-loop.md) - Event/Message 分层、Agent Loop 顺序流程与行为事件 hook、Event/Message 转换边界 `[draft]`
- [工具系统](./design-tool-system.md) - `tools/` 接口（Spec+Handle 两方法）、Result、Resolver、Filter 和执行上下文 `[draft]`
- [扩展与 Hook 系统](./design-hook-extension.md) - 单 `hook.Hook` 接口（6 个生命周期方法：BeforeModel/AfterModel/BeforeTool/AfterTool/BeforeTurn/AfterTurn）+ `hook.Noop` 嵌入式默认实现、指针改写、Abort 三件套、应用事件隔离 `[draft]`
- [会话与持久化](./design-session.md) - Session 接口（6 方法纯追加）、`*Message` 载荷、与 compaction 解耦、fork/replay 走 NewSession+WithMessages `[draft]`
- [Policy 与交互模式边界](./design-policy-mode.md) - 单一 Policy.Check 接口与应用层模式边界 `[draft]`
- [Agent 组合与编排](./design-agent-orchestration.md) - `flow/` 组合（Sequential/Parallel/Loop/Routing/Deep）与多 Agent 边界 `[draft]`
- [Memory 系统](./design-memory.md) - Memory 接口（Recall+Remember+Forget）、`Entry` / `Query` 数据载体、应用层经 prompt.Memory 注入策略 `[draft]`
- [Prompt 系统](./design-prompt.md) - Builder 接口 + Section 函数类型；Static/Dynamic/System/Memory 工厂 `[draft]`
- [Compact 系统](./design-compact.md) - 单一 Compactor 接口与内置实现（Window/ToolResultBudget/Summarize/Chain） `[draft]`
- [Model 与 Provider](./design-model-provider.md) - `model/` Message、Part、Provider（Name/Generate/Stream）、TokenCounter（按能力探测）、Request/Response/Chunk、Options sealed union `[draft]`

## 参考文档

- [pi-agent Framework 参考设计](./reference-pi-agent-framework.md) - pi-agent 的 Agent Loop、上下文管理、Memory、Tool、Extension 分层参考 `[draft]`
- [Claude Code Agent 参考设计](./reference-claude-code-agent.md) - Claude Code 的 Agent Loop、多策略压缩、Memory、Tool、权限、Hook 系统参考 `[draft]`

## 按主题检索

### AgentOS Core

- [Blades AgentOS Framework 设计蓝图](./design-agent-framework.md)
- [Event 系统与 Agent Loop](./design-event-agent-loop.md)
- [Model 与 Provider](./design-model-provider.md)
- [工具系统](./design-tool-system.md)
- [会话与持久化](./design-session.md)

### Capability

- [Policy 与交互模式边界](./design-policy-mode.md)
- [扩展与 Hook 系统](./design-hook-extension.md)
- [Memory 系统](./design-memory.md)
- [Prompt 系统](./design-prompt.md)
- [Compact 系统](./design-compact.md)

### Composition

- [Agent 组合与编排](./design-agent-orchestration.md)

### Reference

- [pi-agent Framework 参考设计](./reference-pi-agent-framework.md)
- [Claude Code Agent 参考设计](./reference-claude-code-agent.md)

## 文档状态统计

- Draft: 11
- Review: 0
- Approved: 0
- Implemented: 0
- Deprecated: 0

---

**更新说明:** 本索引需在文档创建和更新时手动维护。
