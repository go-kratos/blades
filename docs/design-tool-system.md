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

## 4.1 内置工具能力标注（参考）

`tools` 协议本身不规定哪些工具必须存在；以下能力矩阵是参考实现层面的约定，列出一组高频内置工具在 `ReadOnlyTool` / `DestructiveTool` 两个能力接口上的取值，以及 Input schema 中影响 policy 细粒度裁决的关键字段。运行时是否允许执行由 `policy.Policy` 在 `ToolRequest` 边界裁决（见 [design-policy-mode.md §内置工具决策矩阵](design-policy-mode.md#内置工具决策矩阵)）。

| 工具    | `ReadOnly()` | `Destructive()` | Input 关键字段                       | 备注                                                   |
| ------- | ------------ | --------------- | ------------------------------------ | ------------------------------------------------------ |
| `ls`    | true         | false           | `path`                                | 列目录；不修改文件系统。                              |
| `find`  | true         | false           | `path`, `pattern`, `type`            | 按名称/类型搜索；只读遍历。                            |
| `grep`  | true         | false           | `path`, `pattern`, `glob`            | 按内容搜索；只读读取文件。                             |
| `read`  | true         | false           | `path`, `range`                      | 读文件内容；不修改。                                  |
| `write` | false        | true            | `path`, `content`                    | 整文件覆盖写入；可能损毁既有内容。                    |
| `edit`  | false        | true            | `path`, `old_str`, `new_str`         | 增量改写；范围有限但仍属破坏性副作用。                |
| `bash`  | false        | true            | `command`, `args`, `cwd`             | 通用命令执行；语义随命令名而变（详见 policy 决策矩阵）。|

约束：

1. `ReadOnlyTool.ReadOnly() == true` 必须严格满足"不写文件系统、不发起改变远端状态的请求"；任何破坏性副作用都应让 `ReadOnly` 返回 `false`。
2. `DestructiveTool.Destructive() == true` 表示**可能**产生不可逆副作用；具体是否拦截或要求确认由 policy 决定。
3. `bash` 在能力接口上声明为 `Destructive=true` 是**保守默认**：即使白名单内的命令（如 `ls`）实际只读，能力标注也不下放到具体命令；细粒度通过 `policy.Policy` 解析 `Input.command` 完成（见 policy 决策矩阵中的 command allowlist 规则）。
4. 上述字段名是参考约定，protocol 层不强制；`tools.ToolSpec.InputSchema` 才是单一真源。Policy 实现应基于实际 schema 解析 `ToolRequest.Input`。

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
    ID() string       // 当前 tool call 的唯一 ID（与 event.ToolStart/ToolEnd.ToolID 同源）
    Spec() ToolSpec   // 当前工具的完整声明（Name 与 event.ToolStart/ToolEnd.ToolName 一致）
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
3. 工具实现通过 `tc, ok := tools.FromContext(ctx)` 获取当前 `ToolContext`（`ID` / `Spec`）；`ToolContext` 仅暴露调用元数据，不再承载 `Actions` / `SetAction` 等控制信号——控制流一律走 sentinel error 与 sealed Output 帧。Resolver、allowed list 等 Agent 级运行时能力仍保留在 root/agent 层，通过 `Invocation` 或 `agent.FromContext` 暴露，不与 `tools.ToolContext` 混淆。
4. **控制信号不进入 `model.Message` 也不进入 `Session`**：协议层 `model.Message` 严格保持 protocol-only；运行时控制信号仅以 `event.LoopExit` / `event.Handoff` 帧出现在 Output 流上，由 flow 编排层读取。Hook 不再承载这两类控制信号——应用如需观察，直接消费 `<-chan event.Output`（详见 [design-hook-extension.md](design-hook-extension.md)）。

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
- 在调用 `Tool.Handle` 之前，通过 `tools.NewContext(ctx, tc)` 注入当前 `ToolContext`（携带本次 tool call 的 `ID` 与 `Spec`，其中 `Spec().Name` 与同源 `event.ToolStart`/`event.ToolEnd.ToolName` 字段对齐）。
- 调用 `Tool.Handle`；当工具同时实现 `StreamingTool` 时优先使用 `Stream`，按增量语义累积 Parts。
- 识别 sentinel error（`ErrLoopExit` / `ErrHandoff`），在 `event.ToolEnd` 之后紧跟发出 `event.LoopExit` / `event.Handoff` 控制帧，并在需要时为模型 tool-message 合成默认成功 payload。
- 将结果转换为 `event.ToolEnd`，把多模态 `Result.Parts` 序列化进 provider 期望的 tool-message 内容（v1：纯文本拼接；`content.Blob` 以 `{"mime":..,"data_base64":..}` JSON 信封内嵌；provider 原生多模态编码留给后续迭代）。
- 把运行错误交回默认 `llmAgent`。

默认 `llmAgent` 再把 `event.ToolEnd.Result.Parts` 包装为 `content.ToolResult`，继续后续模型调用。

## 8. 沙箱与隔离

`ToolExecutor` 是**编排扩展点**（事件 / sentinel / policy 集成 / Result 序列化），不是**隔离扩展点**。把 sandbox 直接写成自定义 `ToolExecutor` 会让"隔离"与"编排"耦合，并迫使实现重新承担 sentinel 识别、事件帧发射、`ToolContext` 注入等协议层职责。除非确实要重写编排语义，否则应在更轻的层次落 sandbox。

### 8.1 扩展点对比

| 扩展点 | 适用诉求 | 取舍 |
| --- | --- | --- |
| `policy.Policy` + `Modify` | 路径白名单、参数改写、`dry_run` 翻转、目标主机限定 | 协议内零侵入；无法做 OS 级隔离；只能改 `Input`，不能改 `Tool` |
| **Tool 装饰器**（实现 `tools.Tool` 包另一个 `Tool`） | 单工具的 OS 级隔离、远程代理执行、超时/资源配额包装、可观测性注入 | 复用默认 `ToolExecutor` 编排；与 policy 解耦；每个工具单独包，`Spec` 需透传 |
| **Resolver 包装**（`tools.Resolver` 拦截解析） | 批量套壳：所有工具透明走沙箱代理；按命名空间路由本地 / 远程执行 | 对 `ToolExecutor` 完全透明；统一注入便于运维；但所有工具被同一种沙箱包裹，灵活度低 |
| **工具实现内置** | 工具自身就是沙箱化的（如 `bash` 内嵌 firejail / nsjail；`fs` 工具走 chroot） | 隔离细节锁在工具里；不会被绕过；不能在不改工具的前提下切换沙箱实现 |
| **替换 `ToolExecutor`** | 池化进程、跨工具事务、统一远端节点、自定义事件语义 | 唯一能改编排顺序与事件形态的位置；代价是要重做 sentinel / 事件 / policy 集成 |

经验法则：**先 policy / 装饰器 / Resolver；只有当编排本身需要变形时才换 `ToolExecutor`**。

### 8.2 按隔离层级的推荐落点

- **路径白名单 / 参数重写**：走 `policy.Policy`，对越界路径返回 `Modify`（重写到 workspace 根）或 `Deny`。能力标注 + Input 解析在同一个 `ToolRequest` 边界完成，符合 [design-policy-mode.md](design-policy-mode.md) 的两层裁决约束；对 `Tool` 本身无侵入。
- **远程沙箱（独立进程 / 容器 / 远端节点）**：推荐 **Tool 装饰器**或 **Resolver 包装**——`SandboxTool{ inner Tool }` 在 `Handle` 内把 `Spec().Name + input` 通过 RPC 发送到沙箱执行节点，回传 `*Result`；或 `SandboxResolver` 在 `Resolve` 时把每个 `Tool` 都包成代理。`Spec()` 仍由本地透传，模型可见的 schema 不变；默认 `ToolExecutor` 仍负责事件 / sentinel / policy。
- **资源配额**：
  - **单次调用 wall-clock 超时 / CPU 时间 / 内存上限**：放在工具实现内（如 bash 通过子进程 `cgroup` / `ulimit`）或装饰器里基于 `ctx` deadline + 子进程隔离实现；这是执行细节，不进协议。
  - **调用次数 / 频率维度**：用 `policy.Budget` / `policy.RateLimit`（参考 [design-policy-mode.md](design-policy-mode.md) 中的内置工厂），命中后等价 `Deny`，不消费工具进程资源。

### 8.3 运行时商榷

不论选择哪种扩展点，跨进程或跨边界时需要满足：

1. **可序列化契约**：`tools.ToolSpec`（Name/Description/Schema）、`Handle` 输入 `json.RawMessage`、输出 `*Result`（`content.Part` 集合）、以及 sentinel error（`ErrLoopExit` / `ErrHandoff`）必须能跨进程传输。`content.Blob` 沿用 v1 信封 `{"mime":..,"data_base64":..}`；sentinel 通过远端约定的 error code 还原为本地 sentinel 类型，再交给本地 `ToolExecutor` 翻译成 `event.LoopExit` / `event.Handoff`（参见 §6）。
2. **ctx 取消可达远端**：装饰器 / 远程 client 必须把 `ctx.Done()` 转发为 RPC 取消（如 gRPC cancellation）；超时由 ctx deadline 驱动，沙箱内的工具实现也必须读取 deadline 并提前停手。否则用户取消 / agent 上层超时无法生效。
3. **`ToolContext` 的可见性**：`tools.FromContext(ctx)` 返回的 `ID` / `Spec` 是本地运行时元数据；远端工具实现如果需要它（用于审计、可观测性），装饰器需要将这些字段以约定字段塞进 RPC payload，并在远端通过 `tools.NewContext` 重建。否则远端读不到。
4. **事件帧只在本地发**：`event.ToolStart` / `event.ToolEnd` / `event.LoopExit` / `event.Handoff` 必须由**本地** `ToolExecutor` 产出；远端只产出 `Result` 与 error。装饰器不要私自往 Output 通道写事件，避免重复或顺序错乱。
5. **配额命中的语义**：单次超时建议在工具/装饰器内以 `context.DeadlineExceeded` 形式返回，由 `ToolExecutor` 写入 `event.ToolEnd` 的 error 通道；调用次数 / 频率类配额命中应通过 `policy.Policy` 在 `Check` 阶段返回 `Deny`，**不进入** `Tool.Handle`。两条路径不要混用——前者消耗了执行资源，后者没有。
6. **policy 仍是工具调用的唯一裁决边界**：装饰器 / Resolver 包装可以增加副作用、隔离与序列化，但**不要**在装饰器内自行决定"允许 / 拒绝 / 询问"，否则会绕过 policy 的可观测性与可组合性。

### 8.4 何时确实需要替换 `ToolExecutor`

只有当下列至少一项成立时才考虑：

- 需要把 Tool Wave 跨进程统一调度（如远端有自己的并发池、批量化协议）。
- 需要改变 sentinel error → 事件帧的翻译规则（新增控制语义，且不能用 §6 的"新增 sentinel + sealed Output 变体"扩展通道完成）。
- 需要在 policy 之外引入新的裁决边界（不推荐，绝大多数应该走 policy）。
- 需要把 `event.ToolEnd` 的多模态序列化策略整体替换（如改用 provider 原生多模态编码，覆盖 §7 的 v1 默认行为）。

否则应留用默认 `ToolExecutor`，把隔离推给装饰器 / Resolver / 工具实现 / policy。

## 与红线对照

本文覆盖 r4、r16、r17、r29。
