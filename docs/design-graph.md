---
type: design
title: Graph 定位
parent: design-agent-framework.md
date: 2026-05-01
status: draft
modules: [module-12]
---

# Graph 定位

`graph/` 保持独立 DAG 执行器，不并入 Agent Loop，也不导入 `blades/`。DAG 的核心语义是节点、边、状态、条件、checkpoint；Agent Loop 的核心语义是 Event、Message、Provider streaming、tools、session 和 compact。两者不应共享一个大接口。

## 包边界（硬约束）

```text
graph/ -> standard library + graph-local dependencies     // 核心 DAG，零 Agent 依赖
flow/  -> blades/, event/, tools/                          // 命令式组合
graph 调用 flow ✅（adapter 在 graph/ 或 contrib/）
flow  调用 graph ❌（禁止；保持 graph 是底层执行器，不依赖上层组合）
```

`graph/` 可以独立用于非 LLM 工作流。节点如果需要调用模型或工具，应通过应用注入的函数或接口完成，而不是让 `graph/` 依赖 `model.Provider` 或 `tools.Tool`。`graph` 与 `flow` 概念上不重叠：`graph/` 是声明式 DAG（节点+边+条件路由+checkpoint），`flow/` 是命令式组合（Sequential/Parallel/Loop/AsTool 四件套），二者职责分离。

## Agent 桥接

需要把 DAG 暴露为 Agent 时，在 `flow/` 或 contrib 中提供 adapter（**桥接方向只能是 `graph -> flow/Agent`**，`flow` 不调用 `graph`）：

```go
// 在 contrib/graphagent 或应用层提供（不放进 graph/，避免 graph/ 反向依赖 blades/）
func GraphAgent(name string, g *graph.Executor, opts ...GraphOption) blades.Agent
```

adapter 负责：

- 从 `event.Input` 构造 graph 初始 state。
- 执行 graph。
- 把 graph state 或节点输出转换为 `event.Output`。
- 处理 ctx cancellation 和 output 背压。

反方向如果 Agent 要作为 graph node，也由应用或 contrib adapter 包装为 node handler——同样不允许在 `graph/` 内部直接 import `blades.Agent`。

## 设计决策

1. **Graph 独立**：DAG/checkpoint/condition 与 Agent Loop 不是同一种抽象。
2. **桥接在边界包**：`contrib/graphagent` 或 flow adapter 可以依赖两边，核心 `graph/` 不依赖 `blades/`，核心 `flow/` 不依赖 `graph/`。
3. **不以 graph 重写 flow**：`flow.Sequential/Parallel/Loop/AsTool` 是轻量 Event-to-Event 组合（r26），复杂 DAG 才使用 `graph/`。
