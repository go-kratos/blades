---
type: design
title: Tool 协议系统设计
date: 2026-05-05
status: draft
parent: design-agent-framework.md
related: [design-agent-framework.md]
tags: [agentos, tools, protocol, runtime]
---

# Tool 协议系统设计

## 1. 概述

`tools/` 是 AgentOS 的工具协议叶子包。它定义工具如何声明规格、接收 JSON 输入、返回多模态结果，以及如何被运行时解析。它不承载模型调度、工具并发执行、权限裁决、重试或业务标识传播。

工具执行编排属于根包默认 `llmAgent` 的 `ToolExecutor`。工具安全、预算和授权属于 `policy/` 或 middleware。工具实现只需要关注自身协议和处理逻辑。

## 2. Tool 接口

v1 `Tool` 只有两个方法：

```go
package tools

type Tool interface {
    Spec() ToolSpec
    Handle(ctx context.Context, input json.RawMessage) (*Result, error)
}
```

`Spec()` 返回工具声明，供 provider tool calling 与应用展示使用。`Handle` 接收原始 JSON 输入，由工具自行解码和校验。

`ToolSpec` 只定义在 `tools` 包，是工具元数据的唯一真源。`model/` 与各 provider 直接依赖 `tools.ToolSpec`，不做类型别名也不在 `model` 包重复定义：

```go
type ToolSpec struct {
    Name         string
    Description  string
    InputSchema  *jsonschema.Schema
    OutputSchema *jsonschema.Schema
}
```

`OutputSchema` 保留在 `ToolSpec` 内：Gemini 等 provider 以及结构化输出评估器依赖它来描述工具结果的 schema。Provider 通过 `tool.Spec()` 获取所有元数据，禁止再为名称、描述或 schema 暴露独立的 getter。

典型实现：

```go
type EchoTool struct{}

func (EchoTool) Spec() tools.ToolSpec {
    return tools.ToolSpec{
        Name:        "echo",
        Description: "return input text",
        InputSchema: json.RawMessage(`{"type":"object"}`),
    }
}

func (EchoTool) Handle(ctx context.Context, input json.RawMessage) (*tools.Result, error) {
    var req struct {
        Text string `json:"text"`
    }
    if err := json.Unmarshal(input, &req); err != nil {
        return nil, err
    }
    return &tools.Result{
        Parts: []content.Part{content.Text{Text: req.Text}},
    }, nil
}
```

工具默认可并发调用；需要串行化的资源应由工具内部锁、外部 middleware 或编排器处理。

## 3. Result

工具结果只包含多模态内容：

```go
type Result struct {
    Parts []content.Part
}
```

错误通过 `Handle` 的第二返回值表达，不写入 `Result`。Loop 在 `err != nil` 时负责生成错误语义的工具结束事件，并在 provider 消息侧标记该工具结果为错误。

`Result.Parts` 直接使用 `content.Part`：

- 文本结果使用 `content.Text`。
- 二进制、文件、URI 或 JSON 结果使用 `content.Blob`，并通过 MIME 表达格式。
- provider 可校验的思考内容使用 `content.Thinking`。

`tools/` 不定义统一元数据字段。业务标识、审计字段和展示数据应由应用层结构或调用上下文承载。

## 4. 可选能力接口

v1 保留四个可选能力接口，全部为协议层 opt-in：未实现的工具按默认行为处理。

```go
type ReadOnlyTool interface {
    Tool
    ReadOnly() bool
}

type DestructiveTool interface {
    Tool
    Destructive() bool
}

type ToolFilter interface {
    Filter(ctx context.Context, tools []Tool) ([]Tool, error)
}

type StreamingTool interface {
    Tool
    // Stream 以增量语义产出结果：每次 yield 的 *Result 仅包含相对上一次
    // yield 新增的 Parts，不是累计快照。ToolExecutor 负责按顺序累积 Parts，
    // 形成最终回传给模型的工具结果。
    Stream(ctx context.Context, input json.RawMessage) iter.Seq2[*Result, error]
}
```

语义：

- `ReadOnlyTool`：工具声明自身是否只读，供 policy 和 UI 使用。
- `DestructiveTool`：工具声明自身是否可能产生破坏性副作用，供 policy 要求确认或拒绝。
- `ToolFilter`：协议层过滤器，按模型能力、运行模式、用户选择或 policy 预检查裁剪工具列表。最终是否允许调用仍由 policy 在执行前裁决。
- `StreamingTool`：工具按增量语义逐步产出 `Result`。`ToolExecutor` 在累积过程中可以将增量同步转发给上层流（例如 Agent 的 `Generator[*Message, error]`），最终再以累积结果回写给模型。`Stream` 与 `Handle` 同时存在，运行时优先使用 `Stream`；`Handle` 作为非流式回退使用。

以下能力不进入 `tools.Tool` 协议：

- 幂等、缓存、重试：由 middleware 或调用侧包装。
- 授权：由 `policy.Policy` 基于工具、输入和上下文裁决。
- 业务注解：由应用层嵌入业务结构体，不进入核心协议。

## 5. Resolver

工具发现与解析由 `Resolver` 完成：

```go
type Resolver interface {
    List(ctx context.Context) ([]Tool, error)
    Resolve(ctx context.Context, name string) (Tool, error)
}
```

`List` 返回当前运行时可见工具集合；`Resolve` 按名称解析单个工具。解析失败应返回普通 Go error，由调用侧使用 `errors.Is` / `errors.As` 判断。`tools` 提供 `StaticResolver` 作为内置实现；MCP、registry 等动态来源可以提供自己的 `Resolver`。

`ToolFilter`（见第 4 节）与 `Resolver` 组合即可表达"按 policy / 模型能力裁剪运行时可见工具"的诉求。

## 6. 控制流信号：sentinel error

`tools` 不引入 `ToolContext`、`Runtime` 或 context-scoped 注入。工具需要影响 Agent loop 控制流时，通过返回 sentinel error 完成；运行时使用 `errors.As` 识别并翻译为 `message.Actions`。

```go
type ErrLoopExit struct {
    Escalate bool
}

type ErrHandoff struct {
    Agent string
}
```

- `ExitTool` 返回 `&ErrLoopExit{Escalate: req.Escalate}`，`ToolExecutor` 翻译为 `message.Actions[tools.ActionLoopExit]`，`LoopAgent` 据此终止循环。
- Handoff 工具返回 `&ErrHandoff{Agent: name}`，`ToolExecutor` 翻译为 `message.Actions[tools.ActionHandoffToAgent]`，`RoutingAgent` 据此切换执行目标。

约束：

1. sentinel error 仅承载控制信号，不包含业务负载；如果模型仍需要工具结果文本，由 `ToolExecutor` 在识别 sentinel 后合成默认成功 payload。
2. 控制信号集合受协议层管控：新增控制语义必须在本设计文档中显式定义，再由 `ToolExecutor` 和对应 flow agent 同时支持。
3. 工具实现不再访问任何 `tools.ToolContext`/`Runtime`/`NewContext`/`FromContext` helper —— 它们已从 `tools` 包移除。Resolver、allowed list、当前 invocation 等运行时能力保留在 root/agent 层，通过 `Invocation` 或 `agent.FromContext` 暴露。

## 7. 工具执行编排边界

`tools/` 不决定工具何时执行、是否并发、如何取消、如何把结果写回 provider 消息。默认边界是根包 `ToolExecutor`：

```go
type ToolExecutor interface {
    Execute(ctx context.Context, in ToolExecuteInput) ToolExecuteOutput
}
```

ToolExecutor 负责：

- 通过 Agent 持有的 `tools.Resolver`（或 `Invocation.Tools`）查找工具。
- 执行 policy 检查与 `ToolFilter` 裁剪。
- 调用 `Tool.Handle`；当工具同时实现 `StreamingTool` 时优先使用 `Stream`，按增量语义累积 Parts。
- 识别 sentinel error（`ErrLoopExit` / `ErrHandoff`），翻译为 `message.Actions`，并在需要时合成默认成功 payload。
- 将结果转换为 `event.ToolEnd`，把多模态 `Result.Parts` 序列化进 provider 期望的 tool-message 内容（v1：纯文本拼接；`content.Blob` 以 `{"mime":..,"data_base64":..}` JSON 信封内嵌；provider 原生多模态编码留给后续迭代）。
- 把运行错误交回默认 `llmAgent`。

默认 `llmAgent` 再把 `event.ToolEnd.Result.Parts` 包装为 `content.ToolResult`，继续后续模型调用。

## 与红线对照

本文覆盖 r4、r16、r17、r29。
