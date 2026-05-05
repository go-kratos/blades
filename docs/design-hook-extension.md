---
type: design
title: 扩展与 Hook 系统
parent: design-agent-framework.md
date: 2026-05-01
status: draft
modules: [module-4]
---

# 扩展与 Hook 系统

### 现状对比

| 维度 | 当前 Blades | 新设计 |
|------|------------|--------|
| 事件系统 | 无 | 类型化 hook.Event + hook.Registry |
| 扩展机制 | 仅 Middleware | Hook 系统 + Skill 系统 |
| 生命周期覆盖 | 无 | Agent/Model/Tool/Session/Policy/Config/FS/Task/Stop 等 20+ 事件 |
| 扩展层级 | 无 | Prompt → Skill 两层渐进 |

### 4.1 Hook 事件系统

```go
// Event 是所有生命周期事件的判别联合。
type Event interface{ hookEvent() }

// --- Agent 生命周期 ---
type AgentStart         struct{ AgentName string; Turn int }
type AgentEnd           struct{ AgentName string; Messages []*model.Message }
type SubagentStart      struct{ ParentAgent, ChildAgent string; QuerySource QuerySource }
type SubagentEnd        struct{ ParentAgent, ChildAgent string }

// --- Model 生命周期 ---
type BeforeModelRequest struct{ Messages []*model.Message; Tools []model.ToolSpec }
type AfterModelResponse struct{ Message *model.Message; Usage *model.TokenUsage }

// --- Tool 生命周期 ---
type PreToolUse         struct{ ToolName string; Input json.RawMessage }
type PostToolUse        struct{ ToolName string; Result *tools.Result; Err error }
type PostToolUseFailure struct{ ToolName string; Err error }

// --- 权限/模式生命周期 ---
type ModeChange           struct{ From, To policy.Mode }
type PolicyRequest    struct{ ToolName string; Input json.RawMessage; Mode policy.Mode }
type PolicyDenied     struct{ ToolName string; Input json.RawMessage; Reason string }

// --- Session 生命周期 ---
type SessionStart         struct{ SessionID string; IsResume bool }
type SessionEnd           struct{ SessionID string }

// --- 压缩生命周期 ---
type PreCompact           struct{ Messages []*model.Message; TokenCount int64 }
type PostCompact          struct{ Summary string; TokensBefore, TokensAfter int64 }

// --- 配置生命周期 ---
type ConfigChange         struct{ Key string; OldValue, NewValue any }

// --- 文件系统生命周期 ---
type CwdChanged           struct{ OldCwd, NewCwd string }
type FileChanged          struct{ Path string; Action string } // created/modified/deleted

// --- Task 生命周期 ---
type TaskCreated          struct{ TaskID, Subject string }
type TaskCompleted        struct{ TaskID, Subject, Status string }

// --- 通知 ---
type Notification         struct{ Message string }

// --- Stop 生命周期 ---
type Stop                 struct{
    AgentName string
    Messages  []*model.Message
    Usage     *model.TokenUsage
    Reason    string
}
```

### 4.2 Hook 注册与执行

Hook Handler 按事件类型分为两类：观察型（只通知，不拦截）和拦截型（可修改行为）。
拦截型 Hook 使用专用的返回类型，避免"大联合返回值"的误用问题。

```go
// ObserveHandler 观察型 Hook，只通知不拦截。返回 error 会记录日志但不中止操作。
type ObserveHandler[E Event] func(ctx context.Context, event E) error

// --- 拦截型 Hook，使用专用返回类型 ---

// PreToolUseHandler 在工具执行前调用，可阻止执行或修改输入。
type PreToolUseHandler func(ctx context.Context, event *PreToolUse) (*PreToolUseResult, error)

type PreToolUseResult struct {
    Block         bool                 // true = 阻止执行
    Reason        string               // 阻止原因
    Decision      *policy.Decision // 覆盖权限决策
    ModifiedInput json.RawMessage      // 修改后的参数（nil = 不修改）
}

// PostToolUseHandler 在工具执行后调用，可修改结果。
type PostToolUseHandler func(ctx context.Context, event *PostToolUse) (*PostToolUseResult, error)

type PostToolUseResult struct {
    ModifiedResult *tools.Result // 修改后的结果（nil = 不修改）
}

// BeforeModelHandler 在模型调用前调用，可注入系统消息或中止。
type BeforeModelHandler func(ctx context.Context, event *BeforeModelRequest) (*BeforeModelResult, error)

// BeforeModelResult 模型调用前拦截结果。
type BeforeModelResult struct {
    Continue       bool             // false = 中止模型调用
    SystemMessages []*model.Message // 注入系统消息
    StopReason     string           // 中止原因
}

// StopHandler 在 Agent 轮次结束时运行。
// 返回 ContinueLoop=true 可注入 follow-up 消息继续循环。
type StopHandler func(ctx context.Context, event *Stop) (*StopResult, error)

type StopResult struct {
    ContinueLoop bool              // true = 注入 FollowUp 继续循环
    FollowUp     []event.InputPart // 作为 user input 注入，由 Agent Loop 转为 model.Message
}

// Registry 管理 Hook 订阅和发射。
type Registry struct {
    mu       sync.RWMutex
    handlers map[reflect.Type][]hookEntry
}

type hookEntry struct {
    handler  any    // ObserveHandler[E] 或拦截型 Handler
    priority int    // 数字越小优先级越高
    scope    string // 作用域标识（如 agent 名称），空 = 全局
}

// Observe 注册观察型 Hook（只通知，不拦截）。
func Observe[E Event](r *Registry, handler ObserveHandler[E], opts ...Option)

// OnPreToolUse 注册工具执行前拦截 Hook。
func (r *Registry) OnPreToolUse(handler PreToolUseHandler, opts ...Option)

// OnPostToolUse 注册工具执行后拦截 Hook。
func (r *Registry) OnPostToolUse(handler PostToolUseHandler, opts ...Option)

// OnBeforeModel 注册模型调用前拦截 Hook。
func (r *Registry) OnBeforeModel(handler BeforeModelHandler, opts ...Option)

// OnStop 注册 stop hook handler，在 Agent Loop 完成一个完整轮次后触发。
// 用于 memory 提取、auto-dream、prompt suggestion 等后台工作。
func (r *Registry) OnStop(handler StopHandler, opts ...Option)

// Emit 发射事件，按优先级调用所有匹配的 Handler。
func (r *Registry) Emit(ctx context.Context, event Event) error

// Option 配置 Hook 注册。
type Option func(*hookEntry)
func WithPriority(priority int) Option
func WithScope(scope string) Option

// WithSession 将 hook 绑定到特定会话/agent 生命周期。
// 当会话/agent 结束时，绑定的 hooks 自动清理。
func WithSession(sessionID string) Option

// WithTimeout 设置单个 hook 的超时时间，覆盖默认 30s。
func WithTimeout(d time.Duration) Option

// ClearSessionHooks 移除绑定到指定会话的所有 hooks。
// 在 agent 生命周期结束时由 runAgent cleanup 调用。
func (r *Registry) ClearSessionHooks(sessionID string)
```

### 4.3 两层渐进式扩展

| 层级 | 形式 | 位置 | 能力 | 复杂度 |
|------|------|------|------|--------|
| Prompt 模板 | Markdown 文件 | `.blades/prompts/` | 可作为 `/name` 斜杠命令调用的提示模板 | 最低 |
| Skill | Markdown + YAML frontmatter | `.blades/skills/`, `skills/` | 按需加载的可复用指令，含资源和脚本 | 低 |

当前阶段工具通过 `tools.Tool` 接口注册，Provider 通过构造函数注入，这些已经够用。
等真正有第三方扩展生态需求时，再设计 Extension API（工具/命令/Provider/Hook 注册）和 Package 分发机制。

#### Skill frontmatter 增强

```yaml
---
name: my-skill
description: What this skill does
allowed-tools: "read,write,bash*"
model: claude-sonnet-4-6          # 模型覆盖
hooks:                             # Skill 作用域 Hook
  pre_tool_use:
    - command: "validate-input.sh"
mcp-servers:                       # Skill 作用域 MCP 服务器
  - name: my-server
    transport: stdio
    command: "npx my-mcp-server"
max-turns: 20                      # 最大轮次
---
```

### 关键设计决策

1. **类型化 hook.Event 而非字符串事件** — 使用 Go 接口判别联合而非字符串事件名，编译时类型安全，IDE 自动补全，不会拼错事件名。事件类型在 `hook` 包内不加 `Hook` 前缀，例如 `hook.PreToolUse`、`hook.Stop`，避免 `hook.HookPreToolUse` 这类重复命名。

2. **观察型与拦截型分离** — 大多数 Hook 只需要观察（日志、追踪、统计），使用简单的 `ObserveHandler[E]` 即可。少数需要拦截的 Hook（PreToolUse、BeforeModel）使用专用的返回类型，避免"大联合返回值"的误用问题。

3. **Hook 与 Middleware 共存** — Middleware 是洋葱模型（包装 Handler），适合横切关注点（重试、追踪）。Hook 是事件订阅模型，适合观察和拦截特定生命周期节点。两者互补而非替代。

4. **先核心事件，按需扩展** — 初始定义 Agent/Model/Tool 核心路径事件以及 Session/Policy/Compression/Config/FS/Task/Stop/Notification 等扩展事件，共 20+ 种类型化事件覆盖完整生命周期。Hook 注册机制是开放的，新增事件类型不需要修改接口。

5. **两层渐进式扩展** — Prompt 模板和 Skill 覆盖大多数定制需求，无需编写 Go 代码。Extension API 和 Package 分发机制等有第三方扩展生态需求时再设计。

6. **多 Hook 响应聚合：deny 优先** — 当多个 Hook 响应同一事件时，采用 deny-wins 策略：只要有一个 Hook 返回 Block=true / deny，最终结果即为拒绝。这与 Claude Code 的 `resolveHookPolicyDecision` 行为一致，确保安全策略不会被其他 Hook 覆盖。

7. **Hook 超时控制** — 每个 Hook 默认超时 30 秒，防止单个 Hook 阻塞整个 Agent Loop。可通过 `hook.WithTimeout(d time.Duration)` 按需调整。超时后 Hook 被取消，返回 error 记录日志，不影响其他 Hook 执行。

8. **Session 作用域 Hook** — 通过 `hook.WithSession(sessionID)` 将 Hook 绑定到特定会话生命周期。会话结束时自动清理绑定的 Hook，避免内存泄漏和过期 Hook 干扰。适用于 Skill 加载的临时 Hook、会话级别的监控 Hook 等场景。
