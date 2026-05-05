---
type: design
title: Policy 与交互模式边界
parent: design-agent-framework.md
date: 2026-05-01
status: draft
modules: [module-6, module-14]
---

# Policy 与交互模式边界

本文描述 AgentOS core 的 `policy/` 包，以及 Plan Mode、Accept Edits、Auto Mode 等交互模式与 core 的边界。

核心结论：`policy/` 只提供通用决策原语，不内置完整产品交互模式，并且包内只依赖 Go 标准库。计划文件、审批工具、workspace 写入边界、AI 分类器、工具元数据分类和模式提示词由具体应用、examples 或 contrib 包实现。

## Policy Core

### 现状对比

| 维度 | 当前 Blades | AgentOS Core |
|------|-------------|--------------|
| 权限控制 | `Confirm` 中间件 | 通用 `policy.Chain` |
| 决策结果 | 用户确认或拒绝 | allow / deny / ask / modify / passthrough |
| 安全检查 | 无统一抽象 | bypass-immune `SafetyChecker` |
| 规则来源 | 无统一抽象 | CLI / session / project / user / org 等来源由应用注入 |
| 交互模式 | 无 | core 不内置完整模式，只暴露 primitives |

### 决策类型

```go
package policy

type DecisionKind string

const (
    Allow       DecisionKind = "allow"
    Deny        DecisionKind = "deny"
    Ask         DecisionKind = "ask"
    Modify      DecisionKind = "modify"
    Passthrough DecisionKind = "passthrough"
)

type Decision struct {
    Kind   DecisionKind
    Reason string
    Patch  any // 可选修改结果，由具体 Request 类型解释
}
```

`Modify` 用于 hook、policy 或应用层规则改写工具输入、模型请求或资源操作；如果调用点不支持修改，必须把 `Modify` 当成 `Deny` 或错误处理，不能静默忽略。

### 决策请求

Core 定义少量稳定请求类型。`Request` 使用非导出 marker method 封闭在 `policy/` 包内，避免应用随意扩展 request union 后让 checker 之间产生隐式协议。应用如果要接入额外资源类型，应映射为 `ResourceRequest`，或在应用自己的 `policy.Checker` 中解释这些 core request。

```go
package policy

type Request interface{ policyRequest() }

type ToolRequest struct {
    ToolName string
    Input    json.RawMessage
}

type ModelRequest struct {
    Model string
    Usage *BudgetUsage
}

type ResourceRequest struct {
    Kind   string // file, network, process, memory, artifact...
    Action string // read, write, delete, execute...
    Target string
    Input  json.RawMessage
}
```

`ToolRequest` 只包含工具名和 JSON 输入，不携带 `tools.Tool`、schema、readonly metadata 或执行上下文。只读工具分类、schema 解释、MCP 元数据和应用资源边界都在 Agent Loop 或应用 bridge 中完成，再转换成 `ToolRequest` / `ResourceRequest` 交给 `policy.Chain`。

### 规则与来源

```go
type Rule struct {
    Source   RuleSource
    Behavior DecisionKind // allow / deny / ask / passthrough
    Resource string       // tool name, resource kind, model name, or glob
    Pattern  string       // optional input/target matcher
    Reason   string
}

type RuleSource string

const (
    SourceCLI     RuleSource = "cli"
    SourceSession RuleSource = "session"
    SourceProject RuleSource = "project"
    SourceUser    RuleSource = "user"
    SourceOrg     RuleSource = "org"
)
```

Core 不读取配置文件，也不规定 `.blades/config.json`、`~/.blades/config.json` 等路径。应用负责加载配置并构造 `Rule`。

### Chain

```go
type Checker interface {
    Check(ctx context.Context, req Request) (Decision, error)
}

type Chain struct {
    checkers []Checker
}

func NewChain(checkers ...Checker) *Chain

func (c *Chain) Check(ctx context.Context, req Request) (Decision, error)
```

推荐顺序：

1. SafetyChecker：不可绕过的安全不变量。
2. RuleChecker：用户、项目、组织和 session 规则。
3. BudgetPolicy：token、成本、时间和并发预算。
4. RateLimiter：请求频率和外部资源限流。
5. DefaultDecision：没有命中规则时返回 `Ask` 或应用指定默认值。

Hook 不嵌入 `policy.Chain`。Agent Loop 在调用 policy 前后触发 hook，以避免 `policy/` 反向依赖 `hook/`。同理，`policy/` 不导入 `tools/`、`model/`、`event/`、`blades/` 或 `hook/`。

## Optional Modes

Plan Mode、Accept Edits 和 Auto Mode 是应用交互策略，不是 core API。它们可以放在：

- `examples/coding/`：展示 coding app 如何组合只读探索、计划审批和实现。
- `contrib/policy/mode` 或 `contrib/planmode`：提供可选 reusable implementation。
- 具体应用的 `cmd/<app>/internal/mode`：和产品 UI、workspace、配置系统深度集成。

### 最小对接面

应用层模式实现只依赖 core primitives：

```go
type Mode string

type ModeState struct {
    Current Mode
}

type ModeController interface {
    Current() Mode
    Transition(ctx context.Context, to Mode, source string) error
}
```

Core 不定义全局 `policy.Mode` 枚举，避免把 coding app 的交互语义固定进通用 AgentOS。应用可以用自己的 mode 值，并把 mode 状态通过自定义 checker、tool filter、prompt section 和 hook 组合到 Agent Loop。这里的 tool filter 与 prompt section 属于应用或其它 core 包，不属于 `policy/` 包依赖。

### Plan Mode 推荐实现位置

Plan Mode 的完整生命周期包含只读工具过滤、计划文件、审批 UI、预授权规则和模式恢复。这些能力依赖 workspace、channel 和用户交互，因此放在应用层：

```go
package planmode

func Tools(controller ModeController, opts ...Option) []tools.Tool
func PromptSection(controller ModeController, opts ...Option) prompt.Section
func ToolFilter(controller ModeController) tools.Filter
func PolicyChecker(controller ModeController) policy.Checker
```

实现约束：

- 计划文件目录由应用配置，不由 core 规定。
- 审批 UI 由 channel/app 提供，不由 core 直接提示用户。
- 预授权动作转换为 session 规则时，只产生 `policy.Rule`，不修改 core chain 结构。
- 只读判定基于工具 metadata、MCP annotation 或应用自己的工具分类；判定逻辑在应用 bridge 中，`policy/` 只看到 `ToolRequest` / `ResourceRequest`。

### Accept Edits 推荐实现位置

Accept Edits 依赖 workspace 边界和文件工具语义。它应由应用提供 checker：

```go
type AcceptEditsChecker struct {
    WorkspaceRoot string
}
```

checker 只对明确位于 workspace 内的文件写入返回 `Allow`；Bash、MCP、网络和跨 workspace 操作继续走默认 policy。

### Auto Mode 推荐实现位置

Auto Mode 依赖 AI 分类器、失败熔断和用户信任模型。Core 不提供内置 classifier，只要求应用把分类结果转换成 `policy.Decision`。

```go
type Classifier interface {
    Classify(ctx context.Context, req policy.Request) (policy.Decision, error)
}
```

连续拒绝熔断、降级到 default、日志和用户通知由应用 mode controller 处理。

## Agent Loop 集成

Agent Loop 对 policy 的职责保持稳定：

- 在工具执行前构造 `policy.ToolRequest` 并调用 `Chain.Check`。
- 在模型调用前可选构造 `policy.ModelRequest` 做预算和模型限制检查。
- 对 `Allow` 执行原操作，对 `Deny` 返回可恢复错误，对 `Ask` 交给应用提供的 confirmation handler，对 `Modify` 只在调用点明确支持时应用修改。
- 在 policy 前后触发 hook，但不让 `policy/` 依赖 `hook/`。

## 关键设计决策

1. **Policy core 不等于交互模式**：core 只负责可组合决策；Plan Mode、Accept Edits、Auto Mode 是产品策略。
2. **不内置全局 Mode 枚举**：避免把 coding assistant 的状态机固定为通用 AgentOS API。
3. **Hook 编排在 Agent Loop**：消除 `policy/` 与 `hook/` 的循环依赖风险。
4. **应用加载规则**：core 不读取配置路径，只消费已构造的 `policy.Rule` 和 `policy.Checker`。
5. **SafetyChecker 不可绕过**：任何 mode 或 rule 都不能覆盖 hard deny 的安全不变量。
