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
| 事件系统 | 无 | 类型化 HookEvent + HookRegistry |
| 扩展机制 | 仅 Middleware | Hook 系统 + Skill 系统 |
| 生命周期覆盖 | 无 | Agent/Model/Tool/Session/Permission/Config/FS/Task/Stop 等 20+ 事件 |
| 扩展层级 | 无 | Prompt → Skill 两层渐进 |

### 4.1 Hook 事件系统

```go
// HookEvent 是所有生命周期事件的判别联合。
type HookEvent interface{ hookEvent() }

// --- Agent 生命周期 ---
type HookAgentStart         struct{ AgentName string; Turn int }
type HookAgentEnd           struct{ AgentName string; Messages []*Message }
type HookSubagentStart      struct{ ParentAgent, ChildAgent string; QuerySource QuerySource }
type HookSubagentEnd        struct{ ParentAgent, ChildAgent string }

// --- Model 生命周期 ---
type HookBeforeModelRequest struct{ Messages []*model.Message; Tools []Tool }
type HookAfterModelResponse struct{ Message *model.Message; Usage *model.TokenUsage }

// --- Tool 生命周期 ---
type HookPreToolUse         struct{ ToolName string; Input string }
type HookPostToolUse        struct{ ToolName string; Result string; Err error }
type HookPostToolUseFailure struct{ ToolName string; Err error }

// --- 权限/模式生命周期 ---
type HookModeChange           struct{ From, To permission.Mode }
type HookPermissionRequest    struct{ ToolName, Input string; Mode permission.Mode }
type HookPermissionDenied     struct{ ToolName, Input, Reason string }

// --- Session 生命周期 ---
type HookSessionStart         struct{ SessionID string; IsResume bool }
type HookSessionEnd           struct{ SessionID string }

// --- 压缩生命周期 ---
type HookPreCompact           struct{ Messages []*model.Message; TokenCount int64 }
type HookPostCompact          struct{ Summary string; TokensBefore, TokensAfter int64 }

// --- 配置生命周期 ---
type HookConfigChange         struct{ Key string; OldValue, NewValue any }

// --- 文件系统生命周期 ---
type HookCwdChanged           struct{ OldCwd, NewCwd string }
type HookFileChanged          struct{ Path string; Action string } // created/modified/deleted

// --- Task 生命周期 ---
type HookTaskCreated          struct{ TaskID, Subject string }
type HookTaskCompleted        struct{ TaskID, Subject, Status string }

// --- 通知 ---
type HookNotification         struct{ Message string }

// --- Stop 生命周期 ---
type HookStop                 struct{
    AgentName string
    Messages  []*model.Message
    Usage     *model.TokenUsage
    Reason    TerminalReason
}
```

### 4.2 Hook 注册与执行

Hook Handler 按事件类型分为两类：观察型（只通知，不拦截）和拦截型（可修改行为）。
拦截型 Hook 使用专用的返回类型，避免"大联合返回值"的误用问题。

```go
// ObserveHandler 观察型 Hook，只通知不拦截。返回 error 会记录日志但不中止操作。
type ObserveHandler[E HookEvent] func(ctx context.Context, event E) error

// --- 拦截型 Hook，使用专用返回类型 ---

// PreToolUseHandler 在工具执行前调用，可阻止执行或修改输入。
type PreToolUseHandler func(ctx context.Context, event *HookPreToolUse) (*PreToolUseResult, error)

type PreToolUseResult struct {
    Block        bool                // true = 阻止执行
    Reason       string              // 阻止原因
    Decision     *PermissionDecision // 覆盖权限决策
    ModifiedInput string             // 修改后的参数（空 = 不修改）
}

// PostToolUseHandler 在工具执行后调用，可修改结果。
type PostToolUseHandler func(ctx context.Context, event *HookPostToolUse) (*PostToolUseResult, error)

type PostToolUseResult struct {
    ModifiedResult string // 修改后的结果（空 = 不修改）
}

// BeforeModelHandler 在模型调用前调用，可注入系统消息或中止。
type BeforeModelHandler func(ctx context.Context, event *HookBeforeModelRequest) (*BeforeModelResult, error)

// BeforeModelResult 模型调用前拦截结果。
type BeforeModelResult struct {
    Continue      bool   // false = 中止模型调用
    SystemMessage string // 注入系统消息
    StopReason    string // 中止原因
}

// StopHookHandler 在 Agent 轮次结束时运行。
// 返回 ContinueLoop=true 可注入 follow-up 消息继续循环。
type StopHookHandler func(ctx context.Context, event *HookStop) (*StopHookResult, error)

type StopHookResult struct {
    ContinueLoop bool   // true = 注入 FollowUp 继续循环
    FollowUp     string // 作为 user message 注入
}

// HookRegistry 管理 Hook 订阅和发射。
type HookRegistry struct {
    mu       sync.RWMutex
    handlers map[reflect.Type][]hookEntry
}

type hookEntry struct {
    handler  any    // ObserveHandler[E] 或拦截型 Handler
    priority int    // 数字越小优先级越高
    scope    string // 作用域标识（如 agent 名称），空 = 全局
}

// Observe 注册观察型 Hook（只通知，不拦截）。
func Observe[E HookEvent](r *HookRegistry, handler ObserveHandler[E], opts ...HookOption)

// OnPreToolUse 注册工具执行前拦截 Hook。
func (r *HookRegistry) OnPreToolUse(handler PreToolUseHandler, opts ...HookOption)

// OnPostToolUse 注册工具执行后拦截 Hook。
func (r *HookRegistry) OnPostToolUse(handler PostToolUseHandler, opts ...HookOption)

// OnBeforeModel 注册模型调用前拦截 Hook。
func (r *HookRegistry) OnBeforeModel(handler BeforeModelHandler, opts ...HookOption)

// OnStop 注册 stop hook handler，在 Agent Loop 完成一个完整轮次后触发。
// 用于 memory 提取、auto-dream、prompt suggestion 等后台工作。
func (r *HookRegistry) OnStop(handler StopHookHandler, opts ...HookOption)

// Emit 发射事件，按优先级调用所有匹配的 Handler。
func (r *HookRegistry) Emit(ctx context.Context, event HookEvent) error

// HookOption 配置 Hook 注册。
type HookOption func(*hookEntry)
func WithHookPriority(priority int) HookOption
func WithHookScope(scope string) HookOption

// WithHookSession 将 hook 绑定到特定会话/agent 生命周期。
// 当会话/agent 结束时，绑定的 hooks 自动清理。
func WithHookSession(sessionID string) HookOption

// WithHookTimeout 设置单个 hook 的超时时间，覆盖默认 30s。
func WithHookTimeout(d time.Duration) HookOption

// ClearSessionHooks 移除绑定到指定会话的所有 hooks。
// 在 agent 生命周期结束时由 runAgent cleanup 调用。
func (r *HookRegistry) ClearSessionHooks(sessionID string)
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

1. **类型化 HookEvent 而非字符串事件** — 使用 Go 接口判别联合而非字符串事件名，编译时类型安全，IDE 自动补全，不会拼错事件名。

2. **观察型与拦截型分离** — 大多数 Hook 只需要观察（日志、追踪、统计），使用简单的 `ObserveHandler[E]` 即可。少数需要拦截的 Hook（PreToolUse、BeforeModel）使用专用的返回类型，避免"大联合返回值"的误用问题。

3. **Hook 与 Middleware 共存** — Middleware 是洋葱模型（包装 Handler），适合横切关注点（重试、追踪）。Hook 是事件订阅模型，适合观察和拦截特定生命周期节点。两者互补而非替代。

4. **先核心事件，按需扩展** — 初始定义 Agent/Model/Tool 核心路径事件以及 Session/Permission/Compression/Config/FS/Task/Stop/Notification 等扩展事件，共 20+ 种类型化事件覆盖完整生命周期。Hook 注册机制是开放的，新增事件类型不需要修改接口。

5. **两层渐进式扩展** — Prompt 模板和 Skill 覆盖大多数定制需求，无需编写 Go 代码。Extension API 和 Package 分发机制等有第三方扩展生态需求时再设计。

6. **多 Hook 响应聚合：deny 优先** — 当多个 Hook 响应同一事件时，采用 deny-wins 策略：只要有一个 Hook 返回 Block=true / deny，最终结果即为拒绝。这与 Claude Code 的 `resolveHookPermissionDecision` 行为一致，确保安全策略不会被其他 Hook 覆盖。

7. **Hook 超时控制** — 每个 Hook 默认超时 30 秒，防止单个 Hook 阻塞整个 Agent Loop。可通过 `WithHookTimeout(d time.Duration)` 按需调整。超时后 Hook 被取消，返回 error 记录日志，不影响其他 Hook 执行。

8. **Session 作用域 Hook** — 通过 `WithHookSession(sessionID)` 将 Hook 绑定到特定会话生命周期。会话结束时自动清理绑定的 Hook，避免内存泄漏和过期 Hook 干扰。适用于 Skill 加载的临时 Hook、会话级别的监控 Hook 等场景。
