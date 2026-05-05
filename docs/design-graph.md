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

## 包边界

```text
graph/ -> standard library + graph-local dependencies
flow/  -> blades/, event/, tools/, graph/   // 可选桥接
```

`graph/` 可以独立用于非 LLM 工作流。节点如果需要调用模型或工具，应通过应用注入的函数或接口完成，而不是让 `graph/` 依赖 `model.Provider` 或 `tools.Tool`。

## Agent 桥接

需要把 DAG 暴露为 Agent 时，在 `flow/` 或 contrib 中提供 adapter：

```go
func GraphAgent(name string, g *graph.Executor, opts ...GraphOption) blades.Agent
```

adapter 负责：

- 从 `event.Input` 构造 graph 初始 state。
- 执行 graph。
- 把 graph state 或节点输出转换为 `event.Output`。
- 处理 ctx cancellation 和 output 背压。

反方向如果 Agent 要作为 graph node，也由应用或 flow adapter 包装为 node handler。

## 设计决策

1. **Graph 独立**：DAG/checkpoint/condition 与 Agent Loop 不是同一种抽象。
2. **桥接在边界包**：`flow.GraphAgent` 或 contrib adapter 可以依赖两边，核心 `graph/` 不依赖 `blades/`。
3. **不以 graph 重写 flow**：`flow.Sequential/Parallel/Loop` 是轻量 Event-to-Event 组合，复杂 DAG 才使用 `graph/`。
