---
type: design
title: Agent 组合与编排
parent: design-agent-framework.md
date: 2026-05-01
status: draft
modules: [module-7]
---

# Agent 组合与编排

## 设计结论

`flow/` 是 AgentOS 中唯一的 Agent 组合层，承载通用、可组合、与场景无关的原语。所有原语都返回普通 `blades.Agent`，可以被无限嵌套。

核心原语统一使用 `NewXxxAgent(XxxConfig{...})` 构造函数 + Config 结构体的风格：

- `flow.NewSequentialAgent`：顺序串接
- `flow.NewParallelAgent`：并发 fan-out / fan-in
- `flow.NewLoopAgent`：循环执行直到条件停止
- `flow.NewRoutingAgent`：由 LLM 决策单跳分发到子 Agent
- `flow.NewDeepAgent`：在普通 Agent 上叠加 todos / task 工具

`flow/` 单向依赖 `event/` 与 `tools/`（不依赖 `model/`），**根包不依赖 `flow/`**，避免根包膨胀。

> 后台执行、生命周期、workspace 隔离、preset Agent、复杂多方编排（orchestrator/team/swarm）都不属于 `flow/` 的职责，由应用层或 `contrib/` 提供，本文不展开。

## 与新 Agent 接口的对齐

按 [design-agent-framework.md](design-agent-framework.md) 的最新结论，`blades.Agent` 接口为：

```go
type Agent interface {
    Name() string
    Description() string
    Run(context.Context, <-chan event.Input) (<-chan event.Output, error)
}
```

`flow/` 中所有原语都遵循同一接口：**只组合 `event.Input` / `event.Output` channel，不读取 `model.Message`**，也不感知 provider、session、policy。组合的结果仍是一个普通 `blades.Agent`，对调用方无差别。

## API 总览

```go
package flow

type SequentialConfig struct {
    Name        string
    Description string
    SubAgents   []blades.Agent
}
func NewSequentialAgent(cfg SequentialConfig) blades.Agent

type ParallelConfig struct {
    Name        string
    Description string
    SubAgents   []blades.Agent
}
func NewParallelAgent(cfg ParallelConfig) blades.Agent

type LoopConfig struct {
    Name          string
    Description   string
    MaxIterations int           // 默认 10
    Condition     LoopCondition // 可选；优先级高于 ExitTool 信号
    SubAgents     []blades.Agent
}
func NewLoopAgent(cfg LoopConfig) blades.Agent

type RoutingConfig struct {
    Name        string
    Description string
    SubAgents   []blades.Agent
    Router      Router
}
func NewRoutingAgent(cfg RoutingConfig) (blades.Agent, error)

type DeepConfig struct {
    Name          string
    Description   string
    SubAgents     []blades.Agent
    MaxIterations int
}
func NewDeepAgent(cfg DeepConfig) (blades.Agent, error)
```

## Sequential

`SequentialAgent` 按顺序运行 `SubAgents`：把上一段子 Agent 的 `<-chan event.Output` 经过桥接策略转成下一段子 Agent 的 `<-chan event.Input`，最终输出汇聚成一个统一的输出 channel。

```go
pipeline := flow.NewSequentialAgent(flow.SequentialConfig{
    Name:      "research-pipeline",
    SubAgents: []blades.Agent{researcher, planner, executor},
})

input := make(chan event.Input, 1)
input <- event.NewPromptText("...")
close(input)

output, err := pipeline.Run(ctx, input)
if err != nil {
    return err
}
for out := range output {
    // ...
}
```

默认桥接策略只在 `event.TurnEnd` 后把本轮最终内容作为下一段的 `event.Prompt`，**不**默认转发 `ToolStart` / `ToolDelta` / `ToolEnd` 等工具生命周期事件——这些事件属于用户可见输出，不应隐式变成下游 prompt。其它桥接需求由调用方显式提供：

```go
type Bridge interface {
    NextInput(ctx context.Context, from <-chan event.Output) (<-chan event.Input, error)
}
```

报错通过下游 `event.Error` 进入统一输出流，最终发送 `event.Done` 后 close。

## Parallel

`ParallelAgent` 把同一份 input channel fan-out 到多个子 Agent（每个子 Agent 拿到独立副本以避免互相消费），所有子 Agent 的输出 fan-in 到同一个 `<-chan event.Output`。任一子 Agent 触发 `event.Error` 时由调用方决定是否取消其它子 Agent（默认随 ctx 一起取消）。

```go
search := flow.NewParallelAgent(flow.ParallelConfig{
    Name:      "multi-search",
    SubAgents: []blades.Agent{keywordSearch, vectorSearch, webSearch},
})
```

Parallel 不给 `event.Output` 加 wrapper。需要区分来源时，由调用方为每个子 Agent 配置不同的 `Name()`，再通过 hook/trace 记录来源；不要为来源信息改 channel/事件类型。

## Loop

`LoopAgent` 在一组 `SubAgents` 上重复迭代，直到下列任一条件成立：

- `Condition(ctx, LoopState) (bool, error)` 返回 `(false, nil)`：正常停止
- `Condition` 返回非 nil error：以该 error 终止（通过 `event.Error` 上抛）
- 子 Agent 输出 `event.TurnEnd{Action: event.LoopExit{Escalate: ...}}`（由 `tools.ErrLoopExit` sentinel 翻译而来）：当前实现直接结束 loop；`Escalate` 供上层策略识别
- 达到 `MaxIterations`（默认 10）

```go
worker := flow.NewLoopAgent(flow.LoopConfig{
    Name:          "review-loop",
    MaxIterations: 8,
    Condition: func(ctx context.Context, s flow.LoopState) (bool, error) {
        return needsAnotherRound(s.LastOutput), nil
    },
    SubAgents: []blades.Agent{reviewer},
})
```

`LoopState` 暴露 `Iteration / LastOutput`，全部建立在 `event.Output` 之上。Loop 不直接读取模型上下文；需要更复杂判断时通过 Agent 中间件、`tools.ExitTool` 或 session state 实现，避免把模型推理塞进 flow 层。

## Routing

`RoutingAgent` 用于"由应用 Router 决策接下来交给哪个子 Agent"的单跳分发：

1. 从输入流读取第一条 `event.Input`
2. 调用 `Router.Route(ctx, input, subAgents)` 选择目标子 Agent
3. 将同一条输入转交给目标子 Agent，并透传其输出

```go
router, err := flow.NewRoutingAgent(flow.RoutingConfig{
    Name:      "support-router",
    Router:    flow.RouterFunc(routeByInput),
    SubAgents: []blades.Agent{billing, technical, sales},
})
```

Routing 只做单跳。多跳/链式路由通过组合 `NewRoutingAgent` + `NewSequentialAgent` 实现。

## Deep Agent

`DeepAgent` 是面向多步、可分解任务的"加强版 Agent"。它在普通 `blades.NewAgent` 之上叠加：

- `write_todos` 工具与对应 prompt：让模型显式维护任务列表
- 可选的 `task` 工具：当 `SubAgents` 非空或允许 general-purpose agent 时，把子 Agent 暴露为可被调用的 task
- 用户额外传入的 `Tools` 与 `Middlewares`

```go
deep, err := flow.NewDeepAgent(flow.DeepConfig{
    Name:        "researcher",
    Model:       model,
    Instruction: "Break down the question and explore step by step.",
    Tools:       []tools.Tool{search, fetch},
    SubAgents:   []blades.Agent{summarizer},
})
```

DeepAgent 仍然是一个普通 `blades.Agent`，可以再被 Sequential/Parallel/Loop/Routing 组合。当前实现通过子 Agent 最后一条 `TurnEnd.Action` 中的 `event.Handoff{Agent}` 选择下一跳；任务分解的具体协议（todos 字段、task 工具的入参）属于内部实现细节，flow 层不暴露。

## 关键设计决策

1. **统一 Agent 接口**：所有原语都遵循 `Run(ctx, <-chan event.Input) (<-chan event.Output, error)`；组合的结果仍是一个普通 `blades.Agent`，可以无限嵌套。
2. **统一构造风格**：所有原语用 `NewXxxAgent(XxxConfig{...})`，方便未来在不破坏调用方的情况下增加字段。
3. **只组合 channel，不读取 Message**：`flow/` 不感知 `model.Message`、provider、session；需要复杂 DAG/checkpoint/条件边时使用 `graph/`，不要把工作流语义塞进 flow。
4. **桥接显式化**：Sequential 默认只透传 `event.TurnEnd` 的最终内容，工具生命周期事件不默认变成下游 prompt；其它桥接策略由调用方显式提供。
5. **来源信息靠 `Name()`**：Parallel/Routing 都不给事件加 wrapper，调用方通过子 Agent 名字与 trace/hook 还原来源。
6. **范围克制**：`flow/` 只做组合；后台运行、workspace、preset、复杂编排都放到应用层或 `contrib/`，避免根包膨胀。
