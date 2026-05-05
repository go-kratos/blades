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

`tools.ToolSpec` 与 `model.ToolSpec` 同构，用于避免上层在工具协议和 provider 请求之间反复转换：

```go
type ToolSpec = model.ToolSpec
```

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

v1 只保留三个可选能力接口：

```go
type ReadOnlyTool interface {
    ReadOnly() bool
}

type DestructiveTool interface {
    Destructive() bool
}

type StreamingTool interface {
    Stream(ctx context.Context, input json.RawMessage) iter.Seq2[*Result, error]
}
```

语义：

- `ReadOnlyTool`：工具声明自身是否只读，供 policy 和 UI 使用。
- `DestructiveTool`：工具声明自身是否可能产生破坏性副作用，供 policy 要求确认或拒绝。
- `StreamingTool`：工具可逐步产出 `Result`，由 `ToolExecutor` 转为工具增量输出。

以下能力不进入 `tools.Tool` 协议：

- 幂等、缓存、重试：由 middleware 或调用侧包装。
- 授权：由 `policy.Policy` 基于工具、输入和上下文裁决。
- 业务注解：由应用层嵌入业务结构体，不进入核心协议。

## 5. Resolver 与 ToolFilter

工具发现与解析由 `Resolver` 完成：

```go
type Resolver interface {
    List(ctx context.Context) ([]Tool, error)
    Resolve(ctx context.Context, name string) (Tool, error)
}
```

`List` 返回当前运行时可见工具集合；`Resolve` 按名称解析单个工具。解析失败应返回普通 Go error，由调用侧使用 `errors.Is` / `errors.As` 判断。

`ToolFilter` 位于 `tools/` 包，用于对工具集合做协议层过滤：

```go
type ToolFilter interface {
    Filter(ctx context.Context, tools []Tool) ([]Tool, error)
}
```

过滤可用于按模型能力、运行模式、用户选择或 policy 预检查裁剪工具列表。最终是否允许调用仍由 policy 在执行前裁决。

## 6. Runtime 与 context helper

工具运行时能力通过 `tools.Runtime` 表达：

```go
type Runtime struct {
    Resolver Resolver
    Allowed  []Tool
}
```

`Runtime` 不包含 Agent 名称。当前 Agent 内省由 `agent.FromContext` 提供，避免在工具协议中重复携带身份字段。

`tools/` 使用 stdlib 风格 context helper：

```go
func NewContext(ctx context.Context, runtime Runtime) context.Context
func FromContext(ctx context.Context) (Runtime, bool)
```

进入核心 context 的工具 runtime 必须满足三条准则：

1. runtime-scoped：随本次运行取消而失效。
2. 一次 Run 内稳定不变。
3. 至少被 Loop、Tool、Hook 或 Middleware 中两层共同需要。

应用层标识例如用户、渠道、工作区、会话映射等不进入 `tools.Runtime`，应由应用自己的 context key 管理。

## 7. 工具执行编排边界

`tools/` 不决定工具何时执行、是否并发、如何取消、如何把结果写回 provider 消息。默认边界是根包 `ToolExecutor`：

```go
type ToolExecutor interface {
    Execute(ctx context.Context, in ToolExecuteInput) ToolExecuteOutput
}
```

ToolExecutor 负责：

- 根据 `tools.Runtime.Resolver` 查找工具。
- 执行 policy 检查。
- 调用 `Tool.Handle` 或 `StreamingTool.Stream`。
- 将结果转换为 `event.ToolEnd`。
- 把运行错误交回默认 `llmAgent`。

默认 `llmAgent` 再把 `event.ToolEnd.Result.Parts` 包装为 `content.ToolResult`，继续后续模型调用。

## 与红线对照

本文覆盖 r4、r16、r17、r29。
