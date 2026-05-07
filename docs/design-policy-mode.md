---
type: design
title: Policy 与模式边界
date: 2026-05-07
revision: 3
status: draft
parent: design-agent-framework.md
related: [design-agent-framework.md, design-tool-system.md, design-event-agent-loop.md, design-hook-extension.md]
tags: [agentos, policy, mode, safety]
---

# Policy 与模式边界

## 概述

`policy/` 提供 AgentOS core 在 **工具调用边界** 上的通用决策原语。v1 设计目标：

- **聚焦工具裁决**：v1 Policy 只回答一个问题——"这次工具调用是否允许执行，是否需要改写或人工审批"。其它边界（模型请求改写、模型预算、限流）由 hook 与 middleware 承接，不进入 `policy/`。
- **零抽象税**：不引入 sealed union 或多变体 Request。`Policy.Check` 直接接受 `ToolRequest`，这是 v1 唯一的决策入口。
- **直传完整工具对象**：消除 Loop 与 Policy 之间的元数据映射样板，Policy 实现可直接读取 `tools.ReadOnlyTool` / `tools.DestructiveTool` 等可选能力。
- **Fail-closed**：错误（远端不可达、解析失败等）一律按 Deny 处理。
- **核心只提供决策原语**：Plan / Accept / Auto 等交互模式不进 core，由应用层基于 Policy + prompt section + UI + 事件流组合。

不在 v1 范围：模型请求 / 资源 / memory 等其它边界 Policy、审批 UI、审计存储、组织级策略目录、`Reason` 类型化、批量评估接口。如需扩展到模型侧，将作为后续版本的非兼容演进，而不是在 v1 提前抽象。

## Policy 接口与 Decision

```go
package policy

type Policy interface {
    // Check 必须并发安全；遵守 ctx 取消；error != nil 时调用点按 Deny 处理。
    Check(ctx context.Context, req ToolRequest) (Decision, error)
}

type ToolRequest struct {
    Tool  tools.Tool       // 直接持有工具，Policy 可读取 ReadOnlyTool / DestructiveTool 等可选能力
    Input json.RawMessage  // 模型生成的参数；Policy 可解析以做细粒度裁决（路径、动作、目标主机等）
}

type Action string

const (
    Allow  Action = "allow"
    Deny   Action = "deny"
    Ask    Action = "ask"
    Modify Action = "modify"
)

type Decision struct {
    Action   Action
    Reason   string         // 推荐 "<code>: <message>" 形式，便于日志检索
    Modified *ToolRequest   // 仅当 Action=Modify 时非 nil
    Metadata map[string]any // 审计/事件 payload；可选
}
```

语义：

- `Allow`：调用点继续执行原 `ToolRequest`。
- `Deny`：调用点中止本次工具调用，把 `Reason` 转为可观测错误或事件。
- `Ask`：调用点中止本次工具调用，发起人工审批；应用确认后用同一或修改后的请求重新进入 Loop。`Ask` 让 Accept 模式不必把审批伪装成 `Modify`。
- `Modify`：调用点应使用 `Modified` 继续；如果调用点不支持改写涉及的字段，必须按 `Deny` 处理（见"调用点契约"）。

错误：`Check` 返回 `error` 表示评估自身失败（远端服务异常、配置错误等）。Loop 必须按 fail-closed 处理：等价 `Deny`，并把 error 转事件以便观察。

`Decision.Metadata` 由 Policy 实现填充（如命中规则名、预算余量、限流剩余配额），由 hook 取出写入审计；core 不规定字段名。

## ToolRequest 调用点契约

`Modify` 仅允许改写 `Input`（参数改写，例如把目标路径重写到沙箱、把 `dry_run=false` 改为 `true`）。改写 `Tool` 或新增字段 → 调用点必须 Deny。

调用点在收到 `Modify` 后：

1. 检查 `Modified != nil` 且 `Modified.Tool` 与原请求一致；不一致 → Deny。
2. 用 `Modified.Input` 替换原 `Input`，继续执行。

这样 `Decision` 不需要承担能力协商的复杂度。

### 示例：fs 工具的细粒度资源裁决

```go
func fsPolicy(allowedRoots []string) policy.Policy {
    return policy.PolicyFunc(func(ctx context.Context, req policy.ToolRequest) (policy.Decision, error) {
        // 粗粒度：只读工具直接放行
        if ro, ok := req.Tool.(tools.ReadOnlyTool); ok && ro.ReadOnly() {
            return policy.Decision{Action: policy.Allow}, nil
        }
        // 细粒度：解析 Input 拿到路径/动作
        var args struct {
            Path   string `json:"path"`
            Action string `json:"action"`
        }
        if err := json.Unmarshal(req.Input, &args); err != nil {
            return policy.Decision{Action: policy.Deny, Reason: "invalid_input: " + err.Error()}, nil
        }
        if !underAnyRoot(args.Path, allowedRoots) {
            return policy.Decision{Action: policy.Ask, Reason: "out_of_workspace: " + args.Path}, nil
        }
        return policy.Decision{Action: policy.Allow}, nil
    })
}
```

要点：能力标注做粗筛，Input 解析做细筛；两层在同一个 `ToolRequest` 边界完成。

## 内置工厂函数

内置组合均返回 `Policy`：

```go
func Chain(ps ...Policy) Policy

func Budget(limit BudgetLimit, key func(context.Context, ToolRequest) string) Policy

func RateLimit(limiter Limiter, key func(context.Context, ToolRequest) string) Policy

func SafetyCheck(fn SafetyFunc) Policy
```

### Chain

`Chain` 顺序执行多个 Policy：

- 任一返回 `Deny` 或 `Ask` → 立即短路返回。
- 返回 `Modify` → 用 `Modified` 替换当前请求，后续 Policy 看到修改后请求继续检查；最终 `Decision.Modified` 是最后一次 `Modify` 的结果。
- 全部 `Allow` → 返回 `Allow`。
- 任一返回 `error` → 立即短路返回 error，Loop 按 fail-closed 处理。

### Budget / RateLimit

两者都是有状态 Policy，作用域由 `key` 函数决定：

```go
key := func(ctx context.Context, req policy.ToolRequest) string {
    if s, ok := session.FromContext(ctx); ok {
        return "session:" + s.ID() + ":" + req.Tool.Spec().Name
    }
    return "global:" + req.Tool.Spec().Name
}
```

key 抽取通常基于 `session.FromContext` / `agent.FromContext` 等 ctx helper，由应用决定 scope（用户 / session / 项目 / 组织 / 工具粒度）。core 不规定 key 命名，也不内置存储后端：`BudgetLimit` 与 `Limiter` 是接口，由应用注入实现。

> 注意：v1 Policy 不裁决模型调用，所以 **模型 token / 调用次数 / 速率** 不属于这里的 Budget / RateLimit。模型侧的同类需求请走 hook 或 middleware。

### SafetyCheck

`SafetyCheck` 包装安全分类函数。分类器可以是本地规则、远程服务或模型调用；远程实现应当读取 ctx 截止时间并支持取消，否则用 `error` 让 Loop 走 fail-closed 路径。

## 并发与幂等契约

- `Policy.Check` 必须并发安全：默认 `llmAgent` 的 Tool Wave 会并行调用工具，每个工具一次 Policy。
- 实现应避免长阻塞，遵守 `ctx` 取消；超时返回 error，由 Loop 兜底为 Deny。
- `Modify` 应当是语义改写而不是带副作用的动作（不要在 `Check` 内写库、修改 session）。审计/计数等副作用可以做，但要保证幂等或允许重复（Chain 可能重复评估上游修改后的请求）。
- 有状态 Policy（Budget/RateLimit）记账应在 `Allow` 路径下进行；`Deny/Ask/error` 不应消耗配额。

## 审计与可观测性

core 不内置审计存储；可观测性走两条通路：

1. **Hook**：`PreToolCall / PostToolCall` 钩子在 Policy 调用前后观察 `Decision`，把 `Action / Reason / Metadata` 写入应用日志或 trace（参考 `design-hook-extension.md`）。
2. **Event**：`Deny / Ask` 通过 `event.Prompt` 或应用自定义 channel 回流到外部界面，应用决定是否重试、走人工审批或终止 Run。

`Decision.Metadata` 是 Policy 实现与审计层之间的可选 payload 通道（命中规则、预算余量、限流剩余配额、远端 trace id 等）；core 不规定字段名，避免锁死实现。

## 与其它模块的集成

### Loop 边界顺序

```
Run
└── Turn
    └── Step
        ├── PreModelCall hook    ← 模型请求改写在此发生（注入 system、裁剪 tools、调整 sampling）
        ├── model.Generate / Stream
        └── Tool Wave (并行)
            ├── PreToolCall hook
            ├── policy.Check(ToolRequest{Tool, Input}) ← v1 唯一 Policy 边界
            ├── tool.Handle
            └── PostToolCall hook
```

### 与 tools

- `tools.ToolFilter`（见 `design-tool-system.md`）决定 **哪些工具暴露给模型**（spec 裁剪，影响请求体）；`policy/` 决定 **运行时是否允许执行**。组合方式：先 Filter（构造请求时裁剪），再 Policy（执行前裁决）。
- Policy 实现读取 `tools.ReadOnlyTool / DestructiveTool` 做粗粒度分流，再解析 `ToolRequest.Input` 做细粒度裁决。
- 副作用全部走 tool：v1 不引入资源边界 Policy；非 tool 路径的副作用（应用级 scheduler、background workflow 等）属于应用层，不进 core。

### 与 hook（替代模型边界 Policy）

- 模型请求的运行时改写（注入 system block、裁剪 `Tools`、调整 sampling 等）由 `PreModelCall` hook Mutator 承担，而非 Policy。
- 模型调用的观察（耗时、token 用量、错误）也走 hook，不在 Policy 类型系统里。
- Hook 与 Policy 关注点互补：Hook 观察/扩展、可改写模型请求；Policy 裁决工具调用。

### 与 middleware（替代模型 Budget / RateLimit）

- 输入/输出 middleware（`middleware/`）作用于 channel 级输入加工与跨 Run 公共能力。
- 模型层面的 token 预算、QPS 限制、provider 重试退避等放在 middleware 或 provider 适配层，不进 policy/。

### 与 event

- `Deny / Ask` 不直接产出事件；由调用点（Loop 或应用）将其转为 `event.Prompt`、`event.Steer` 或应用 channel。
- 这样保持 `policy/` 不依赖 `event/` 的具体回流路径，只暴露决策结构。

### 与 prompt

- Plan 模式所需的 "先输出计划再执行" 由 prompt section 表达；Policy 只负责约束（如禁止破坏性工具）。
- prompt 与 Policy 解耦：prompt 改变模型行为预期，Policy 兜底真实工具边界。

### 与 session

- Policy 通常不直接操作 session；需要时通过 `session.FromContext` 读取 session id / metadata 作为 Budget/RateLimit key。
- core 不规定哪些 ctx capability 必须存在；缺失时 key extractor 退化到 `global` 即可。

### 与 memory

- `memory.Recall / Remember`（见 `design-memory.md`）不进入 Policy 边界：memory 是应用注入到 prompt 构造路径的能力，不是模型可调用的副作用入口；可见性与写入策略由 memory 实现自管。
- 如果应用希望让模型主动调用 memory（例如 `recall_memory` 工具），就把 memory 包装成 tool，自然落入 `ToolRequest` 边界。

## 应用层交互模式

core 不内置 Plan、Accept、Auto 模式。应用通过组合实现：

- **Plan**：prompt section 要求先输出计划；Policy 对破坏性工具返回 `Deny`，只允许只读工具执行。
- **Accept**：Policy 对破坏性工具或 Input 命中敏感规则的调用返回 `Ask`；UI 拿到 `Ask` 事件让用户确认，确认后重新提交（必要时附加 `Metadata` 表示已批准）。
- **Auto**：组合 Budget / RateLimit / SafetyCheck / 工具能力裁决 Policy，在允许范围内自动执行；预算用尽 → `Deny`；触达限流 → `Deny` 或 `Ask`。

## 设计决策

1. **v1 Policy 仅工具裁决**：唯一边界是 `ToolRequest`，不引入 `Request` sealed union 与 `ModelRequest`，模型侧改写交给 hook，模型预算/限流交给 middleware。带来的好处：核心类型最小，工具是模型唯一的副作用接触面，能力标注 + Input 解析已足够。
2. **单一 Check 接口**：所有工具决策点都用同一形态，Loop 集成简单。
3. **直接持有工具对象**：`ToolRequest` 直传 `tools.Tool`，Policy 不需要二次查表，可直接读取可选能力接口。
4. **Tool 是副作用的唯一边界**：文件、网络、命令执行等资源裁决统一在 `ToolRequest` 内完成（能力标注 + Input 解析），保持核心类型最小、避免工具实现反向感知 policy。
5. **模式外置**：交互模式差异大，core 只提供可组合决策原语。
6. **Fail-closed 错误语义**：`Check` 返回 `(Decision, error)`，error → 调用点等价 Deny；远端依赖失败永不静默放行。
7. **能力协商隐式化**：`Modify` 只允许改 `Input`；不在 `Decision` 上加显式协商字段，保持类型简洁。
8. **Metadata 而非强类型 Reason**：v1 不为 `Reason` 引入 `{Code, Message}` 结构；审计 payload 走 `Decision.Metadata`，按需扩展。

## 与红线对照

- r2：`policy/` 单向依赖 `tools/`，与 `model/event/content` 无依赖关系（v1 不需要），保持单向无环。
- r23：`Policy.Check(ctx, ToolRequest) (Decision, error)` 是单一接口；`Chain`、`Budget`、`RateLimit`、`SafetyCheck` 均返回 `Policy`。
- r23：v1 不再使用 sealed union；唯一请求结构是 `ToolRequest`。如未来扩展到模型/资源等其它边界，将作为非兼容版本演进。
- r23：`Decision{Action, Reason, Modified, Metadata}` 使用 `Allow / Deny / Ask / Modify` 四类动作。

> 说明：`design-agent-framework.md` 当前仍提到 `policy.Request` sealed 与 `ModelRequest / ResourceRequest`；该处需要在后续同步修订（不在本轮范围）。
