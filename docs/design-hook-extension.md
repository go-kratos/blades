---
type: design
title: 扩展与 Hook 系统
parent: design-agent-framework.md
date: 2026-05-01
status: draft
modules: [module-4]
---

# 扩展与 Hook 系统

Hook 是 AgentOS core 的生命周期观察与拦截机制。Core 只内置 Agent、Model、Tool、Session、Policy、Compact 相关事件；配置、文件系统、任务、channel、notification 等产品事件由应用层自己的事件总线承接，不进入 core 事件集合。

## 设计结论

- `hook/` 可以依赖 `event/`、`model/`、`tools/`、`policy/`，但 `policy/` 不依赖 `hook/`。
- Agent Loop 负责在生命周期边界发射 hook，并把拦截结果传回对应调用点。
- 观察型 hook 和拦截型 hook 分开建模，避免一个大联合返回值覆盖所有事件。
- `hook.Event` 使用 core-sealed 判别联合。应用层事件不能复用 core `hook.Registry`，应使用应用自己的 bus 或 registry。
- Model hook 只观察 request/response snapshot，不接触 raw `*model.Request`，也不能注入或修改 `model.Message`。

## Core Events

```go
package hook

type Event interface{ hookEvent() }

// Agent lifecycle.
type AgentStart struct {
    AgentName string
    SessionID string
}

type AgentEnd struct {
    AgentName string
    SessionID string
    Err       error
}

type TurnStart struct {
    AgentName string
    Turn      int
}

type TurnEnd struct {
    AgentName string
    Turn      int
    Reason    string
    Usage     *model.TokenUsage
}

// Model lifecycle.
type BeforeModelRequest struct {
    Request model.RequestSnapshot
}

type AfterModelResponse struct {
    Response model.ResponseSnapshot
    Err      error
}

// Tool lifecycle.
type PreToolUse struct {
    ToolName string
    Input    json.RawMessage
}

type PostToolUse struct {
    ToolName string
    Result   *tools.Result
    Err      error
}

// Policy lifecycle.
type PolicyRequest struct {
    Request policy.Request
}

type PolicyDecision struct {
    Request  policy.Request
    Decision policy.Decision
    Err      error
}

// Session lifecycle.
type SessionStart struct {
    SessionID string
    IsResume  bool
}

type SessionEnd struct {
    SessionID string
}

// Compact lifecycle.
type PreCompact struct {
    Messages   []*model.Message
    TokenCount int64
}

type PostCompact struct {
    Messages     []*model.Message
    TokensBefore int64
    TokensAfter  int64
    Err          error
}
```

不进入 core 的事件包括 `ConfigChange`、`CwdChanged`、`FileChanged`、`TaskCreated`、`TaskCompleted` 和 channel notification。应用可以定义自己的事件类型：

```go
package apphook

type FileChanged struct {
    Path   string
    Action string
}
```

这些应用事件通过应用自己的 bus 发射。Core `hook.Registry` 只接受实现了非导出 `hookEvent()` 的 core events，从类型层面防止应用事件绕过核心边界进入 Agent Loop。

## Registry

```go
type ObserveHandler[E Event] func(ctx context.Context, event E) error

type Registry struct {
    mu       sync.RWMutex
    handlers map[reflect.Type][]entry
}

func NewRegistry(opts ...Option) *Registry

func Observe[E Event](r *Registry, handler ObserveHandler[E], opts ...Option)

func (r *Registry) Emit(ctx context.Context, event Event) error

type Option func(*entry)

func WithPriority(priority int) Option
func WithScope(scope string) Option
func WithTimeout(d time.Duration) Option
func WithSession(sessionID string) Option
func (r *Registry) ClearSessionHooks(sessionID string)
```

观察型 hook 返回 error 时，Agent Loop 记录错误并继续执行，除非发射点显式声明该 error 会中止当前操作。

## Interceptors

少数生命周期点需要拦截能力，使用专用 handler 和专用结果类型。

```go
type PreToolUseHandler func(ctx context.Context, event *PreToolUse) (*PreToolUseResult, error)

type PreToolUseResult struct {
    Block         bool
    Reason        string
    ModifiedInput json.RawMessage
}

type PostToolUseHandler func(ctx context.Context, event *PostToolUse) (*PostToolUseResult, error)

type PostToolUseResult struct {
    ModifiedResult *tools.Result
}

type BeforeModelHandler func(ctx context.Context, event *BeforeModelRequest) (*BeforeModelResult, error)

type BeforeModelResult struct {
    Continue   bool
    StopReason string
}

type StopHandler func(ctx context.Context, event *TurnEnd) (*StopResult, error)

type StopResult struct {
    ContinueLoop bool
    FollowUp     []event.InputPart
}

func (r *Registry) OnPreToolUse(handler PreToolUseHandler, opts ...Option)
func (r *Registry) OnPostToolUse(handler PostToolUseHandler, opts ...Option)
func (r *Registry) OnBeforeModel(handler BeforeModelHandler, opts ...Option)
func (r *Registry) OnStop(handler StopHandler, opts ...Option)
```

Policy override 不放在 `PreToolUseResult` 中。权限决策由 Agent Loop 调用 `policy.Chain`，并在 policy 前后发射 `PolicyRequest` / `PolicyDecision` 事件。需要改变权限行为时，应通过 `policy.Checker` 或应用层 policy wrapper 实现。

Model interceptor 只允许继续或中止本次调用。它不能追加 system message、改写 user/assistant message、替换工具声明、修改 raw request 或注入 provider 参数。需要影响模型上下文时，应通过 `prompt.Builder`、`compact.Pipeline`、`tools.Resolver`、`policy.Checker` 或应用在构造 Agent 前提供的配置完成。

## Extension Levels

| 层级 | 形式 | 位置 | 能力 |
|------|------|------|------|
| Prompt 模板 | Markdown 文件 | 应用定义 | 作为 slash command 或 prompt section 使用 |
| Skill | Markdown + front matter | 应用或 `skills/` | 可复用指令、资源和脚本 |
| Hook | Go handler | core/contrib | 核心生命周期观察和少量拦截 |
| App event bus | 应用自定义 | 应用 | 配置、文件、任务、channel notification 等产品事件 |
| Extension package | Go package | contrib 或应用 | Provider、Tool、Hook、Policy 组合分发 |

Skill front matter 可以声明 hook、MCP server、模型覆盖等应用层配置，但 core hook 不负责解析 skill 文件。Skill loader 读取这些配置后再注册对应 hook 或 tool。

## 关键设计决策

1. **核心事件收窄且封闭**：core 只承诺 Agent/Model/Tool/Session/Policy/Compact 生命周期事件，`hook.Event` 不开放应用自定义实现。
2. **应用事件总线自持**：配置、文件、任务、channel notification 属于应用协议，不进入 core API，也不通过 core `hook.Registry` 发射。
3. **拦截点显式化**：只有 Tool、Model、Stop 等少量点提供专用拦截 handler。
4. **模型 hook 只看快照**：hook 可以观察模型生命周期，但不能修改 raw `model.Request` 或注入 `model.Message`。
5. **Policy 与 Hook 解耦**：Agent Loop 编排二者，避免 `policy/` 依赖 `hook/`。
6. **Session 作用域清理**：临时 hook 通过 `WithSession` 绑定，session 结束时清理，避免过期 handler 干扰后续运行。
