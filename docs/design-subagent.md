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
| 子 Agent | NewAgentTool 包装 | Spawn 共享缓存 + 多种派生模式 |
| 缓存共享 | 无 | 共享父 Agent 的 prompt cache 前缀 |
| 后台执行 | 无 | BackgroundAgent fire-and-forget |
| 隔离模式 | 仅 session 隔离 | Session / Worktree / Remote |
| 来源标记 | 无 | QuerySource 区分行为 |

### 7.1 Spawn 配置

子 Agent 通过 `blades.Spawn(parent, child, opts...)` 创建。child 可以是任意实现了 `blades.Agent` 接口的 Agent（预设 Agent、自定义 Agent、组合 Agent）。配置通过 `SpawnOption` 注入，采用 Go 惯用的 Options 模式。

```go
// SpawnOption 控制子 Agent 的派生方式。
type SpawnOption func(*spawnConfig)

type spawnConfig struct {
    ShareCachePrefix bool
    IsolateSession   bool
    QuerySource      QuerySource
    Tools            []tools.Tool
    MaxTurns         int
    PermissionMode   permission.Mode
    Model            model.Provider
    Background       bool
    Hooks            []hook.Registration
}

// 常用 SpawnOption
func WithShareCache() SpawnOption        // 共享父 Agent prompt cache 前缀
func WithIsolatedSession() SpawnOption   // 创建独立 session
func WithMaxTurns(n int) SpawnOption     // 限制最大轮次
func WithBackground() SpawnOption        // fire-and-forget 后台运行
func WithQuerySource(src QuerySource) SpawnOption
func WithPermissionMode(mode permission.Mode) SpawnOption
func WithModel(m model.Provider) SpawnOption
func WithTools(tools ...tools.Tool) SpawnOption

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

### 7.2 Spawn

```go
// Spawn 从任意 Agent 创建子 Agent。
// child 可以是 agents.Explore()、agents.Plan()、blades.New("custom", ...) 等任意 Agent。
// 当 ShareCachePrefix=true 时，子 Agent 的 system prompt 构建为
// 与父 Agent 共享静态前缀，使 LLM Provider 可以命中 prompt cache。
func Spawn(parent Agent, child Agent, opts ...SpawnOption) Agent

// 内部实现：
// 1. 解析 SpawnOption，构建 spawnConfig
// 2. 如果需要共享缓存，克隆父 Agent 的 PromptBuilder（共享静态 sections）
// 3. 替换动态 sections（子 Agent 可能有不同的 Memory/环境）
// 4. 根据 config 覆盖工具集、权限、模型
// 5. 如果 IsolateSession=true，创建新 session
// 6. 如果 Background=true，包装为 BackgroundAgent
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
    parent Agent, child Agent, config WorktreeConfig, opts ...SpawnOption,
) (agent Agent, cleanup func() error, err error)

// 内部实现：
// 1. git worktree add <baseDir>/<name> -b <name> <baseBranch>
// 2. 设置子 Agent 的 CWD 为 worktree 路径
// 3. cleanup 函数：git worktree remove <path>
```

### 7.5 子 Agent 执行流程

```
1. 解析 SpawnOption，构建 spawnConfig
2. 构建子 Agent system prompt（共享静态前缀）
3. 创建子 Agent 上下文
   - 同步 Agent：共享 AbortController
   - 异步 Agent：隔离的 AbortController
4. 发射 HookSubagentStart
5. 注册子 Agent 专属 Hook（生命周期作用域）
6. 预加载 Skill（如果指定）
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

`blades.Tool(agent)` 将 Agent 包装为 Tool。主 Agent 通过 `subagent_type` 参数（或直接传入 Agent 实例）决定路由目标：

```
1. subagent_type 指定预设名 → 从 agents/ 查找对应构造函数 → Spawn
2. subagent_type 为空 + Agent 实例直接传入 → 直接 Spawn
3. Background=true → BackgroundAgent
4. Worktree 配置 → CreateWorktreeAgent
5. 默认 → 同步 Spawn
```

#### Implicit Fork（隐式 fork）

当无需特定 Agent 类型、只需在子 Agent 中延续当前对话时，使用默认定制：

```go
// 隐式 fork：继承父级完整上下文，不改变 Agent 配置
// 子 agent 继承父级完整对话上下文和已渲染的 system prompt 字节
// 保证 prompt cache 命中稳定性。
func implicitFork(parent Agent, opts ...SpawnOption) Agent {
    return Spawn(parent, parent, append([]SpawnOption{
        WithShareCache(),
        WithQuerySource(QuerySourceSubAgent),
        WithPermissionMode(permission.ModeBubble),
    }, opts...)...)
}
```

### 关键设计决策

1. **共享 Prompt Cache** — 当前子 Agent 完全隔离，每次调用都是冷缓存。新设计通过共享静态 system prompt 前缀，使子 Agent 可以命中父 Agent 的 prompt cache，压缩和 Memory 提取等高频操作成本大幅降低。

2. **Fire-and-forget 后台 Agent** — Memory 提取和任务摘要不需要阻塞主循环。BackgroundAgent 在 goroutine 中运行，主循环继续处理用户请求。Drain 机制确保关闭前等待后台任务完成。

3. **QuerySource 行为区分** — 不同来源的 fork 有不同的行为约束。例如 `compact` fork 只需要生成摘要，不需要执行工具；`extract_memory` fork 只能使用只读工具 + Memory 写入工具。QuerySource 标记使这些约束可以在权限链和 Hook 中精确匹配。

3.1. **隐式 fork 使用 ModeBubble** — 隐式 fork 继承父级完整上下文，因此权限决策应与父级保持一致。Bubble 模式确保父级的权限链处理所有决策，避免子 agent 产生与父级不一致的权限行为。如果子 agent 独立决策权限，可能出现父级已拒绝但子 agent 允许的情况，破坏安全边界。

---

## 预设 Agent

框架在 `agents/` 包中提供 4 种预设 Agent，覆盖搜索→规划→执行→验证的完整工作流。预设 Agent 是返回 `blades.Agent` 的构造函数，与 `blades.New("custom", ...)` 返回同一种东西。

没有 Role、没有工厂、没有 Registry——"一切皆 Agent"。

### 7.7 预设 Agent 概览

| 构造函数 | 工具约束 | MaxTurns | 用途 |
|----------|---------|----------|------|
| `agents.Explore()` | ReadOnlyTools | 5 | 快速只读代码搜索 |
| `agents.Plan()` | ReadOnlyTools | 15 | 架构设计与实现规划 |
| `agents.General()` | 全部继承 | 继承 | 全能力通用 Agent |
| `agents.Verify()` | ReadOnlyTools + /tmp | 20 | 对抗性验证 |

### 7.8 预设 Agent 实现

每个预设 Agent 内部调用 `blades.New()` + 预设 `AgentOption`，与用户自定义 Agent 走相同的代码路径。

```go
// agents/explore.go

// Explore returns a fast read-only code search agent.
func Explore(opts ...blades.AgentOption) blades.Agent {
    defaults := []blades.AgentOption{
        blades.WithDescription(
            "Fast read-only search agent for locating code. " +
            "Use it to find files by pattern, grep for symbols, " +
            "or answer 'where is X defined'."),
        blades.WithToolFilter(tools.ReadOnlyTools()),
        blades.WithMaxTurns(5),
    }
    return blades.New("explore", append(defaults, opts...)...)
}
```

```go
// agents/plan.go

// Plan returns a software architect agent for designing implementation plans.
func Plan(opts ...blades.AgentOption) blades.Agent {
    defaults := []blades.AgentOption{
        blades.WithDescription(
            "Software architect agent for designing implementation plans. " +
            "Returns step-by-step plans, identifies critical files, and " +
            "considers architectural trade-offs."),
        blades.WithToolFilter(tools.ReadOnlyTools()),
        blades.WithMaxTurns(15),
    }
    return blades.New("plan", append(defaults, opts...)...)
}
```

```go
// agents/general.go

// General returns a full-capability general-purpose agent.
func General(opts ...blades.AgentOption) blades.Agent {
    return blades.New("general", opts...)
}
```

```go
// agents/verify.go

// Verify returns a verification specialist that tries to break
// the implementation.
func Verify(opts ...blades.AgentOption) blades.Agent {
    defaults := []blades.AgentOption{
        blades.WithDescription(
            "Verification specialist that tries to break the implementation. " +
            "Runs builds, tests, linters, and adversarial probes."),
        blades.WithToolFilter(tools.And(
            tools.ReadOnlyTools(),
            tools.AllowOnly("bash"),
        )),
        blades.WithMaxTurns(20),
    }
    return blades.New("verify", append(defaults, opts...)...)
}
```

### 7.9 用户自定义 Agent

用户用 `blades.New()` + `AgentOption` 创建自定义 Agent。与预设 Agent 完全相同的机制：

```go
codeReviewer := blades.New("code-review",
    blades.WithDescription("Specialized code review agent"),
    blades.WithToolFilter(tools.ReadOnlyTools()),
    blades.WithMaxTurns(15),
    blades.WithSystemPrompt(`You are a code review specialist. Focus on:
- Correctness: logic errors, off-by-one, nil dereferences
- Security: injection, auth bypass, data exposure
- Performance: unnecessary allocations, N+1 queries
- Maintainability: naming, structure, test coverage

Report findings with file:line references.`),
)

// 直接使用
codeReviewer.Run(ctx, input)

// 或作为子 Agent
sub := blades.Spawn(parent, codeReviewer, blades.WithShareCache())
```

### 7.10 组合原语

`blades.Sequential`、`blades.Parallel`、`blades.Loop` 在根包中，与 `Agent` 接口同包。它们是 Agent 的基础组合能力，类似 `io.MultiReader` 之于 `io.Reader`。

```go
// 顺序组合
pipeline := blades.Sequential(
    agents.Explore(blades.WithModel(fastModel)),
    agents.Plan(),
    codeReviewer,
)

// 并行组合
parallel := blades.Parallel(
    agents.Explore(),
    agents.Verify(),
)
```

### 7.11 ToolFilter（tools/ 包）

`ToolFilter` 是 `tools` 包的函数类型，纯工具操作，不依赖 `blades`。支持组合器：

```go
// tools/filter.go
type ToolFilter func(Tool) bool

func ReadOnlyTools() ToolFilter                // 保留实现了 ReadOnlyTool 接口的工具
func AllowOnly(names ...string) ToolFilter      // 白名单
func Disallow(names ...string) ToolFilter       // 黑名单
func And(filters ...ToolFilter) ToolFilter      // 全部满足
func Or(filters ...ToolFilter) ToolFilter       // 至少一个满足
func FilterTools(all []Tool, f ToolFilter) []Tool
```

### 关键设计决策

1. **一切皆 Agent** — 预设 Agent 是返回 `blades.Agent` 的普通函数，与 `blades.New()` 返回值完全相同。无 Role 工厂、无 ForkConfig、无 Registry。

2. **构造函数 + AgentOption 模式** — 预设 Agent 内部调用 `blades.New()`，通过 `AgentOption` 注入默认配置。用户可通过传入额外 `AgentOption` 覆盖默认值。

3. **ToolFilter 在 tools/ 包** — 纯工具操作，`tools.ToolFilter` 不依赖 `blades`。`tools.ReadOnlyTools()` 利用 `tools.ReadOnlyTool` 可选接口动态检测。

4. **组合原语在根包** — `blades.Sequential/Parallel/Loop` 与 `Agent` 接口同包，是 Agent 的基础能力。类似 Go 标准库 `io.MultiReader`。

---

## Coordinator 模式

Coordinator 不是新的 Agent 类型，而是一种**运行模式**——把主 Agent 从"执行者"切换为"调度器"。主线程负责分解任务、派出 worker、汇总结果。实现在 `team/` 包中。

> 详细设计 → [design-team.md](design-team.md)

### 7.12 Coordinator 配置（team/ 包）

```go
// team/coordinator.go

// CoordinatorConfig 配置 coordinator 模式。
type CoordinatorConfig struct {
    // Workers 可用的 worker Agent 列表。
    Workers []blades.Agent

    // MaxWorkers 最大并发 worker 数。0 = 不限制。
    MaxWorkers int
}

// NewCoordinator 创建 coordinator agent。
// 内部：生成 coordinator system prompt + 配置 AgentTool/SendMessageTool/TaskStopTool
func NewCoordinator(name string, config CoordinatorConfig, opts ...blades.AgentOption) (blades.Agent, error)
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
// team/scratchpad.go

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

Swarm 是比 Coordinator 更进一步的团队协作模式：持久化的团队实体，有共享任务列表和 agent 间通信。实现在 `team/` 包中。

> 详细设计 → [design-team.md](design-team.md)

### 7.13 核心抽象（team/ 包）

```go
// team/team.go
type Team struct {
    Name, Description string
    LeadID  string
    Members []Member
}
type TeamStore interface { Create, Get, AddMember, RemoveMember, Delete }

// team/mailbox.go
type Mailbox struct { ... }
func (m *Mailbox) Send(to string, msg MailMessage) error
func (m *Mailbox) ReadUnread(agentName string) ([]MailMessage, error)

// team/task.go
type TaskList struct { ... }
type Task struct { ID, Subject, Description string; Status TaskStatus; Owner string }

// team/bridge.go
type PermissionBridge interface {
    RequestPermission(ctx, req) (permission.Decision, error)
}
```

### 7.14 Teammate 拓扑约束

- **Teammate 不能 spawn 其他 teammate** — 防止无限嵌套
- **In-process teammate 不能启动 background agent** — 避免资源失控
- **Teammate 工具池强制注入协作工具** — SendMessage、TaskCreate/Update/List

---

## 三层 Multi-Agent 体系

Blades 的 multi-agent 设计分为三个层级，每层基于前一层构建：

| 层级 | 名称 | 入口 | 特点 | 包 |
|------|------|------|------|-----|
| L1 | SubAgent | `blades.Spawn()` + `blades.Tool()` | 单个子 agent，同步或后台，隔离上下文 | `blades/` |
| L2 | Coordinator | `team.NewCoordinator()` | 主线程变调度器，worker 结果以 notification 回流 | `team/` |
| L3 | Swarm/Team | `TeamCreateTool` + `Mailbox` + `TaskList` | 显式团队实体，持久化状态，mailbox 通信 | `team/` |

**层级关系：**

```
L3 Swarm/Team
  └── 基于 L2 + 持久化状态（team file + task list + mailbox + permission bridge）
L2 Coordinator
  └── 基于 L1 + coordinator system prompt + task notification + 工作流分相
L1 SubAgent
  └── 基于 blades.Spawn + blades.Tool(agent) + agents/ 预设 Agent
```

**关键设计决策：**

1. **三层渐进式 Multi-Agent** — L1 是基础原语（根包），L2/L3 在 `team/` 包中。用户按需选择复杂度层级。

2. **Coordinator 是运行模式，不是 Agent 类型** — 通过 system prompt 重写主线程角色，不引入新的 Agent 接口。

3. **Swarm 通信基于文件** — Mailbox 使用文件 + 锁实现，简单可靠，支持跨进程通信。

4. **权限统一回流到 leader** — Teammate 不拥有独立权限 UI，所有权限请求通过 bridge 回到 leader。
