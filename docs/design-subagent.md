---
type: design
title: 子 Agent 系统
parent: design-agent-framework.md
date: 2026-05-01
status: draft
modules: [module-7]
---

## 模块 7：子 Agent 系统

### 现状对比

| 维度 | 当前 Blades | 新设计 |
|------|------------|--------|
| 子 Agent | NewAgentTool 包装 | ForkAgent 共享缓存 + 多种派生模式 |
| 缓存共享 | 无 | 共享父 Agent 的 prompt cache 前缀 |
| 后台执行 | 无 | BackgroundAgent fire-and-forget |
| 隔离模式 | 仅 session 隔离 | Session / Worktree / Remote |
| 来源标记 | 无 | QuerySource 区分行为 |

### 7.1 Fork 配置

```go
// ForkConfig 控制子 Agent 的派生方式。
type ForkConfig struct {
    // ShareCachePrefix 使子 Agent 共享父 Agent 的 prompt cache 前缀。
    // 压缩、Memory 提取等操作因此可以命中缓存，成本低廉。
    ShareCachePrefix bool

    // IsolateSession 创建新 session（true）或共享父 session（false）。
    IsolateSession bool

    // QuerySource 标记此 fork 的来源，用于行为区分。
    QuerySource QuerySource

    // Tools 覆盖工具集。nil = 继承父 Agent 工具。
    Tools []Tool

    // MaxTurns 限制子 Agent 的最大轮次。
    MaxTurns int

    // PermissionMode 覆盖权限模式。空 = 继承父 Agent。
    PermissionMode permission.Mode

    // Model 覆盖模型。nil = 继承父 Agent。
    Model model.Provider

    // Background 是否后台运行（fire-and-forget）。
    Background bool

    // Hooks 子 Agent 专属 Hook（生命周期作用域）。
    Hooks []HookRegistration

    // AgentMemory 子 Agent 专属 memory 实例。
    // 当非 nil 时，子 Agent 拥有独立的 memory 存储，
    // 可在多次调用间持久化角色专属知识。
    AgentMemory *memory.AgentMemory // NEW: agent 专属 memory

    // Effort 控制模型的推理努力程度。
    // 低努力适合简单搜索，高努力适合复杂推理和规划。
    // 空值 = 继承父 Agent 设置。
    Effort EffortLevel // NEW: 推理努力级别
}

// EffortLevel 控制模型的推理努力程度。
type EffortLevel string
const (
    EffortLow    EffortLevel = "low"
    EffortMedium EffortLevel = "medium"
    EffortHigh   EffortLevel = "high"
)

type QuerySource string
const (
    QuerySourceUser          QuerySource = "user"
    QuerySourceSubAgent      QuerySource = "sub_agent"
    QuerySourceCompact       QuerySource = "compact"
    QuerySourceExtractMemory QuerySource = "extract_memory"
    QuerySourceTaskSummary   QuerySource = "task_summary"
    QuerySourceSkill         QuerySource = "skill"
)
```

### 7.2 ForkAgent

```go
// ForkAgent 创建轻量级 Agent fork。
// 当 ShareCachePrefix=true 时，子 Agent 的 system prompt 构建为
// 与父 Agent 共享静态前缀，使 LLM Provider 可以命中 prompt cache。
func ForkAgent(parent Agent, config ForkConfig) Agent

// 内部实现：
// 1. 克隆父 Agent 的 prompt.Builder（共享静态 sections）
// 2. 替换动态 sections（子 Agent 可能有不同的 Memory/环境）
// 3. 根据 config 设置工具集、权限、模型
// 4. 如果 IsolateSession=true，创建新 session
// 5. 如果 Background=true，包装为 BackgroundAgent
```

### 7.3 BackgroundAgent

```go
// BackgroundAgent 在 goroutine 中运行 fork agent，不阻塞主循环。
// 用于 Memory 提取、任务摘要等 fire-and-forget 操作。
type BackgroundAgent struct {
    agent    Agent
    cancel   context.CancelFunc
    done     chan struct{}
    err      error
    messages []*Message
}

// RunBackground 启动后台 Agent。
func RunBackground(ctx context.Context, agent Agent, input <-chan InputEvent) *BackgroundAgent

// Drain 等待后台 Agent 完成（在关闭前调用）。
func (b *BackgroundAgent) Drain(timeout time.Duration) error

// Cancel 取消后台 Agent。
func (b *BackgroundAgent) Cancel()

// Done 返回完成信号 channel。
func (b *BackgroundAgent) Done() <-chan struct{}
```

### 7.4 Worktree 隔离

```go
// WorktreeConfig 控制 git worktree 隔离。
type WorktreeConfig struct {
    BaseBranch string // 基于哪个分支创建 worktree
    Name       string // worktree 名称（空 = 自动生成）
    BaseDir    string // worktree 基础目录，默认 .blades/worktrees/
}

// CreateWorktreeAgent 创建在隔离 git worktree 中运行的子 Agent。
// 返回 Agent、清理函数和错误。
func CreateWorktreeAgent(
    parent Agent, config WorktreeConfig, forkConfig ForkConfig,
) (agent Agent, cleanup func() error, err error)

// 内部实现：
// 1. git worktree add <baseDir>/<name> -b <name> <baseBranch>
// 2. 设置子 Agent 的 CWD 为 worktree 路径
// 3. cleanup 函数：git worktree remove <path>
```

### 7.5 子 Agent 执行流程

```
1. 解析 ForkConfig（模型、权限、工具集）
2. 构建子 Agent system prompt（共享静态前缀）
3. 创建子 Agent 上下文
   - 同步 Agent：共享 AbortController
   - 异步 Agent：隔离的 AbortController
4. 发射 HookSubagentStart
5. 注册子 Agent 专属 Hook（生命周期作用域）
6. 预加载 Skill（如果 ForkConfig 指定）
7. 初始化子 Agent 专属 MCP 服务器（叠加到父 Agent）
8. 调用 agent.Run() 循环，yield LoopEvent
9. finally：清理 MCP 服务器、作用域 Hook、prompt cache
```

### 7.6 内置 Fork 用途

| 用途 | QuerySource | ShareCache | Background | 说明 |
|------|------------|------------|------------|------|
| 上下文压缩 | `compact` | 是 | 否 | 生成压缩摘要 |
| Memory 提取 | `extract_memory` | 是 | 是 | 从对话中提取持久性事实 |
| 任务摘要 | `task_summary` | 是 | 是 | 周期性生成任务进度摘要 |
| Skill 执行 | `skill` | 否 | 否 | 在隔离环境中执行 Skill |
| 用户子 Agent | `sub_agent` | 否 | 可选 | 用户通过 AgentTool 派生 |
| 隐式 fork | `sub_agent` | 是 | 否 | subagent_type 为空时自动触发 |

### 7.6.1 AgentTool 路由逻辑

AgentTool 接收 `subagent_type` 参数后，按以下优先级路由：

```
1. subagent_type 非空 → Registry.Resolve(subagent_type) → Spawn
2. subagent_type 为空 + 显式 role 参数 → 按 role 路由
3. subagent_type 为空 + Background=true → BackgroundAgent
4. subagent_type 为空 + Worktree 配置 → CreateWorktreeAgent
5. subagent_type 为空 + ForkSubagent 特性启用 → ForkAgent（隐式 fork）
6. 默认 → 同步 runAgent()
```

#### Implicit Fork（隐式 fork）

当 `subagent_type` 为空且没有显式指定角色时，若 `ForkSubagent` 特性开关启用，自动触发隐式 fork。隐式 fork 继承父级完整对话上下文，适用于需要在子 agent 中延续当前对话但不改变角色的场景。

```go
// ForkAgent 是隐式 fork 的内置角色。
// 当 AgentTool 的 subagent_type 为空时自动使用。
// 子 agent 继承父级完整对话上下文和已渲染的 system prompt 字节（不重新生成），
// 保证 prompt cache 命中稳定性。
var ForkAgent = &Role{
    Name:        "_fork",
    Description: "隐式 fork：继承父级完整上下文",
    Source:      SourceBuiltIn,
    WhenToUse:   "internal: 当 subagent_type 为空时自动触发",
    ConfigureFunc: func(ctx context.Context, parent ConfigContext) (*ForkConfig, error) {
        return &ForkConfig{
            ShareCachePrefix: true,
            IsolateSession:   false, // 共享父级上下文
            QuerySource:      QuerySourceSubAgent,
            PermissionMode:   permission.ModeBubble,
            // tools 默认继承父级所有工具
        }, nil
    },
}
```

### 关键设计决策

1. **共享 Prompt Cache** — 当前子 Agent 完全隔离，每次调用都是冷缓存。新设计通过共享静态 system prompt 前缀，使子 Agent 可以命中父 Agent 的 prompt cache，压缩和 Memory 提取等高频操作成本大幅降低。

2. **Fire-and-forget 后台 Agent** — Memory 提取和任务摘要不需要阻塞主循环。BackgroundAgent 在 goroutine 中运行，主循环继续处理用户请求。Drain 机制确保关闭前等待后台任务完成。

3. **QuerySource 行为区分** — 不同来源的 fork 有不同的行为约束。例如 `compact` fork 只需要生成摘要，不需要执行工具；`extract_memory` fork 只能使用只读工具 + Memory 写入工具。QuerySource 标记使这些约束可以在权限链和 Hook 中精确匹配。

3.1. **隐式 fork 使用 ModeBubble** — 隐式 fork 继承父级完整上下文，因此权限决策应与父级保持一致。Bubble 模式确保父级的权限链处理所有决策，避免子 agent 产生与父级不一致的权限行为。如果子 agent 独立决策权限，可能出现父级已拒绝但子 agent 允许的情况，破坏安全边界。

---

## Agent 角色系统

`agent.Role` 是 ForkConfig 的上层抽象——一个可复用的 Agent 角色模板，定义了工具约束、system prompt、模型选择等策略。框架内置 4 种通用角色（explore/plan/general/verify），用户可注册自定义角色。

### 7.7 Role 定义

`Role` 是**工厂**，不是 Agent 本身。它产出 ForkConfig + instruction，实际 Agent 创建由 `ForkAgent` 完成。这避免了平行的 Agent 层次结构，`ForkAgent` 始终是创建子 Agent 的唯一机制。

```go
// agent/role.go

// Role defines a reusable agent role that can be instantiated via ForkAgent.
// It is a factory for ForkConfig + system prompt, not an Agent itself.
type Role struct {
    // Name is the unique identifier (e.g., "explore", "plan", "verify").
    Name string

    // WhenToUse describes when a coordinator should dispatch to this role.
    // Used by the AgentTool to help the model choose the right sub-agent.
    WhenToUse string

    // Source distinguishes built-in roles from user-defined ones.
    Source Source

    // MemoryScope controls the agent memory scope for this role.
    // Determines which memory entries are visible and writable.
    MemoryScope memory.AgentMemoryScope // NEW: agent memory 作用域

    // ConfigureFunc produces a ForkConfig from the parent agent context.
    // The parent's tools are passed in so the role can filter them.
    ConfigureFunc func(ctx context.Context, parent ConfigContext) (*ForkConfig, error)

    // SystemPrompt returns the specialized system prompt for this agent role.
    SystemPrompt func(ctx context.Context, parent ConfigContext) (string, error)

    // Options holds optional behavioral flags.
    Options RoleOptions
}

type Source string

const (
    SourceBuiltIn Source = "built-in"
    SourceUser    Source = "user"
    SourcePlugin  Source = "plugin"
)

// RoleOptions controls optional behaviors.
type RoleOptions struct {
    // Background runs the agent in a goroutine (fire-and-forget).
    Background bool

    // OmitMemory skips loading CLAUDE.md / memory files.
    OmitMemory bool

    // Color for UI display (optional).
    Color string
}

// ConfigContext provides read access to the parent agent's configuration
// so that Role.ConfigureFunc can make informed decisions.
type ConfigContext interface {
    Tools() []tools.Tool
    Model() model.Provider
    PermissionMode() permission.Mode
}
```

### 7.8 ToolFilter 工具过滤

`ToolFilter` 是函数类型，比字符串列表更可组合。`ReadOnlyTools()` 利用 tools 包已有的 `ReadOnlyTool` 可选接口，静态列表无法表达"父 agent 拥有的所有只读工具"。

```go
// agent/filter.go

// ToolFilter selects which tools are available to the agent type.
type ToolFilter func(tool tools.Tool) bool

// ReadOnlyTools keeps only tools that implement ReadOnlyTool and return true.
func ReadOnlyTools() ToolFilter

// AllowOnly keeps only tools whose names are in the whitelist.
func AllowOnly(names ...string) ToolFilter

// Disallow removes tools whose names are in the blacklist.
func Disallow(names ...string) ToolFilter

// And combines filters: tool must pass all filters.
func And(filters ...ToolFilter) ToolFilter

// Or combines filters: tool must pass at least one filter.
func Or(filters ...ToolFilter) ToolFilter

// FilterTools applies a filter to a tool slice, returning the matching subset.
func FilterTools(all []tools.Tool, f ToolFilter) []tools.Tool
```

使用示例：

```go
// Keep read-only tools, but also exclude bash
filter := And(ReadOnlyTools(), Disallow("bash"))
filtered := FilterTools(parent.Tools(), filter)
```

### 7.9 Role Registry

线程安全的注册表，遵循 `recipe.MiddlewareRegistry` 的模式。

```go
// agent/registry.go

// Registry stores and retrieves Role definitions.
type Registry struct {
    mu    sync.RWMutex
    types map[string]*Role
}

func NewRegistry() *Registry

// Register adds a Role. Panics on duplicate name (fail-fast at init).
func (r *Registry) Register(role *Role)

// Resolve returns the Role for the given name, or an error if not found.
func (r *Registry) Resolve(name string) (*Role, error)

// List returns all registered roles (for discovery / help text).
func (r *Registry) List() []*Role

// DefaultRegistry returns a registry pre-loaded with the 4 built-in roles.
func DefaultRegistry() *Registry
```

### 7.10 Spawn 便捷函数

将 Registry 查找、ForkConfig 构建、ForkAgent 创建串联为一步调用。AgentTool 内部通过 `Spawn` 处理 `subagent_type` 参数。

```go
// agent/spawn.go

// Spawn creates a sub-agent from a registered Role.
// Flow: Resolve role → ConfigureFunc(parent) → SystemPrompt(parent) → ForkAgent(parent, config)
func Spawn(ctx context.Context, registry *Registry, roleName string, parent Agent) (Agent, error)
```

### 7.11 内置 Agent 角色

框架内置 4 种通用 Agent 角色，覆盖搜索→规划→执行→验证的完整工作流。

| 类型 | 工具约束 | 模型 | 后台 | OmitMemory | MaxTurns | ShareCache | MemoryScope | Effort |
|------|---------|------|------|-----------|----------|-----------|-------------|--------|
| `explore` | `ReadOnlyTools()` | 可配置（默认更快模型） | 否 | 是 | 5 | 是 | `none` | `low` |
| `plan` | `ReadOnlyTools()` | 继承父 agent | 否 | 是 | 15 | 是 | `read_only` | `high` |
| `general` | 全部继承 | 继承父 agent | 否 | 否 | 继承 | 否 | `inherit` | 继承 |
| `verify` | `ReadOnlyTools()` + /tmp 写入 | 继承父 agent | 是 | 否 | 20 | 是 | `read_only` | `medium` |
| `_fork` | 全部继承 | 继承父 agent | 否 | 否 | 继承 | 是 | `inherit` | 继承 |

#### explore — 快速只读搜索

```go
var Explore = &Role{
    Name:      "explore",
    WhenToUse: "Fast read-only search agent for locating code. Use it to find files " +
        "by pattern, grep for symbols or keywords, or answer 'where is X defined'. " +
        "Specify search breadth: quick, medium, or very thorough.",
    Source:      SourceBuiltIn,
    MemoryScope: memory.ScopeNone, // 搜索不需要 memory
    ConfigureFunc: func(ctx context.Context, parent ConfigContext) (*ForkConfig, error) {
        return &ForkConfig{
            Tools:            FilterTools(parent.Tools(), ReadOnlyTools()),
            ShareCachePrefix: true,
            IsolateSession:   true,
            QuerySource:      QuerySourceSubAgent,
            MaxTurns:         5,
            Effort:           EffortLow, // 快速搜索，低努力
        }, nil
    },
    SystemPrompt: exploreSystemPrompt,
    Options: RoleOptions{
        OmitMemory: true,
    },
}
```

System prompt 要点：
- 文件搜索专家，强调速度和并行工具调用
- 严格只读模式，禁止任何文件修改
- 高效使用搜索工具，多个搜索并行发起
- 结果直接作为消息返回，不创建文件

#### plan — 架构设计

```go
var Plan = &Role{
    Name:      "plan",
    WhenToUse: "Software architect agent for designing implementation plans. " +
        "Returns step-by-step plans, identifies critical files, and considers " +
        "architectural trade-offs.",
    Source:      SourceBuiltIn,
    MemoryScope: memory.ScopeReadOnly, // 规划需要读取历史决策
    ConfigureFunc: func(ctx context.Context, parent ConfigContext) (*ForkConfig, error) {
        return &ForkConfig{
            Tools:            FilterTools(parent.Tools(), ReadOnlyTools()),
            ShareCachePrefix: true,
            IsolateSession:   true,
            QuerySource:      QuerySourceSubAgent,
            MaxTurns:         15,
            Effort:           EffortHigh, // 复杂推理，高努力
        }, nil
    },
    SystemPrompt: planSystemPrompt,
    Options: RoleOptions{
        OmitMemory: true,
    },
}
```

System prompt 要点：
- 架构师角色，读代码→理解现有模式→设计方案
- 严格只读模式
- 输出结构化实现计划 + 关键文件列表
- 考虑权衡和架构决策

#### general — 全能力默认

```go
var General = &Role{
    Name:      "general",
    WhenToUse: "General-purpose agent for researching complex questions, searching " +
        "for code, and executing multi-step tasks.",
    Source:      SourceBuiltIn,
    MemoryScope: memory.ScopeInherit, // 继承父 agent memory 作用域
    ConfigureFunc: func(ctx context.Context, parent ConfigContext) (*ForkConfig, error) {
        return &ForkConfig{
            QuerySource: QuerySourceSubAgent,
            // Effort 为空，继承父 agent 设置
        }, nil
    },
    SystemPrompt: generalSystemPrompt,
}
```

System prompt 要点：
- 最小化包装，全能力
- 完成任务后简洁报告
- 不创建不必要的文件

#### verify — 对抗性验证

```go
var Verify = &Role{
    Name:      "verify",
    WhenToUse: "Verification specialist that tries to break the implementation. " +
        "Runs builds, tests, linters, and adversarial probes. " +
        "Outputs structured PASS/FAIL/PARTIAL verdict with evidence.",
    Source:      SourceBuiltIn,
    MemoryScope: memory.ScopeReadOnly, // 验证需要读取已知问题和约束
    ConfigureFunc: func(ctx context.Context, parent ConfigContext) (*ForkConfig, error) {
        return &ForkConfig{
            Tools:            FilterTools(parent.Tools(), ReadOnlyTools()),
            ShareCachePrefix: true,
            IsolateSession:   true,
            QuerySource:      QuerySourceSubAgent,
            MaxTurns:         20,
            Effort:           EffortMedium, // 验证需要适度推理
        }, nil
    },
    SystemPrompt: verifySystemPrompt,
    Options: RoleOptions{
        Background: true,
    },
}
```

System prompt 要点：
- 对抗性验证思维——目标是尝试破坏实现，而非确认它能工作
- 严格只读（项目文件），允许 /tmp 写入临时测试脚本
- 运行构建、测试套件、lint/类型检查
- 对抗性探测：并发、边界值、幂等性、孤儿操作
- 输出结构化判定：每个检查项有 Command run / Output observed / Result
- 最终输出 `VERDICT: PASS`、`VERDICT: FAIL` 或 `VERDICT: PARTIAL`

### 7.12 组合模式

将 `flow/` 包中的 3 种通用组合模式迁移到 `agent/` 包。组合模式和 Role 是同一层概念——都是"特定角色/行为的 Agent"，放在同一个包中。

```go
// agent/sequential.go

// SequentialConfig is the configuration for a Sequential agent.
type SequentialConfig struct {
    Name        string
    Description string
    SubAgents   []blades.Agent
}

// NewSequential creates an agent that runs sub-agents sequentially.
// Each sub-agent receives a clone of the original invocation.
func NewSequential(config SequentialConfig) blades.Agent
```

```go
// agent/parallel.go

// ParallelConfig is the configuration for a Parallel agent.
type ParallelConfig struct {
    Name        string
    Description string
    SubAgents   []blades.Agent
}

// NewParallel creates an agent that runs sub-agents in parallel.
// Results are streamed as they arrive from any sub-agent.
func NewParallel(config ParallelConfig) blades.Agent
```

```go
// agent/loop.go

// LoopState captures the observable state available to a LoopCondition.
type LoopState struct {
    Iteration int
    Input     *blades.Message
    Output    *blades.Message
}

// LoopCondition is called after every complete iteration.
type LoopCondition func(ctx context.Context, state LoopState) (bool, error)

// LoopConfig is the configuration for a Loop agent.
type LoopConfig struct {
    Name          string
    Description   string
    MaxIterations int
    Condition     LoopCondition
    SubAgents     []blades.Agent
}

// NewLoop creates an agent that runs sub-agents in a loop.
func NewLoop(config LoopConfig) blades.Agent
```

**去掉的组合模式：**

- **RoutingAgent** — 被 Role + AgentTool 路由替代。主 agent 通过 AgentTool 的 `subagent_type` 参数自行决定路由目标，LLM 天然具备路由决策能力，不需要专门的路由 agent。
- **DeepAgent** — 其 todo/task 管理能力应内化为框架内置工具（TaskCreate/TaskUpdate/TaskList），不作为独立 agent 类型。

### 7.13 用户自定义角色

用户通过构造 `Role` struct 并调用 `registry.Register()` 定义自定义角色。与内置角色走完全相同的路径。

```go
registry := agent.DefaultRegistry()

registry.Register(&agent.Role{
    Name:      "code-review",
    WhenToUse: "When the user asks for a code review or PR review",
    Source:    agent.SourceUser,
    ConfigureFunc: func(ctx context.Context, parent agent.ConfigContext) (*ForkConfig, error) {
        return &ForkConfig{
            Tools:            agent.FilterTools(parent.Tools(), agent.ReadOnlyTools()),
            ShareCachePrefix: true,
            IsolateSession:   true,
            MaxTurns:         15,
        }, nil
    },
    SystemPrompt: func(ctx context.Context, parent agent.ConfigContext) (string, error) {
        return `You are a code review specialist. Focus on:
- Correctness: logic errors, off-by-one, nil dereferences
- Security: injection, auth bypass, data exposure
- Performance: unnecessary allocations, N+1 queries
- Maintainability: naming, structure, test coverage

Report findings with file:line references.`, nil
    },
})
```

### 关键设计决策

4. **Role 是工厂，不是 Agent** — 产出 ForkConfig + instruction，实际创建由 ForkAgent 完成。`ForkAgent` 始终是创建子 Agent 的唯一机制，不引入平行层次。

5. **ConfigureFunc 而非静态字段** — 用函数而非静态 AllowedTools/DisallowedTools 列表，因为需要运行时访问父 agent 的实际工具集。`ReadOnlyTools()` 检查 `ReadOnlyTool` 接口，静态列表做不到。

6. **ToolFilter 函数类型** — 比字符串列表更可组合。过滤器可组合：`And(ReadOnlyTools(), Disallow("bash"))`。

7. **组合模式迁入 agent/ 包** — Sequential/Parallel/Loop 和 Role 是同一层概念，都是"特定角色/行为的 Agent"。RoutingAgent 被 Role 路由替代，DeepAgent 的 todo/task 能力内化为框架内置工具。

8. **4 种内置角色** — explore/plan/general/verify 覆盖搜索→规划→执行→验证的完整工作流。产品特定角色（如文档指南、状态栏配置）由用户自定义。

8.1. **MemoryScope 分级** — 不同角色对 memory 的需求不同。explore 不需要 memory（纯搜索），plan/verify 需要只读访问（读取历史决策和已知约束），general 和 _fork 继承父级作用域。分级控制避免低权限角色意外写入 memory。

8.2. **EffortLevel 推理努力** — 不同任务的推理复杂度差异显著。explore 只需快速匹配（low），plan 需要深度推理（high），verify 需要适度分析（medium）。Effort 映射到模型 API 的 reasoning effort 参数，在成本和质量之间取得平衡。空值表示继承父 agent 设置，避免强制覆盖。

---

## Coordinator 模式

参考 Claude Code `coordinatorMode.ts`，Coordinator 不是新的 Agent 角色，而是一种**运行模式**——把主 Agent 的角色从"执行者"切换为"调度器"。主线程不再自己写代码，而是负责分解任务、派出 worker、汇总结果。

### 7.14 Coordinator 配置

```go
// agent/coordinator.go

// CoordinatorConfig 配置 coordinator 模式。
type CoordinatorConfig struct {
    // Roles 可用的 worker 角色列表。Coordinator 的 system prompt 会列出这些角色供 LLM 选择。
    Roles []*Role

    // MaxWorkers 最大并发 worker 数。0 = 不限制。
    MaxWorkers int

    // NotificationFormat worker 结果回流格式。
    NotificationFormat NotificationFormat
}

type NotificationFormat string

const (
    NotificationXML  NotificationFormat = "xml"
    NotificationJSON NotificationFormat = "json"
)

// NewCoordinator 创建 coordinator agent。
// 内部实现：
// 1. 生成 coordinator system prompt（角色定义 + worker 列表 + 工作流分相 + 约束规则）
// 2. 配置 AgentTool 作为主要工具（coordinator 通过 AgentTool 派出 worker）
// 3. 配置 SendMessageTool（继续已有 worker）
// 4. 配置 TaskStopTool（停止 worker）
func NewCoordinator(name string, config CoordinatorConfig, opts ...blades.AgentOption) (blades.Agent, error)

// CoordinatorSystemPrompt 生成 coordinator 专用 system prompt。
func CoordinatorSystemPrompt(config CoordinatorConfig) string
```

### 7.15 工作流分相

Coordinator 的 system prompt 显式鼓励分相工作流：

| 阶段 | 执行者 | 目的 |
|------|--------|------|
| Research | Workers (并行) | 搜索文件、理解问题 |
| Synthesis | Coordinator | 汇总研究结果、设计实现方案 |
| Implementation | Workers (按文件集串行) | 按 spec 实施修改 |
| Verification | Workers (并行) | 运行构建、测试、lint |

### 7.16 Worker 结果回流

Worker 完成后，结果包装为 `TaskNotificationEvent`（OutputEvent 的一种），注入到 coordinator 的 input channel 中：

```go
// TaskNotificationEvent 是 worker 完成后的结果通知。
// Coordinator 从 input channel 接收此事件，决定下一步操作。
type TaskNotificationEvent struct {
    AgentID string
    Status  string // completed / failed / killed
    Summary string
    Result  string
    Usage   model.TokenUsage
}
```

### 7.17 Coordinator 约束规则

写入 system prompt 的约束（参考 Claude Code）：

- 用 AgentTool spawn worker，用 SendMessage 继续已有 worker，用 TaskStop 停止 worker
- 不要让一个 worker 去检查另一个 worker 的工作
- 独立任务并行 launch
- 写操作按文件集串行，研究任务可并行
- Worker 不能 spawn 其他 worker（防止无限嵌套）

### 7.17.1 Scratchpad（跨 Worker 知识共享）

Coordinator 模式下，多个 worker 可能需要交换中间产物（分析结果、计划、数据文件）。Scratchpad 提供一个共享目录作为 worker 间的知识交换平面。

```go
// agent/scratchpad.go

// ScratchpadConfig 启用 Coordinator 模式下的跨 worker 知识共享。
type ScratchpadConfig struct {
    BaseDir string // 默认 .blades/scratchpad/
}

// Scratchpad 提供共享目录，Coordinator worker 可在其中
// 交换中间产物（分析结果、计划、数据文件）。
type Scratchpad struct {
    dir string
}

func NewScratchpad(config ScratchpadConfig) *Scratchpad
func (s *Scratchpad) Dir() string
```

**集成方式：**

- Coordinator 激活时，自动创建 scratchpad 目录
- 将 scratchpad 目录路径注入每个 worker 的 system prompt（作为环境变量或 prompt section）
- 可选添加 `ScratchpadTool`（读写 scratchpad 目录）到 worker 工具池，提供结构化的文件读写接口
- Worker 完成后，coordinator 可读取 scratchpad 中的产物用于 synthesis 阶段
- Scratchpad 生命周期与 coordinator session 绑定，session 结束时可选清理

```go
// ScratchpadTool 提供对 scratchpad 目录的结构化读写。
type ScratchpadTool struct {
    scratchpad *Scratchpad
}

func NewScratchpadTool(sp *Scratchpad) *ScratchpadTool

// Write 写入文件到 scratchpad。
func (t *ScratchpadTool) Write(name string, content []byte) error

// Read 从 scratchpad 读取文件。
func (t *ScratchpadTool) Read(name string) ([]byte, error)

// List 列出 scratchpad 中的所有文件。
func (t *ScratchpadTool) List() ([]string, error)
```

---

## Swarm / Team 模式

参考 Claude Code `TeamCreateTool` + `teammateMailbox` + `leaderPermissionBridge`，Swarm 是比 Coordinator 更进一步的团队协作模式。区别在于：Coordinator 是临时的 worker 调度，Swarm 是持久化的团队实体，有共享任务列表和 agent 间通信。

### 7.18 Team 实体

```go
// agent/team.go

// Team 是一个显式的团队实体，有持久化状态。
type Team struct {
    Name        string    `json:"name"`
    Description string    `json:"description"`
    CreatedAt   time.Time `json:"created_at"`
    LeadID      string    `json:"lead_id"`
    Members     []Member  `json:"members"`
}

type Member struct {
    AgentID string `json:"agent_id"`
    Name    string `json:"name"`
    Role    string `json:"role"`     // 对应 agent.Role 名称
    Model   string `json:"model"`
    Color   string `json:"color"`    // UI 显示颜色
}

// TeamConfig 配置团队创建。
type TeamConfig struct {
    Name        string
    Description string
    LeadRole    *Role
    BaseDir     string // 团队文件目录，默认 .blades/teams/
}

// TeamStore 团队状态持久化接口。
type TeamStore interface {
    Create(team *Team) error
    Get(name string) (*Team, error)
    AddMember(teamName string, member Member) error
    RemoveMember(teamName string, agentID string) error
    Delete(name string) error
}

// NewFileTeamStore 基于文件的团队存储（JSON）。
func NewFileTeamStore(baseDir string) TeamStore
```

### 7.19 Mailbox 通信

Agent 间通信采用基于文件的 mailbox 系统。每个 teammate 有独立 inbox，支持锁文件保证并发安全。

```go
// agent/mailbox.go

// Mailbox 是基于文件的 agent 间消息系统。
// 每个 teammate 有独立 inbox：{baseDir}/{team}/inboxes/{agent}.json
type Mailbox struct {
    teamName string
    baseDir  string
}

type MailMessage struct {
    From      string    `json:"from"`
    Text      string    `json:"text"`
    Summary   string    `json:"summary"`
    Timestamp time.Time `json:"timestamp"`
    Type      MailType  `json:"type"`
    Read      bool      `json:"read"`
}

type MailType string

const (
    MailRegular            MailType = "regular"
    MailPermissionRequest  MailType = "permission_request"
    MailPermissionResponse MailType = "permission_response"
    MailShutdown           MailType = "shutdown"
)

func NewMailbox(teamName, baseDir string) *Mailbox

// Send 发送消息到指定 agent 的 inbox（带文件锁）。
func (m *Mailbox) Send(to string, msg MailMessage) error

// ReadUnread 读取指定 agent 的未读消息。
func (m *Mailbox) ReadUnread(agentName string) ([]MailMessage, error)

// Broadcast 向团队所有成员广播消息。
func (m *Mailbox) Broadcast(msg MailMessage) error
```

### 7.20 共享任务平面

团队创建时自动绑定共享任务列表。Teammate 可以 claim 未分配任务，更新任务状态。这是一个真正的 work queue 设计。

```go
// agent/task.go

// TaskList 是团队共享的任务列表。
type TaskList struct {
    teamName string
    baseDir  string
}

type Task struct {
    ID          string     `json:"id"`
    Subject     string     `json:"subject"`
    Description string     `json:"description"`
    Status      TaskStatus `json:"status"`
    Owner       string     `json:"owner"` // agent name，空 = 未分配
    CreatedAt   time.Time  `json:"created_at"`
    UpdatedAt   time.Time  `json:"updated_at"`
}

type TaskStatus string

const (
    TaskPending    TaskStatus = "pending"
    TaskInProgress TaskStatus = "in_progress"
    TaskCompleted  TaskStatus = "completed"
)

func NewTaskList(teamName, baseDir string) *TaskList
func (t *TaskList) Create(task *Task) error
func (t *TaskList) Claim(taskID, agentName string) error
func (t *TaskList) Update(taskID string, updates TaskUpdate) error
func (t *TaskList) List() ([]*Task, error)
```

### 7.21 权限桥接

Teammate 不拥有独立权限 UI。权限请求回流到 leader，保持用户对整个 swarm 的统一控制。

```go
// agent/permission_bridge.go

// PermissionBridge 将 teammate 的权限请求桥接到 leader。
type PermissionBridge interface {
    RequestPermission(ctx context.Context, req PermissionRequest) (permission.Decision, error)
}

// NewInProcessBridge 同进程权限桥接。
// Teammate 的权限请求直接进入 leader 的权限决策队列，UI 上带 workerBadge 标识来源。
func NewInProcessBridge(leaderQueue chan<- PermissionRequest) PermissionBridge

// NewMailboxBridge 跨进程权限桥接。
// 通过 mailbox 发送 permission_request，等待 leader 的 permission_response。
func NewMailboxBridge(mailbox *Mailbox, leaderName string) PermissionBridge
```

### 7.22 Teammate 拓扑约束

参考 Claude Code 的工程化约束，防止 agent graph 失控：

- **Teammate 不能 spawn 其他 teammate** — 防止无限嵌套
- **In-process teammate 不能启动 background agent** — 避免资源失控
- **Teammate 工具池强制注入协作工具** — SendMessage、TaskCreate/Update/List 是 swarm runtime contract

### 7.23 Swarm 协作工具

Teammate 的工具池会被强制注入以下协作工具：

| 工具 | 说明 |
|------|------|
| `SendMessageTool` | 发送消息给其他 teammate 或 leader |
| `TaskCreateTool` | 创建新任务 |
| `TaskUpdateTool` | 更新任务状态（claim / complete） |
| `TaskListTool` | 列出所有任务 |
| `TaskStopTool` | 停止正在运行的任务 |

---

## 三层 Multi-Agent 体系

参考 Claude Code 源码分析，Blades 的 multi-agent 设计分为三个层级，每层基于前一层构建：

| 层级 | 名称 | 入口 | 特点 | 适用场景 |
|------|------|------|------|---------|
| L1 | SubAgent | `AgentTool` + `subagent_type` | 单个子 agent，同步或后台，隔离上下文 | 独立的搜索、分析、验证任务 |
| L2 | Coordinator | `agent.NewCoordinator()` | 主线程变调度器，worker 结果以 notification 回流，显式分相工作流 | 复杂任务分解与并行执行 |
| L3 | Swarm/Team | `TeamCreateTool` + `Mailbox` + `TaskList` | 显式团队实体，持久化状态，共享任务列表，mailbox 通信，权限桥接 | 大规模多 agent 协作 |

**层级关系：**

```
L3 Swarm/Team
  └── 基于 L2 + 持久化状态（team file + task list + mailbox + permission bridge）
L2 Coordinator
  └── 基于 L1 + coordinator system prompt + task notification + 工作流分相
L1 SubAgent
  └── 基于 ForkAgent + AgentTool + Role Registry
```

**关键设计决策：**

9. **三层渐进式 Multi-Agent** — L1 是基础原语，L2 是模式化使用（coordinator prompt + notification），L3 增加持久化协作状态。用户按需选择复杂度层级。

10. **Coordinator 是运行模式，不是 Agent 角色** — 通过 system prompt 重写主线程角色，不引入新的 Agent 接口。

11. **Swarm 通信基于文件** — Mailbox 使用文件 + 锁实现，简单可靠，支持跨进程通信。不引入消息队列等重依赖。

12. **权限统一回流到 leader** — Teammate 不拥有独立权限 UI，所有权限请求通过 bridge 回到 leader，保持用户对整个 swarm 的统一控制。
