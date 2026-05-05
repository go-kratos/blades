---
type: design
title: Policy 与模式边界
date: 2026-05-05
status: draft
parent: design-agent-framework.md
related: [design-agent-framework.md]
tags: [agentos, policy, mode, safety]
---

# Policy 与模式边界

## 概述

`policy/` 提供 AgentOS core 的通用决策原语。v1 只有一个接口：`Policy.Check(ctx, req)`。工具审批、模型预算、资源访问、安全检查都映射成 sealed `Request` 后交给 Policy 判断。

Plan、Accept、Auto 等交互模式不是 core 内建类型。应用通过组合 Policy 工厂、prompt section、UI 审批和事件流实现具体产品体验。

## Policy 接口与 Decision

```go
package policy

type Policy interface {
    Check(ctx context.Context, req Request) Decision
}

type Action string

const (
    Allow  Action = "allow"
    Deny   Action = "deny"
    Modify Action = "modify"
)

type Decision struct {
    Action   Action
    Reason   string
    Modified *Request
}
```

语义：

- `Allow`：调用点可继续执行。
- `Deny`：调用点必须停止本次请求，并把 `Reason` 转换为可观测错误或事件。
- `Modify`：调用点应使用 `Modified` 指向的新请求继续；如果调用点不支持修改，必须按拒绝处理。

`Decision` 是结构体，便于后续增加审计字段、分类标签或预算信息，同时保持调用点只依赖 `Action` 主分支。

## Request sealed 三分类

```go
type Request interface {
    policyRequest()
}

type ToolRequest struct {
    Tool  tools.Tool
    Input json.RawMessage
}

type ModelRequest struct {
    Req *model.Request
}

type ResourceRequest struct {
    Kind     string
    Path     string
    Action   string
    Metadata map[string]any
    Input    json.RawMessage
}
```

`Request` 使用私有 marker，由 `policy/` 穷尽定义。这样所有 Policy 实现都能明确处理三类请求：

- `ToolRequest`：直接持有 `tools.Tool`，包含 schema、名称、描述、只读元信息等工具侧信息。
- `ModelRequest`：直接持有 `*model.Request`，可检查 system blocks、messages、tools、预算和 provider-neutral 参数。
- `ResourceRequest`：表达文件、网络、进程、artifact、memory 等资源操作。

依赖方向是单向的：`policy/` 可以依赖 `tools/`、`model/` 和事件相关协议；这些下层包不反向依赖 `policy/`。

## 内置工厂函数

内置组合均返回 `Policy`：

```go
func Chain(ps ...Policy) Policy

func Budget(limit BudgetLimit) Policy

func RateLimit(limiter Limiter) Policy

func SafetyCheck(fn SafetyFunc) Policy
```

### Chain

`Chain` 顺序执行多个 Policy。遇到 `Deny` 立即返回；遇到 `Modify` 时，后续 Policy 使用修改后的请求继续检查；全部允许则返回 `Allow`。

### Budget

`Budget` 检查模型请求、工具调用或资源操作的预算。预算来源可以是用户、session、项目、组织或运行时配置，但这些来源由应用注入。

### RateLimit

`RateLimit` 对请求频率做限制，适合模型调用、网络资源、昂贵工具或敏感资源。

### SafetyCheck

`SafetyCheck` 包装安全分类函数。分类器可以是本地规则、远程服务或模型调用；core 只关心最终 `Decision`。

## Policy 与协议层依赖关系

Policy 运行在 Agent Loop 的关键边界：

```go
// tool boundary
d := p.Check(ctx, policy.ToolRequest{Tool: tool, Input: raw})

// model boundary
d = p.Check(ctx, policy.ModelRequest{Req: req})

// resource boundary
d = p.Check(ctx, policy.ResourceRequest{
    Kind:   "file",
    Path:   "docs/design.md",
    Action: "write",
})
```

`model.Request` 保持 v1 形态：`System []*model.SystemBlock`、`Messages []*model.Message`、`Tools []model.ToolSpec`。Policy 可以检查这些对象，但不改变协议定义。

## 应用层交互模式

core 不内置 Plan、Accept、Auto 模式。应用可以这样组合：

- Plan：prompt section 要求先输出计划；Policy 拒绝写资源请求，只允许读与模型调用。
- Accept：Policy 对写资源或执行类工具返回拒绝或修改为审批请求；UI 获得用户确认后重新提交。
- Auto：组合预算、速率、安全和资源边界 Policy，在允许范围内自动执行。

这些模式需要 UI、配置、用户身份、workspace 和审计日志，属于应用层产品概念，不进入 `policy/` 类型系统。

## 设计决策

1. **单一 Check 接口**：所有决策点都用同一形态，Loop 集成简单。
2. **sealed Request**：请求联合由 core 穷尽，避免隐式扩展导致实现不完整。
3. **直接持有协议对象**：工具和模型请求携带完整元信息，Policy 不需要二次查表。
4. **模式外置**：交互模式差异很大，core 只提供可组合决策原语。

## 与红线对照

- r2：Policy 可依赖 tools、model、event 等协议层，保持单向无环。
- r23：`Policy.Check(ctx, req) Decision` 单一接口；`Chain`、`Budget`、`RateLimit`、`SafetyCheck` 均返回 `Policy`。
- r23：`Request` 为 sealed union，包含 `ToolRequest`、`ModelRequest`、`ResourceRequest`。
- r23：`Decision{Action, Reason, Modified}` 使用 `Allow`、`Deny`、`Modify` 三类动作。
