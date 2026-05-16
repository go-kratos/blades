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

工具执行编排属于根包默认 `llmAgent` 的内部 tool wave。工具安全、预算和授权属于 `policy/`、hook、provider 适配层或应用层组合。工具实现只需要关注自身协议和处理逻辑。

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

工具调用批次由模型决定：如果 provider 返回多个 `content.ToolUse`，Agent Loop 会把这批调用并发执行；如果需要模型一次最多返回一个工具调用，应在 provider 构造时关闭并发工具调用（例如 OpenAI `parallel_tool_calls=false`、Claude `disable_parallel_tool_use=true`）。只需要保护工具内部资源时，推荐工具内部锁或资源池。

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
- 二进制、文件、URI 或 JSON 结果使用 `content.DataPart` / `content.FilePart` / `content.FileRefPart`，并通过 MIME 表达格式。
- provider 可校验的思考内容使用 `content.Thinking`。

`tools/` 不定义统一元数据字段。业务标识、审计字段和展示数据应由应用层结构或调用上下文承载。

## 4. 可选过滤器

当前 v1 在 `tools.Tool` 之外只公开**1 个集合过滤器**。工具能力标注（例如只读、破坏性、流式工具）尚未进入当前公开 API；需要这类信息时，先放在应用层工具实现、policy 或 resolver 包装中。

```go
type ToolFilter interface {
    Filter(ctx context.Context, tools []Tool) ([]Tool, error)
}
```

语义：

- `ToolFilter`：协议层过滤器，按模型能力、运行模式、用户选择或 policy 预检查裁剪工具列表。最终是否允许调用仍由 policy 在执行前裁决。`ToolFilter` 不嵌入 `Tool`，作用对象是 `[]Tool` 集合。

以下能力不进入 `tools.Tool` 协议：

- 幂等、缓存、重试：由 hook、provider 适配层或调用侧包装。
- 授权：由 `policy.Policy` 基于工具、输入和上下文裁决。
- 业务注解：由应用层嵌入业务结构体，不进入核心协议。

## 4.1 内置工具能力标注（参考）

`tools` 协议本身不规定哪些工具必须存在；以下能力矩阵是 policy 层参考约定，不是当前 `tools` 包的公开接口。运行时是否允许执行由 `policy.Policy` 在 `ToolRequest` 边界裁决（见 [design-policy-mode.md §内置工具决策矩阵](design-policy-mode.md#内置工具决策矩阵)）。

| 工具    | 只读 | 破坏性 | Input 关键字段                       | 备注                                                   |
| ------- | ------------ | --------------- | ------------------------------------ | ------------------------------------------------------ |
| `ls`    | true         | false           | `path`                                | 列目录；不修改文件系统。                              |
| `find`  | true         | false           | `path`, `pattern`, `type`            | 按名称/类型搜索；只读遍历。                            |
| `grep`  | true         | false           | `path`, `pattern`, `glob`            | 按内容搜索；只读读取文件。                             |
| `read`  | true         | false           | `path`, `range`                      | 读文件内容；不修改。                                  |
| `write` | false        | true            | `path`, `content`                    | 整文件覆盖写入；可能损毁既有内容。                    |
| `edit`  | false        | true            | `path`, `old_str`, `new_str`         | 增量改写；范围有限但仍属破坏性副作用。                |
| `bash`  | false        | true            | `command`, `args`, `cwd`             | 通用命令执行；语义随命令名而变（详见 policy 决策矩阵）。|

约束：

1. `只读=true` 必须严格满足"不写文件系统、不发起改变远端状态的请求"；任何破坏性副作用都应让只读标注为 `false`。
2. `破坏性=true` 表示**可能**产生不可逆副作用；具体是否拦截或要求确认由 policy 决定。
3. `bash` 标注为 `破坏性=true` 是**保守默认**：即使白名单内的命令（如 `ls`）实际只读，能力标注也不下放到具体命令；细粒度通过 `policy.Policy` 解析 `Input.command` 完成（见 policy 决策矩阵中的 command allowlist 规则）。
4. 上述字段名是参考约定，protocol 层不强制；`tools.ToolSpec.InputSchema` 才是单一真源。Policy 实现应基于实际 schema 解析 `ToolRequest.Input`。

## 5. Resolver

工具发现与解析由 `Resolver` 完成：

```go
type Resolver interface {
    List(ctx context.Context) ([]Tool, error)
    Resolve(ctx context.Context, name string) (Tool, error)
}
```

`List` 返回当前运行时可见工具集合；`Resolve` 按名称解析单个工具。解析失败应返回普通 Go error，由调用侧使用 `errors.Is` / `errors.As` 判断。静态工具通常直接通过根包 `WithTools` 注入；MCP、registry 等动态来源可以提供自己的 `Resolver` 并通过 `WithToolsResolver` 接入。

`ToolFilter`（见第 4 节）与 `Resolver` 组合即可表达"按 policy / 模型能力裁剪运行时可见工具"的诉求。

## 6. 控制流信号：sentinel error → TurnEnd.Action

`tools` 提供 **`ToolContext`** 暴露当前工具调用的运行时元数据；工具需要影响 Agent loop 控制流时，通过返回 sentinel error 完成。这两类 sentinel error 类型 `ErrLoopExit` / `ErrHandoff` 定义在 `tools` 包内（`tools/errors.go`），运行时使用 `errors.As` 识别。当前 v1 不发出独立 `event.LoopExit` / `event.Handoff` 输出帧，而是在本 turn 的 `event.TurnEnd.Action` 上聚合控制动作（参见 [design-event-agent-loop.md](design-event-agent-loop.md) §4.2）。

```go
// package tools (tools/context.go)

// ToolContext 暴露当前工具调用的运行时元数据，由默认 tool wave 在调用 Tool.Handle
// 之前注入到 ctx；不承载控制信号（控制流统一走 sentinel error）。
type ToolContext interface {
    ID() string       // 当前 tool call 的唯一 ID（与 event.ToolStart/ToolEnd.ID 同源）
    Spec() ToolSpec   // 当前工具的完整声明（Name 与 event.ToolStart/ToolEnd.Name 一致）
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
}

func (e *ErrHandoff) Error() string { return "tools: handoff to " + e.Agent }
```

翻译规则（由默认 tool wave 实施）：

- `ExitTool` 返回 `&ErrLoopExit{Escalate: req.Escalate}`：正常发出对应的 `event.ToolEnd`，本 turn 结束时输出 `event.TurnEnd{Action: event.LoopExit{Escalate: ...}}`；`flow.LoopAgent` 据此终止循环。
- Handoff 工具返回 `&ErrHandoff{Agent: name}`：正常发出对应的 `event.ToolEnd`，本 turn 结束时输出 `event.TurnEnd{Action: event.Handoff{Agent: name}}`；支持 handoff 的 flow agent 据此切换执行目标。

约束：

1. sentinel error 仅承载控制信号，不包含业务负载；如果模型仍需要工具结果文本，由默认 tool wave 在识别 sentinel 后合成默认成功 payload，写入 `ToolEnd.Parts` 与后续 tool-result message。
2. 控制信号集合受协议层管控：新增控制语义必须在本设计文档中显式定义新的 sentinel error 与对应的 `event.Action` 变体，再由默认 tool wave 和对应 flow agent 同时支持；不再使用 `map[string]any` + 字符串 key。
3. 工具实现通过 `tc, ok := tools.FromContext(ctx)` 获取当前 `ToolContext`（`ID` / `Spec`）；`ToolContext` 仅暴露调用元数据，不再承载 `Actions` / `SetAction` 等控制信号——控制流一律走 sentinel error 与 `TurnEnd.Action`。Resolver、allowed list 等 Agent 级运行时能力仍保留在 root/agent 层，不与 `tools.ToolContext` 混淆。
4. **控制信号不进入 `model.Message` 也不进入 `Session`**：协议层 `model.Message` 严格保持 protocol-only；运行时控制信号只出现在 `event.TurnEnd.Action` 上，由 flow 编排层读取。Hook 不承载这两类控制信号——应用如需观察，直接消费 `<-chan event.Output` 中的 `TurnEnd`（详见 [design-hook-extension.md](design-hook-extension.md)）。

## 7. 工具执行编排边界

`tools/` 不决定工具何时执行、是否并发、如何取消、如何把结果写回 provider 消息。当前 v1 的默认编排由根包 `llmAgent` 内部的 tool wave 完成；它不是公开可替换的 `ToolExecutor` 接口。需要完全改写编排语义时，应实现一个自定义 `blades.Agent`。

工具批次是否出现多个调用由模型/provider 决定，不由本地 `tools` 包配置。默认 Loop 对同一 assistant message 中的 tool wave 采用固定语义：`ToolStart` 按 assistant 源顺序发出，实际 `Handle` 并发执行，`ToolEnd` 按完成顺序发出，`content.ToolResult` 按 assistant 源顺序写回 Session。多个工具同时返回控制信号时，`TurnEnd.Action` 取 assistant 源顺序的第一个 action，保证确定性。

默认 tool wave 负责：

- 通过 Agent 持有的静态 `tools.Tool` 列表和可选 `tools.Resolver` 查找工具。
- 执行 policy 检查；工具暴露集合的裁剪由应用在 `WithTools` / `WithToolsResolver` 前完成，或通过 resolver 包装实现。
- 在调用 `Tool.Handle` 之前，通过 `tools.NewContext(ctx, tc)` 注入当前 `ToolContext`（携带本次 tool call 的 `ID` 与 `Spec`，其中 `Spec().Name` 与同源 `event.ToolStart`/`event.ToolEnd.Name` 字段对齐）。
- 调用 `Tool.Handle`。
- 识别 sentinel error（`ErrLoopExit` / `ErrHandoff`），记录到本 turn 的 `TurnEnd.Action`，并在需要时为模型 tool-message 合成默认成功 payload。
- 在 `Hook.AfterTool` 有机会改写 `Result.Parts` 后，将结果转换为 `event.ToolEnd`，并把同一份 parts 包装成 `content.ToolResult` 写回 provider 上下文；provider 原生多模态编码留给后续迭代。
- 把运行错误交回默认 `llmAgent`。

默认 `llmAgent` 再把 `ToolEnd.Parts` 对应的 `content.ToolResult` 写回 Session，继续后续模型调用。

## 8. 沙箱与隔离

默认 tool wave 是**编排职责**（事件 / sentinel / policy 集成 / Result 序列化），不是**隔离扩展点**。把 sandbox 做成自定义 Agent 会让"隔离"与"编排"耦合，并迫使实现重新承担 sentinel 识别、事件发射、`ToolContext` 注入等协议层职责。除非确实要重写编排语义，否则应在更轻的层次落 sandbox。

### 8.1 扩展点对比

| 扩展点 | 适用诉求 | 取舍 |
| --- | --- | --- |
| `policy.Policy` + `Modify` | 路径白名单、参数改写、`dry_run` 翻转、目标主机限定 | 协议内零侵入；无法做 OS 级隔离；只能改 `Input`，不能改 `Tool` |
| **Tool 装饰器**（实现 `tools.Tool` 包另一个 `Tool`） | 单工具的 OS 级隔离、远程代理执行、超时/资源配额包装、可观测性注入 | 复用默认 tool wave 编排；与 policy 解耦；每个工具单独包，`Spec` 需透传 |
| **Resolver 包装**（`tools.Resolver` 拦截解析） | 批量套壳：所有工具透明走沙箱代理；按命名空间路由本地 / 远程执行 | 对默认 tool wave 透明；统一注入便于运维；但所有工具被同一种沙箱包裹，灵活度低 |
| **工具实现内置** | 工具自身就是沙箱化的（如 `bash` 内嵌 firejail / nsjail；`fs` 工具走 chroot） | 隔离细节锁在工具里；不会被绕过；不能在不改工具的前提下切换沙箱实现 |
| **自定义 Agent** | 池化进程、跨工具事务、统一远端节点、自定义事件语义 | 唯一能改编排顺序与事件形态的位置；代价是要重做 sentinel / 事件 / policy 集成 |

经验法则：**先 policy / 装饰器 / Resolver；只有当编排本身需要变形时才实现自定义 Agent**。

### 8.2 按隔离层级的推荐落点

- **路径白名单 / 参数重写**：走 `policy.Policy`，对越界路径返回 `Modify`（重写到 workspace 根）或 `Deny`。能力标注 + Input 解析在同一个 `ToolRequest` 边界完成，符合 [design-policy-mode.md](design-policy-mode.md) 的两层裁决约束；对 `Tool` 本身无侵入。
- **远程沙箱（独立进程 / 容器 / 远端节点）**：推荐 **Tool 装饰器**或 **Resolver 包装**——`SandboxTool{ inner Tool }` 在 `Handle` 内把 `Spec().Name + input` 通过 RPC 发送到沙箱执行节点，回传 `*Result`；或 `SandboxResolver` 在 `Resolve` 时把每个 `Tool` 都包成代理。`Spec()` 仍由本地透传，模型可见的 schema 不变；默认 tool wave 仍负责事件 / sentinel / policy。
- **资源配额**：
  - **单次调用 wall-clock 超时 / CPU 时间 / 内存上限**：放在工具实现内（如 bash 通过子进程 `cgroup` / `ulimit`）或装饰器里基于 `ctx` deadline + 子进程隔离实现；这是执行细节，不进协议。
  - **调用次数 / 频率维度**：用 `policy.Budget` / `policy.RateLimit`（参考 [design-policy-mode.md](design-policy-mode.md) 中的内置工厂），命中后等价 `Deny`，不消费工具进程资源。

### 8.3 运行时商榷

不论选择哪种扩展点，跨进程或跨边界时需要满足：

1. **可序列化契约**：`tools.ToolSpec`（Name/Description/Schema）、`Handle` 输入 `json.RawMessage`、输出 `*Result`（`content.Part` 集合）、以及 sentinel error（`ErrLoopExit` / `ErrHandoff`）必须能跨进程传输。二进制内容使用 `content.DataPart` / `content.FilePart` / `content.FileRefPart` 对齐 v1 content 协议；sentinel 通过远端约定的 error code 还原为本地 sentinel 类型，再交给本地默认 tool wave 翻译成 `TurnEnd.Action`（参见 §6）。
2. **ctx 取消可达远端**：装饰器 / 远程 client 必须把 `ctx.Done()` 转发为 RPC 取消（如 gRPC cancellation）；超时由 ctx deadline 驱动，沙箱内的工具实现也必须读取 deadline 并提前停手。否则用户取消 / agent 上层超时无法生效。
3. **`ToolContext` 的可见性**：`tools.FromContext(ctx)` 返回的 `ID` / `Spec` 是本地运行时元数据；远端工具实现如果需要它（用于审计、可观测性），装饰器需要将这些字段以约定字段塞进 RPC payload，并在远端通过 `tools.NewContext` 重建。否则远端读不到。
4. **事件帧只在本地发**：`event.ToolStart` / `event.ToolEnd` 必须由**本地**默认 tool wave 产出；远端只产出 `Result` 与 error。装饰器不要私自往 Output 通道写事件，避免重复或顺序错乱。
5. **配额命中的语义**：单次超时建议在工具/装饰器内以 `context.DeadlineExceeded` 形式返回，由默认 tool wave 写入 `event.ToolEnd` 的 error 语义；调用次数 / 频率类配额命中应通过 `policy.Policy` 在 `Check` 阶段返回 `Deny`，**不进入** `Tool.Handle`。两条路径不要混用——前者消耗了执行资源，后者没有。
6. **policy 仍是工具调用的唯一裁决边界**：装饰器 / Resolver 包装可以增加副作用、隔离与序列化，但**不要**在装饰器内自行决定"允许 / 拒绝 / 询问"，否则会绕过 policy 的可观测性与可组合性。

### 8.4 何时确实需要自定义 Agent

只有当下列至少一项成立时才考虑：

- 需要把 Tool Wave 跨进程统一调度（如远端有自己的并发池、批量化协议）。
- 需要改变 sentinel error → `TurnEnd.Action` 的翻译规则（新增控制语义，且不能用 §6 的"新增 sentinel + `event.Action` 变体"扩展通道完成）。
- 需要在 policy 之外引入新的裁决边界（不推荐，绝大多数应该走 policy）。
- 需要把 `event.ToolEnd` 的多模态序列化策略整体替换（如改用 provider 原生多模态编码，覆盖 §7 的 v1 默认行为）。

否则应留用默认 `llmAgent` tool wave，把隔离推给装饰器 / Resolver / 工具实现 / policy。

## 与红线对照

本文覆盖 r4、r16、r17、r29。
