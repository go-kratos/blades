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
| 生命周期覆盖 | 无 | Agent/Model/Tool 核心事件（按需扩展） |
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
type HookModeChange         struct{ From, To permission.Mode }

// 其他生命周期事件（Session、压缩、Memory、配置等）
// 在对应模块实现时按需添加。Hook 注册机制是开放的，
// 新增事件类型不需要修改接口。
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

type BeforeModelResult struct {
    Continue      bool   // false = 中止模型调用
    SystemMessage string // 注入系统消息
    StopReason    string // 中止原因
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

// Emit 发射事件，按优先级调用所有匹配的 Handler。
func (r *HookRegistry) Emit(ctx context.Context, event HookEvent) error

// HookOption 配置 Hook 注册。
type HookOption func(*hookEntry)
func WithHookPriority(priority int) HookOption
func WithHookScope(scope string) HookOption
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

4. **先核心事件，按需扩展** — 初始只定义 Agent/Model/Tool 核心路径事件。压缩、权限、Memory、配置等事件在对应模块实现时按需添加。Hook 注册机制是开放的，新增事件类型不需要修改接口。

5. **两层渐进式扩展** — Prompt 模板和 Skill 覆盖大多数定制需求，无需编写 Go 代码。Extension API 和 Package 分发机制等有第三方扩展生态需求时再设计。
