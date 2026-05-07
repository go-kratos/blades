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
        InputSchema: &jsonschema.Schema{
            Type: "object",
            Properties: map[string]*jsonschema.Schema{
                "text": {Type: "string"},
            },
            Required: []string{"text"},
        },
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

v1 在 `tools.Tool` 之外保留**3 个工具能力接口**与**1 个集合过滤器**，全部为协议层 opt-in：未实现的工具按默认行为处理。

```go
// 工具能力接口（嵌入 Tool）：
type ReadOnlyTool interface {
    Tool
    ReadOnly() bool
}

type DestructiveTool interface {
    Tool
    Destructive() bool
}

type StreamingTool interface {
    Tool
    // Stream 以增量语义产出结果：每次 yield 的 *Result 仅包含相对上一次
    // yield 新增的 Parts，不是累计快照。ToolExecutor 负责按顺序累积 Parts，
    // 形成最终回传给模型的工具结果。
    Stream(ctx context.Context, input json.RawMessage) iter.Seq2[*Result, error]
}

// 工具集合过滤器（不嵌入 Tool，作用于工具列表）：
type ToolFilter interface {
    Filter(ctx context.Context, tools []Tool) ([]Tool, error)
}
```

语义：

- `ReadOnlyTool`：工具声明自身是否只读，供 policy 和 UI 使用。
- `DestructiveTool`：工具声明自身是否可能产生破坏性副作用，供 policy 要求确认或拒绝。
- `StreamingTool`：工具按增量语义逐步产出 `Result`。`ToolExecutor` 在累积过程中可以将增量同步转发给上层流，最终再以累积结果回写给模型。`Stream` 与 `Handle` 同时存在，运行时优先使用 `Stream`；`Handle` 作为非流式回退使用。
- `ToolFilter`：协议层过滤器，按模型能力、运行模式、用户选择或 policy 预检查裁剪工具列表。最终是否允许调用仍由 policy 在执行前裁决。`ToolFilter` 不嵌入 `Tool`，作用对象是 `[]Tool` 集合。

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

## 6. 控制流信号：sentinel error → 专用 Output 帧

`tools` 提供 **`ToolContext`** 暴露当前工具调用的运行时元数据；工具需要影响 Agent loop 控制流时，通过返回 sentinel error 完成。这两类 sentinel error 类型 `ErrLoopExit` / `ErrHandoff` 定义在 `tools` 包内（`tools/errors.go`），运行时使用 `errors.As` 识别，并紧跟在产生它的 `event.ToolEnd` 之后发出**专用 Output 帧** `event.LoopExit` / `event.Handoff`（运行时控制信号承载于独立 Output 变体上而非 `model.Message`，参见 [design-model-provider.md](design-model-provider.md) §6 与 [design-event-agent-loop.md](design-event-agent-loop.md) §4.2）。

```go
// package tools (tools/context.go)

// ToolContext 暴露当前工具调用的运行时元数据，由 ToolExecutor 在调用 Tool.Handle
// 之前注入到 ctx；不承载控制信号（控制流统一走 sentinel error）。
type ToolContext interface {
    ID() string   // 当前 tool call 的唯一 ID（与 event.ToolStart/ToolEnd.ToolID 同源）
    Name() string // 当前工具名（与 Tool.Spec().Name 一致）
}

func NewContext(ctx context.Context, tc ToolContext) context.Context
func FromContext(ctx context.Context) (ToolContext, bool)
```

```go
// package tools (tools/errors.go)

type ErrLoopExit struct {
    Escalate bool
}

func (e *ErrLoopExit) Error() string { return "tools: loop exit" }

type ErrHandoff struct {
    Agent string
    Carry *content.ToolResult // 可选转交 payload
}

func (e *ErrHandoff) Error() string { return "tools: handoff to " + e.Agent }
```

翻译规则（由 `ToolExecutor` 实施）：

- `ExitTool` 返回 `&ErrLoopExit{Escalate: req.Escalate}`：发出 `event.ToolEnd` 后紧跟一帧 `event.LoopExit{ToolID, ToolName, Escalate}`；`flow.LoopAgent` 据此终止循环。
- Handoff 工具返回 `&ErrHandoff{Agent: name, Carry: ...}`：发出 `event.ToolEnd` 后紧跟一帧 `event.Handoff{ToolID, ToolName, Agent, Carry}`；`flow.RoutingAgent` 据此切换执行目标。

约束：

1. sentinel error 仅承载控制信号，不包含业务负载；如果模型仍需要工具结果文本，由 `ToolExecutor` 在识别 sentinel 后合成默认成功 payload，写入 `ToolEnd.Result.Parts`。
2. 控制信号集合受协议层管控：新增控制语义必须在本设计文档中显式定义新的 sentinel error **与对应的 sealed Output 变体**（在 `event/` 包中），再由 `ToolExecutor` 和对应 flow agent 同时支持；不再使用 `map[string]any` + 字符串 key。
3. 工具实现通过 `tc, ok := tools.FromContext(ctx)` 获取当前 `ToolContext`（`ID` / `Name`）；`ToolContext` 仅暴露调用元数据，不再承载 `Actions` / `SetAction` 等控制信号——控制流一律走 sentinel error 与 sealed Output 帧。Resolver、allowed list 等 Agent 级运行时能力仍保留在 root/agent 层，通过 `Invocation` 或 `agent.FromContext` 暴露，不与 `tools.ToolContext` 混淆。
4. **控制信号不进入 `model.Message` 也不进入 `Session`**：协议层 `model.Message` 严格保持 protocol-only；运行时控制信号仅以 `event.LoopExit` / `event.Handoff` 帧出现在 Output 流上，由 flow 编排层与 hook 读取（hook 侧对应 `hook.LoopExit` / `hook.Handoff` 同源同步触发，详见 [design-hook-extension.md](design-hook-extension.md)）。

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
- 在调用 `Tool.Handle` 之前，通过 `tools.NewContext(ctx, tc)` 注入当前 `ToolContext`（携带本次 tool call 的 `ID` / `Name`，与同源 `event.ToolStart`/`event.ToolEnd` 字段对齐）。
- 调用 `Tool.Handle`；当工具同时实现 `StreamingTool` 时优先使用 `Stream`，按增量语义累积 Parts。
- 识别 sentinel error（`ErrLoopExit` / `ErrHandoff`），在 `event.ToolEnd` 之后紧跟发出 `event.LoopExit` / `event.Handoff` 控制帧，并在需要时为模型 tool-message 合成默认成功 payload。
- 将结果转换为 `event.ToolEnd`，把多模态 `Result.Parts` 序列化进 provider 期望的 tool-message 内容（v1：纯文本拼接；`content.Blob` 以 `{"mime":..,"data_base64":..}` JSON 信封内嵌；provider 原生多模态编码留给后续迭代）。
- 把运行错误交回默认 `llmAgent`。

默认 `llmAgent` 再把 `event.ToolEnd.Result.Parts` 包装为 `content.ToolResult`，继续后续模型调用。

## 与红线对照

本文覆盖 r4、r16、r17、r29。
