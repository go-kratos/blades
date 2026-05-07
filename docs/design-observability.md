---
type: design
title: OTel 可观测集成
date: 2026-05-05
status: draft
parent: design-agent-framework.md
related: [design-agent-framework.md]
tags: [agentos, otel, hook, telemetry]
---

# OTel 可观测集成

## 1. 概述

v1 core 不定义 logger、tracer、meter、exporter 或独立可观测抽象。所有可观测能力通过 `hook.Hook` 旁路接入，并依赖 `context.Context` 传播 trace、deadline 和 cancellation。

OpenTelemetry 集成放在 `contrib/otel`。该包提供一个实现 `hook.Hook` 的 OTel hook，监听六类核心 hook event：`PreModelCall`、`PostModelCall`、`PreToolCall`、`PostToolCall`、`TurnStart`、`TurnEnd`，再产出 span、metric 和 event。它不改写请求，不返回 guardrail 决策，也不参与 Agent Loop 控制流。

## 2. 集成方式

应用在启动装配阶段创建 OTel hook，并把它注入 Agent：

```go
otelHook := contribotel.NewHook(
    contribotel.WithServiceName("coding-agent"),
    contribotel.WithCaptureUsage(true),
)

agent := blades.NewAgent("assistant",
    blades.WithModel(provider),
    blades.WithTools(resolver),
    blades.WithSession(store),
    blades.WithPolicy(pol),
    blades.WithHooks(otelHook),
    blades.WithCompact(compactor),
    blades.WithPrompt(builder),
)

input := make(chan event.Input)
out := agent.Run(ctx, input)
```

约定：

- exporter、resource、采样率、敏感字段脱敏由应用配置。
- `contrib/otel` 不导入应用配置包，不读取全局环境。
- trace context 只通过 `context.Context` 传递；工具运行时能力使用 `tools.NewContext` / `tools.FromContext`，session 使用 `session.NewContext` / `session.FromContext`，Agent 内省使用 `agent.NewContext` / `agent.FromContext`。
- Memory 不在根 Agent 配置中；应用可在 `prompt.Section` 中调用 `memory.Recall`，在 turn 后调用 `memory.Remember`，必要时用自定义 Hook 记录耗时。

## 3. Hook 到 OTel 的映射

| Hook event | OTel 映射 | 关键属性与指标 |
|---|---|---|
| `TurnStart` | 开始 turn 根 span | `agent.name`、`agent.turn`、`session.id` |
| `PreModelCall` | 开始 model 子 span | `model.provider`、`model.request.system_blocks`、`model.request.messages`、工具 spec 数量 |
| `PostModelCall` | 结束 model 子 span | `model.stop_reason`、`model.usage.input_tokens`、`model.usage.output_tokens`、错误状态 |
| `PreToolCall` | 开始 tool 子 span | `tool.name`、`tool.spec.name`、输入大小、只读/破坏性/流式能力标记 |
| `PostToolCall` | 结束 tool 子 span | `tool.result.parts`、结果大小、错误状态、耗时 metric |
| `TurnEnd` | 结束 turn 根 span | `turn.stop_reason`、`turn.parts`、汇总 token usage、错误状态 |

生命周期规则：

1. `TurnStart` / `TurnEnd` 包裹一轮 Agent Loop；根包 `Agent.Run(ctx, input <-chan event.Input) blades.Generator[event.Output, error]` 的每次 turn 都应形成一个根 span。
2. `PreModelCall` / `PostModelCall` 包裹一次 `model.Provider.Stream` 或 `model.Provider.Generate`；`Provider` 接口只有 `Name`、`Generate`、`Stream` 三方法，token 统计由 `model.Usage` 字段或独立的 `model.TokenCounter`（按能力探测）辅助产出。
3. `PreToolCall` / `PostToolCall` 包裹一次 `tools.Tool.Handle`；tool spec 与 `model.ToolSpec` 同构，便于统一记录工具 schema 信息。
4. 如果 ctx 被取消，span 状态应体现 cancellation；如果 hook 自身失败，应记录本地错误并避免影响主流程。
5. `PostModelCall` 可记录 token usage metric；`PostToolCall` 可记录工具耗时、错误计数和结果大小 metric；`TurnEnd` 可记录一轮汇总 metric。

## 4. OTel 集成边界

1. **核心无 vendor 依赖**：`blades`、`hook`、`model`、`tools` 不导入 OTel SDK；OTel 仅在 `contrib/otel` 中使用。
2. **语义集中在 hook**：模型调用、工具执行、turn 边界由六类 hook event 表达，OTel 集成通过 `hook.Hook` 直接消费，无需额外抽象层。
3. **应用治理**：采样、脱敏、导出目的地、resource 命名均为应用策略，由 `contrib/otel` 安装时配置。
4. **Interceptor 边界清晰**：可观测集成只做 Observer；安全、预算、内容改写走 `policy.Policy.Check` 或 hook Interceptor。

## 5. 自定义 Hook 示例

不用 OTel 也可以直接创建 metrics 或 log hook：

```go
metricsHook := hook.Chain(
    hook.OnPostToolCall(func(ctx context.Context, ev *hook.PostToolCall) error {
        status := "ok"
        if ev.Err != nil {
            status = "error"
        }
        metrics.Count("agent.tool.calls", 1,
            "agent", ev.AgentName,
            "tool", ev.ToolName,
            "status", status,
        )
        return nil
    }),

    hook.OnTurnEnd(func(ctx context.Context, ev *hook.TurnEnd) error {
        logger.InfoContext(ctx, "agent turn finished",
            "agent", ev.AgentName,
            "turn", ev.Turn,
            "stop_reason", ev.StopReason,
        )
        return nil
    }),
)

agent := blades.NewAgent("assistant",
    blades.WithModel(provider),
    blades.WithTools(resolver),
    blades.WithHooks(metricsHook),
)
```

如果应用需要动态启停、按租户分发或统一装配多个 hook，可以在应用层实现 registry，并让它自身实现 `hook.Hook`：

```go
registry := apphook.NewRegistry()
registry.Use(
    contribotel.NewHook(contribotel.WithServiceName("coding-agent")),
    metricsHook,
)

agent := blades.NewAgent("assistant",
    blades.WithModel(provider),
    blades.WithTools(resolver),
    blades.WithHooks(registry),
)
```

自定义 Hook 要遵守三条规则：

- 不调用 Mutator，不改写 `model.Request`、`model.Response`、工具输入或工具结果。
- 不把敏感 prompt、tool input、tool result 全量写入外部系统；必要时由应用传入脱敏函数。
- 失败时返回 nil 或由应用层聚合 hook 降级处理，避免旁路系统影响 Agent Loop。

## 与红线对照

- r26：核心不引入可观测抽象；OTel 通过 `contrib/otel` 提供 `hook.Hook`；六类 hook event 到 span/metric 的映射已覆盖。
- Hook 边界：只监听 `PreModelCall` / `PostModelCall` / `PreToolCall` / `PostToolCall` / `TurnStart` / `TurnEnd`，不参与 Interceptor 改写。
- 相关 v1 协议：根包 `Agent.Run`、根包 Option、`model.Provider` 三方法、`tools.Tool` 两方法、Memory 不进 root Agent、capability helper 使用 `NewContext` / `FromContext`。
