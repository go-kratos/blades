---
type: design
title: Blades Agent Framework 蓝图设计
date: 2026-04-30
status: draft
author: chenzhihui
related: [reference-claude-code-agent.md, reference-pi-agent-framework.md, design-streaming-optimization.md]
tags: [agent, framework, architecture, core, tools, context, session, sandbox]
---

# Blades Agent Framework 蓝图设计

## 概述

本文档是 Blades Agent Framework 的全新蓝图设计，围绕六大支柱展开。融合 Claude Code 的工程实践（不可变状态、缓存感知压缩、并发工具分区）和 pi-agent 的架构理念（纯函数循环、三层分离、两阶段上下文转换），充分利用 Go 语言特性（接口组合、`context.Context`、`iter.Seq2`、goroutine 并发）。

### 设计原则

1. **每轮状态不可变** — 每次循环迭代产生新的 `TurnSnapshot` 快照，不原地修改
2. **无状态循环 + 有状态包装分离** — `RunLoop()` 不持有可变状态，副作用通过回调注入；`llmAgent` 管理 Session 等有状态资源
3. **分层架构** — Provider → Core → Application，严格单向依赖
4. **事件驱动生命周期** — 通过 Hook 系统实现可扩展的生命周期管理
5. **缓存感知上下文管理** — 多策略分层压缩，prompt cache 友好
6. **并发工具执行** — 工具自声明并发安全性，编排器自动分区
7. **可插拔执行后端** — 本地、Docker、SSH 统一抽象

### 整体架构

```
┌─────────────────────────────────────────────────────────────────┐
│                      Application Layer                          │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌───────────────────┐  │
│  │  Recipe   │ │  Skills  │ │  Memory  │ │  CLI / SDK / REPL │  │
│  └──────────┘ └──────────┘ └──────────┘ └───────────────────┘  │
├─────────────────────────────────────────────────────────────────┤
│                        Core Layer                               │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                    Agent Loop (RunLoop)                   │   │
│  │  ┌─────────┐ ┌──────────┐ ┌──────────┐ ┌────────────┐   │   │
│  │  │ Context  │ │  Tools   │ │  Events  │ │  Session   │   │   │
│  │  │ Manager  │ │ Executor │ │  System  │ │  Manager   │   │   │
│  │  └─────────┘ └──────────┘ └──────────┘ └────────────┘   │   │
│  ├──────────────────────────────────────────────────────────┤   │
│  │  Interfaces (定义在 Core，实现在各层)                     │   │
│  │  ┌──────────────┐ ┌──────────────┐ ┌────────────────┐   │   │
│  │  │ModelProvider │ │  Executor    │ │ SessionStore   │   │   │
│  │  │  (interface) │ │  (interface) │ │  (interface)   │   │   │
│  │  └──────────────┘ └──────────────┘ └────────────────┘   │   │
│  ├──────────────────────────────────────────────────────────┤   │
│  │  Flow Orchestration                                      │   │
│  │  ┌────────┐ ┌────────┐ ┌──────┐ ┌────────┐ ┌──────┐    │   │
│  │  │Sequent.│ │Parallel│ │ Loop │ │Routing │ │ Deep │    │   │
│  │  └────────┘ └────────┘ └──────┘ └────────┘ └──────┘    │   │
│  ├──────────────────────────────────────────────────────────┤   │
│  │  Graph DAG Engine                                        │   │
│  └──────────────────────────────────────────────────────────┘   │
├─────────────────────────────────────────────────────────────────┤
│                  Provider Layer (具体实现)                       │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────────┐   │
│  │ Anthropic│ │  OpenAI  │ │  Gemini  │ │  Custom Provider │   │
│  └──────────┘ └──────────┘ └──────────┘ └──────────────────┘   │
├─────────────────────────────────────────────────────────────────┤
│                  Sandbox Layer (具体实现)                        │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐                        │
│  │  Local   │ │  Docker  │ │   SSH    │                        │
│  └──────────┘ └──────────┘ └──────────┘                        │
└─────────────────────────────────────────────────────────────────┘

依赖方向: Application → Core ← Provider/Sandbox
Core 定义接口，Provider/Sandbox 提供实现（依赖反转）。
```

---

## 支柱一：Agent Loop & State & Tools & Skills

### 设计目标

Agent Loop 是整个框架的心脏。将循环拆分为 **无状态 `RunLoop()`** 和 **有状态 `llmAgent` 包装器** 两部分，使循环逻辑可用 mock 回调独立测试，同时保持 Go 惯用的 `iter.Seq2` 流式接口。

### 1.1 TurnSnapshot — 不可变轮次快照

`TurnSnapshot` 是循环内部的轮次快照，与 `Session.State()` 返回的 `State`（`map[string]any`）不同：
`State` 是跨 Run 持久化的键值对，`TurnSnapshot` 是单轮的只读视图，循环结束即丢弃。

```go
// TurnSnapshot 是每轮迭代的不可变快照。
// 每次循环产生新的 TurnSnapshot，不原地修改。
type TurnSnapshot struct {
    Messages    []*Message     // 当前对话消息（不可变切片）
    TurnCount   int            // 当前轮次
    Transition  Transition     // 状态转换信号
    TokenUsage  TokenUsage     // 累计 token 用量
    Metadata    map[string]any // 扩展元数据
}

// Transition 表示循环的状态转换。实现 fmt.Stringer 以便日志输出。
type Transition int

const (
    TransitionContinue  Transition = iota // 继续下一轮
    TransitionCompleted                    // 正常完成
    TransitionAborted                      // 中止
    TransitionMaxTurns                     // 达到最大轮次
    TransitionHookStopped                  // Hook 中止
    TransitionOverflow                     // 上下文超限
    TransitionModelError                   // 模型错误
    TransitionEscalated                    // 升级到外层循环
)

func (t Transition) String() string // go:generate stringer
```

### 1.2 RunLoop — 无状态核心循环

`RunLoop` 本身不持有可变状态，也不直接操作 Session。它通过回调将副作用（session 持久化、
hook 触发）推给调用方。这使得循环逻辑可以用 mock 回调独立测试。

```go
// LoopConfig 是 RunLoop 的配置。
type LoopConfig struct {
    MaxIterations    int
    MaxTokens        int64
    Model            ModelProvider
    Tools            []tools.Tool
    Instruction      *Message
    InputSchema      *jsonschema.Schema
    OutputSchema     *jsonschema.Schema
    ConcurrencyLimit int // 工具最大并发数，默认 10
}

// LoopCallbacks 将副作用从循环中解耦。
// llmAgent.Run 提供真实实现，测试时替换为 mock。
type LoopCallbacks struct {
    // EmitEvent 触发生命周期事件，返回 HookResult 控制后续行为。
    EmitEvent func(ctx context.Context, event Event) (HookResult, error)
    // PrepareMessages 执行上下文转换和压缩，返回模型输入消息。
    PrepareMessages func(ctx context.Context, snap TurnSnapshot) ([]*Message, error)
    // OnMessage 在循环产生消息后调用，负责 session.Append 等持久化。
    OnMessage func(ctx context.Context, msg *Message) error
}

// RunLoop 是核心循环的无状态实现。
// 不直接操作 Session，所有副作用通过 LoopCallbacks 注入。
// 通过 iter.Seq2 yield 每轮产生的消息。
func RunLoop(ctx context.Context, config LoopConfig, callbacks LoopCallbacks, initial TurnSnapshot) Generator[*Message, error]
```

Session 持久化边界：

```
RunLoop                              llmAgent.Run
  │                                     │
  ├── yield(assistantMsg) ──────────→  callbacks.OnMessage(assistantMsg)
  │                                     └── session.Append(ctx, assistantMsg)
  ├── yield(toolResultMsg) ─────────→  callbacks.OnMessage(toolResultMsg)
  │                                     └── session.Append(ctx, toolResultMsg)
  └── (RunLoop 不感知 Session)
```

循环流程：

```
RunLoop(ctx, config, callbacks, initialSnapshot)
  │
  ├── for turn := 0; turn < config.MaxIterations; turn++
  │     │
  │     ├── 1. PrepareMessages ────────── callbacks.PrepareMessages(snapshot)
  │     │     ├── TransformContext()     Agent 消息层面：裁剪、注入、压缩
  │     │     └── ConvertToLLM()        转换为 LLM 可理解的标准消息
  │     │
  │     ├── 2. EmitEvent(PreGenerate)
  │     │
  │     ├── 3. Model Generate ─────────── 流式 API 调用
  │     │     └── yield MessageUpdate（流式 delta）
  │     │
  │     ├── 4. EmitEvent(PostGenerate)
  │     │
  │     ├── 5. Execute Tools ──────────── 并发 + 串行分区执行
  │     │     ├── EmitEvent(PreToolUse)  每个工具调用前
  │     │     ├── PartitionToolCalls()   按 ConcurrencySafe 分区
  │     │     ├── RunConcurrent()        并发安全组并行执行
  │     │     ├── RunSequential()        串行组顺序执行
  │     │     └── EmitEvent(PostToolUse) 每个工具调用后
  │     │
  │     ├── 6. callbacks.OnMessage ────── 调用方负责 session 持久化
  │     │
  │     ├── 7. Build Next TurnSnapshot ── 产生新的不可变快照
  │     │
  │     └── 8. Check Transition ──────── 判断是否继续
  │
  └── return Terminal
```

恢复路径（在 `RunLoop` 内部处理，不经过压缩管道）：

| 错误场景 | 恢复策略 |
|---------|---------|
| `max_output_tokens` | 升级到更大输出限制，重试当前轮 |
| 多轮失败 | 最多 3 次续接尝试 |
| `prompt_too_long` (API 413) | 调用 `ReactiveCompact` 紧急压缩后重试（不在常规管道中） |
| 上下文耗尽 | Fork 子 Agent 继续执行 |

错误类型：

```go
var (
    ErrModelProviderRequired = errors.New("blades: model provider is required")
    ErrMaxIterationsExceeded = errors.New("blades: maximum iterations exceeded")
    ErrNoFinalResponse       = errors.New("blades: no final response from model")
    ErrHookStopped           = errors.New("blades: hook stopped execution")
    ErrPermissionDenied      = errors.New("blades: permission denied")
    ErrContextOverflow       = errors.New("blades: context window overflow")
    ErrLoopEscalated         = errors.New("blades: loop escalated to outer agent")
    ErrSpawnFailed           = errors.New("blades: failed to spawn sub-agent")
    ErrExecTimeout           = errors.New("blades: executor timed out")
)
```

### 1.3 Agent 与 Runner

```go
// Agent 接口 — 框架的统一抽象。
// llmAgent、SequentialAgent、RoutingAgent 等均实现此接口。
type Agent interface {
    Name() string
    Description() string
    Run(context.Context, *Invocation) Generator[*Message, error]
}

// Invocation 携带单次调用的完整上下文。
type Invocation struct {
    ID                string
    Model             string
    Resume            bool
    Stream            bool
    Session           Session
    Instruction       *Message
    Message           *Message
    EphemeralMessages []*Message   // 仅追加到下一次模型请求，不持久化
    Tools             []tools.Tool
    committed         *atomic.Bool // 跨克隆共享，保证 exactly-once append
}

// Runner 是应用层的入口，包装 Agent 并管理 Session 生命周期。
// 提供 Run（同步）和 RunStream（流式）两个便捷方法。
type Runner struct {
    rootAgent Agent
}

func NewRunner(rootAgent Agent, opts ...RunnerOption) *Runner

// Run 创建 Invocation、建立 Session、调用 Agent.Run、收集最终结果。
func (r *Runner) Run(ctx context.Context, message *Message, opts ...RunOption) (*Message, error)

// RunStream 流式版本，yield 每条消息。
func (r *Runner) RunStream(ctx context.Context, message *Message, opts ...RunOption) Generator[*Message, error]
```

context.Context 传播：

```
context.Context
  ├── SessionContext  — NewSessionContext(ctx, session)
  │                     SessionFromContext(ctx) → Session
  ├── AgentContext    — NewAgentContext(ctx, agent)
  │                     FromAgentContext(ctx) → Agent
  └── ToolContext     — tools.NewContext(ctx, toolCtx)
                        tools.FromContext(ctx) → ToolContext
                        ToolContext.SetAction(key, value) → 控制流信号
```

`ToolContext.SetAction` 是工具向循环发送控制流信号的机制（如 `ExitTool` 设置 `ActionLoopExit`），
与 Hook 系统互补：Hook 在工具执行前拦截，`SetAction` 在工具执行中发出信号，循环在工具执行后检查 `Message.Actions`。

// llmAgent 是唯一直接调用 LLM 的 Agent 实现，包装 RunLoop。
// 与 flow 包中的编排类 Agent（SequentialAgent、RoutingAgent 等）形成对比，
// 它们负责编排，而 llmAgent 负责实际的模型交互。
// Run 方法负责：
// 1. 准备 Invocation（解析工具、注入指令、合并 Skills）
// 2. 构建初始 TurnSnapshot
// 3. 调用 RunLoop() 纯函数
// 4. 处理 session 持久化等副作用
type llmAgent struct {
    name                string
    description         string
    instruction         string
    instructionProvider InstructionProvider
    outputKey           string
    maxIterations       int
    model               ModelProvider
    inputSchema         *jsonschema.Schema
    outputSchema        *jsonschema.Schema
    middlewares         []Middleware
    tools               []tools.Tool
    skills              []skills.Skill
    skillToolset        *skills.Toolset
    toolsResolver       tools.Resolver
    useContext          bool
    hooks               *HookRegistry
    contextManager      ContextManager
}
```

Middleware 链保持 Kratos 风格：

```go
type Handler interface {
    Handle(context.Context, *Invocation) Generator[*Message, error]
}

type Middleware func(Handler) Handler

func ChainMiddlewares(mws ...Middleware) Middleware
```

### 1.4 工具系统

```go
package tools

// Tool 接口 — 保持与现有代码兼容的核心接口。
type Tool interface {
    Name() string
    Description() string
    InputSchema() *jsonschema.Schema
    OutputSchema() *jsonschema.Schema
    Handler
}

// ConcurrencyDeclarer 是可选接口，声明工具是否可并发执行。
// 未实现此接口的工具默认为串行执行。
type ConcurrencyDeclarer interface {
    ConcurrencySafe() bool
}

// ReadOnlyDeclarer 是可选接口，声明工具是否为只读操作。
// 未实现此接口的工具默认为非只读。
// 用于权限系统和沙箱决策。
type ReadOnlyDeclarer interface {
    ReadOnly() bool
}

// IsConcurrencySafe 检查工具是否声明了并发安全。
// 未实现 ConcurrencyDeclarer 的工具返回 false。
func IsConcurrencySafe(t Tool) bool {
    if d, ok := t.(ConcurrencyDeclarer); ok {
        return d.ConcurrencySafe()
    }
    return false
}

// IsReadOnly 检查工具是否声明了只读。
func IsReadOnly(t Tool) bool {
    if d, ok := t.(ReadOnlyDeclarer); ok {
        return d.ReadOnly()
    }
    return false
}

// StreamingHandler 支持工具执行过程中的流式进度更新。
type StreamingHandler interface {
    HandleStream(ctx context.Context, input string, onProgress func(ProgressUpdate)) (string, error)
}

// ProgressUpdate 是工具执行过程中的进度更新。
type ProgressUpdate struct {
    Status  string  // "running", "progress", "completed", "error"
    Message string
    Percent float64 // 0.0 ~ 1.0, -1 表示不确定
}

// Resolver 动态解析工具（MCP servers、plugins 等）。
type Resolver interface {
    Resolve(ctx context.Context) ([]Tool, error)
}
```

工具并发分区：

```
输入: [bash, read, read, edit, grep, grep]

PartitionToolCalls() 按 ConcurrencySafe() 分区：

  [bash]        → serial    (ConcurrencySafe=false)
  [read, read]  → concurrent (ConcurrencySafe=true, Promise.all, max=10)
  [edit]        → serial    (ConcurrencySafe=false)
  [grep, grep]  → concurrent (ConcurrencySafe=true)
```

```go
// ToolPartition 表示一组可以一起执行的工具调用。
type ToolPartition struct {
    Calls      []ToolCall
    Concurrent bool
}

// PartitionToolCalls 根据工具的 ConcurrencySafe 声明将连续的工具调用分区。
func PartitionToolCalls(calls []ToolCall, registry map[string]Tool) []ToolPartition
```

### 1.5 Skills 系统

Skills 是 Tool 的高级抽象层，从 `SKILL.md` 加载自包含的能力单元：

```go
package skills

// Skill 是自包含的能力单元。
type Skill interface {
    Name() string
    Description() string
    Instruction() string
}

// FrontmatterProvider 提供 SKILL.md 的 YAML frontmatter 元数据。
type FrontmatterProvider interface {
    Frontmatter() Frontmatter
}

// ResourcesProvider 提供 skill 的资源文件。
type ResourcesProvider interface {
    Resources() Resources
}

// Toolset 将 skills 组合为工具集，提供四个核心工具：
// - list_skills     列出所有可用 skills
// - load_skill      加载指定 skill 的指令
// - load_skill_resource  加载 skill 的资源文件
// - run_skill_script     在临时目录中执行 skill 脚本
type Toolset struct { ... }

// ComposeTools 合并基础工具和 skill 工具，应用 allowed-tools glob 过滤。
func (t *Toolset) ComposeTools(base []tools.Tool) []tools.Tool
```

### 1.6 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 循环实现 | 无状态 `RunLoop()` + 有状态 `llmAgent` 包装 | 循环逻辑可用 mock 回调独立测试（来自 pi-agent） |
| 状态管理 | 每轮不可变 `TurnSnapshot` | 避免并发修改，简化调试和回放（来自 Claude Code） |
| 副作用边界 | `LoopCallbacks` 回调注入 | RunLoop 不感知 Session，持久化由调用方负责 |
| 流式接口 | `iter.Seq2[*Message, error]` | Go 1.23+ 惯用模式 |
| 工具并发 | 可选接口 `ConcurrencyDeclarer` | 向后兼容，编排器通过类型断言检查（来自 Claude Code） |
| 工具分区 | 连续并发安全工具分组并行 | 最大化并行度，保证串行工具的执行顺序 |
| Skills | 独立于 Tool 的高级抽象 | 支持自包含的能力包分发和加载 |

---

## 支柱二：MainAgent & SubAgent & Coordinator

### 设计目标

提供从简单的顺序执行到复杂的层级任务分解的完整编排能力，支持缓存感知的 Agent fork 以降低子 Agent 的冷启动成本。

### 2.1 Coordinator

```go
// Coordinator 管理子 Agent 的生命周期、通信和资源共享。
type Coordinator interface {
    // Spawn 创建并启动一个子 Agent。
    Spawn(ctx context.Context, config SpawnConfig) (AgentHandle, error)
    // Wait 等待指定 Agent 完成。
    Wait(ctx context.Context, handle AgentHandle) (*Message, error)
    // Cancel 取消指定 Agent 的执行。
    Cancel(handle AgentHandle) error
}

// SpawnConfig 定义子 Agent 的创建配置。
type SpawnConfig struct {
    Agent       Agent
    Message     *Message
    Strategy    SpawnStrategy
    Session     Session       // nil = 隔离 session
    Instruction *Message      // 额外指令注入
}

// SpawnStrategy 定义子 Agent 的派生策略。
type SpawnStrategy int

const (
    Synchronous SpawnStrategy = iota // 同步执行，阻塞父 Agent
    Background                       // 后台执行，不阻塞父 Agent
    Forked                           // 缓存感知 fork，共享 prompt cache 前缀
    Isolated                         // 完全隔离（独立 session、独立沙箱）
)

// AgentHandle 是子 Agent 的句柄。
type AgentHandle struct {
    ID       string
    Agent    Agent
    Strategy SpawnStrategy
    Done     <-chan struct{}
}
```

默认 Coordinator 实现：

```go
// defaultCoordinator 是 Coordinator 的默认实现。
type defaultCoordinator struct {
    mu      sync.Mutex
    handles map[string]*agentTask
}

// Spawn 根据 Strategy 选择执行方式：
// - Synchronous: 在当前 goroutine 调用 agent.Run，阻塞直到完成
// - Background:  启动新 goroutine + 可取消 context，立即返回 handle
// - Forked:      调用 ForkSession(parentSession) 创建缓存感知子 session，
//                然后按 Synchronous 或 Background 执行
// - Isolated:    创建全新 Session，在独立 context 中执行
//
// Cancel 通过 context.CancelFunc 传播取消信号到运行中的 agent。
// Wait 阻塞在 handle.Done channel 上。
```

### 2.2 Flow 编排模式

```
┌─────────────────────────────────────────────────────┐
│                  Flow Patterns                       │
│                                                      │
│  ┌────────────┐    ┌────────────┐    ┌───────────┐  │
│  │ Sequential │    │  Parallel  │    │   Loop    │  │
│  │  A → B → C │    │  A ┬ B     │    │  ┌→ A ─┐  │  │
│  │            │    │    └ C     │    │  └─────┘  │  │
│  └────────────┘    └────────────┘    └───────────┘  │
│                                                      │
│  ┌────────────┐    ┌─────────────────────────────┐  │
│  │  Routing   │    │          Deep               │  │
│  │  R → A|B|C │    │  Main ──┬── Sub1            │  │
│  │            │    │         ├── Sub2            │  │
│  │            │    │         └── Sub3 ── Sub3.1  │  │
│  └────────────┘    └─────────────────────────────┘  │
└─────────────────────────────────────────────────────┘
```

```go
package flow

// SequentialAgent 按顺序运行子 agent，每个接收 invocation 的克隆。
// 错误短路整个链。
func NewSequentialAgent(name string, agents ...Agent) Agent

// ParallelAgent 并发运行子 agent，通过 errgroup 管理。
// 结果通过 buffered channel 流式返回。第一个错误取消所有 goroutine。
func NewParallelAgent(name string, agents ...Agent) Agent

// LoopAgent 重复运行子 agent，支持 ExitTool 和 LoopCondition。
// 最大迭代次数默认 10。支持 ErrLoopEscalated 升级到外层循环。
func NewLoopAgent(name string, agent Agent, opts ...LoopOption) Agent

// RoutingAgent 使用 LLM 通过 handoff 工具选择目标子 agent。
// 根 agent 运行后，检查 ActionHandoffToAgent 信号委派到子 agent。
func NewRoutingAgent(name string, root Agent, agents ...Agent) Agent

// DeepAgent 层级任务管理器，内置 TodosTool 和 TaskTool。
// 支持将复杂任务分解为子任务并委派给子 agent。
func NewDeepAgent(name string, config DeepConfig) Agent
```

### 2.3 AgentTool — Agent 作为工具

```go
// AgentTool 将 Agent 包装为 Tool，使父 Agent 可以通过工具调用委派任务。
// 子 Agent 在隔离的 session 中运行，避免污染父 Agent 的对话历史。
type AgentTool struct {
    agent    Agent
    strategy SpawnStrategy
}

func NewAgentTool(agent Agent) tools.Tool

// 实现可选接口声明：AgentTool 不可并发、非只读。
func (t *AgentTool) ConcurrencySafe() bool { return false }
func (t *AgentTool) ReadOnly() bool        { return false }

// Handle 执行子 Agent：
// 1. 创建隔离 session
// 2. 根据 strategy 选择执行方式
// 3. 收集子 Agent 的最终输出
func (t *AgentTool) Handle(ctx context.Context, input string) (string, error)
```

### 2.4 缓存感知 Fork

```go
// ForkSession 创建一个共享父 session prompt cache 前缀的子 session。
// 子 session 的消息不会写入父 session，但可以利用父 session 的缓存。
//
// 使以下操作成本低廉：
// - 上下文压缩摘要生成（fork agent 生成摘要）
// - 自动 Memory 提取（fork agent 提取持久化事实）
// - 任务摘要生成
//
// 原理：子 session 的 system prompt 和历史消息前缀与父 session 相同，
// LLM API 的 prompt cache 命中，避免重新计算。
func ForkSession(parent Session, opts ...SessionOption) Session
```

### 2.5 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 子 Agent 隔离 | 默认隔离 session | 避免子 Agent 的内部对话污染父 Agent |
| 缓存感知 fork | 共享 prompt cache 前缀 | 降低子 Agent 冷启动成本（来自 Claude Code） |
| 派生策略 | 4 种（同步/后台/fork/隔离） | 覆盖不同场景，比 Claude Code 的 5 种更精简 |
| Flow 模式 | 5 种（Sequential/Parallel/Loop/Routing/Deep） | 覆盖常见编排需求 |
| Agent 作为 Tool | AgentTool 包装器 | 统一 Agent 和 Tool 的调用方式 |

---

## 支柱三：Context & Compressor

### 设计目标

上下文管理是 Agent 框架中最关键的工程挑战。实现多策略分层压缩，在保持对话质量的同时最大化 prompt cache 命中率。

### 3.1 ContextManager

```go
// ContextManager 是上下文管理的顶层接口。
// 协调两阶段转换和多策略压缩。
type ContextManager interface {
    // PrepareMessages 执行完整的上下文准备流程：
    // 1. TransformContext — Agent 消息层面的转换（裁剪、注入）
    // 2. Compress — 多策略压缩管道
    // 3. ConvertToLLM — 转换为 LLM 可理解的格式
    PrepareMessages(ctx context.Context, snap TurnSnapshot) ([]*Message, error)
}

// ContextTransformer 在 Agent 消息层面操作。
// 用于裁剪、注入、重排等不涉及 LLM 调用的转换。
type ContextTransformer interface {
    TransformContext(ctx context.Context, messages []*Message) ([]*Message, error)
}

// ContextCompressor 压缩、截断或过滤消息列表。
type ContextCompressor interface {
    Compress(ctx context.Context, messages []*Message) ([]*Message, error)
}

// TokenCounter 估算消息的 token 数量。
type TokenCounter interface {
    Count(messages ...*Message) int64
}
```

### 3.2 多策略分层压缩管道

```
┌─────────────────────────────────────────────────────────┐
│           Context Compression Pipeline (主动)            │
│                                                          │
│  输入: []*Message (完整对话历史)                          │
│                                                          │
│  ┌──────────────────────────────────────────────────┐   │
│  │ Stage 1: ToolResultBudget                        │   │
│  │ 裁剪超大工具结果，写入磁盘，发送预览              │   │
│  │ 触发: 每轮开始                                    │   │
│  │ 粒度: 单个工具结果                                │   │
│  └──────────────────────────────────────────────────┘   │
│                        ↓                                 │
│  ┌──────────────────────────────────────────────────┐   │
│  │ Stage 2: SlidingWindow                           │   │
│  │ 丢弃最旧消息，保持在消息数/token 预算内           │   │
│  │ 触发: 每轮开始                                    │   │
│  │ 粒度: 整条消息                                    │   │
│  └──────────────────────────────────────────────────┘   │
│                        ↓                                 │
│  ┌──────────────────────────────────────────────────┐   │
│  │ Stage 3: RollingSummary                          │   │
│  │ 调用 LLM 对历史消息生成滚动摘要                   │   │
│  │ 触发: token 用量超过阈值                          │   │
│  │ 粒度: 批量消息 → 摘要消息                         │   │
│  └──────────────────────────────────────────────────┘   │
│                                                          │
│  输出: []*Message (压缩后的消息列表)                     │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│           ReactiveCompact (被动，错误恢复路径)           │
│                                                          │
│  触发: Model API 返回 prompt_too_long (413) 错误         │
│  不在常规管道中，由 RunLoop 的错误恢复逻辑直接调用       │
│  对全部对话生成 LLM 摘要，然后重试模型调用               │
└─────────────────────────────────────────────────────────┘
```

### 3.3 压缩策略实现

```go
package context

// ToolResultBudget 裁剪超大工具结果。
// 超过 MaxResultChars 的结果持久化到磁盘，向模型发送预览摘要。
type ToolResultBudget struct {
    MaxResultChars int    // 单个工具结果的最大字符数，默认 50000
    StoragePath    string // 超大结果的持久化路径
}

// SlidingWindow 滑动窗口压缩。
// 当消息数或 token 数超限时，丢弃最旧的消息。
type SlidingWindow struct {
    MaxMessages int          // 最大消息数，默认 100
    MaxTokens   int64        // 最大 token 数
    Counter     TokenCounter
}

// RollingSummary 使用 LLM 生成滚动摘要。
// 当工作视图超过 MaxTokens 时，取下一批 BatchSize 条消息，
// 调用 LLM 扩展滚动摘要，推进偏移量。
// 始终保留最近 KeepRecent 条消息的原文。
// 压缩状态持久化到 Session 中，跨 Run 存活。
type RollingSummary struct {
    Model      ModelProvider
    MaxTokens  int64        // 触发压缩的 token 阈值
    KeepRecent int          // 始终保留的最近消息数，默认 10
    BatchSize  int          // 每次压缩的消息批量大小，默认 20
    Counter    TokenCounter
}

// ReactiveCompact 紧急压缩。
// 在 API 返回上下文超限错误时触发，对全部对话生成摘要。
type ReactiveCompact struct {
    Model   ModelProvider
    Counter TokenCounter
}

// Pipeline 将多个压缩策略组合为管道。
// 按顺序执行，每个策略的输出作为下一个策略的输入。
type Pipeline struct {
    stages []ContextCompressor // 未导出，通过 NewPipeline 构造
}

func NewPipeline(stages ...ContextCompressor) *Pipeline
```

### 3.4 两阶段上下文转换

借鉴 pi-agent 的设计，将上下文转换分为两个独立阶段：

```
Phase 1: TransformContext (Agent 消息层)
  ├── 注入 ephemeral messages
  ├── 注入 skill instructions
  ├── 裁剪过期的 tool results
  └── 应用 extension 的 context 事件处理器

Phase 2: ConvertToLLM (LLM 消息层)
  ├── CompactionSummary → <summary> XML user message
  ├── ToolPart → provider-specific tool_use/tool_result
  ├── DataPart → base64 encoded content
  └── FilePart → file URI reference
```

```go
// LLMConverter 将 Agent 内部消息转换为 LLM 可理解的标准消息。
// 不同的 Provider 可能需要不同的转换逻辑。
type LLMConverter interface {
    ConvertToLLM(ctx context.Context, messages []*Message) ([]*Message, error)
}
```

### 3.5 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 压缩架构 | 管道式 3 级主动压缩 + 1 级被动恢复 | 主动管道每轮执行，ReactiveCompact 仅在 API 413 错误时触发 |
| 上下文转换 | 两阶段分离 | Agent 消息和 LLM 消息解耦，支持自定义消息类型（来自 pi-agent） |
| 摘要状态 | 持久化到 Session | 跨 Run 存活，避免重复压缩 |
| Token 计数 | 接口抽象 | 支持精确 tokenizer 和启发式近似 |
| ModelProvider 位置 | 接口定义在 Core 层 | RollingSummary 依赖接口而非具体实现，符合依赖反转 |

---

## 支柱四：Event & Session

### 设计目标

事件系统是框架可扩展性的基础。通过统一的事件协议覆盖 Agent 完整生命周期，支持 Hook 拦截和修改行为。Session 提供对话状态的持久化和恢复能力。

### 4.1 事件系统

```go
// Event 是所有事件的基础接口。
type Event interface {
    Type() EventType
    Timestamp() time.Time
}

// BaseEvent 提供 Event 接口的通用实现，具体事件嵌入它。
type BaseEvent struct {
    EventType  EventType
    OccurredAt time.Time
}

func (e BaseEvent) Type() EventType      { return e.EventType }
func (e BaseEvent) Timestamp() time.Time { return e.OccurredAt }

// EventType 定义事件类型。
type EventType string

const (
    // 生命周期事件
    EventSessionStart    EventType = "session_start"
    EventSessionEnd      EventType = "session_end"
    EventAgentStart      EventType = "agent_start"
    EventAgentEnd        EventType = "agent_end"

    // 循环事件
    EventTurnStart       EventType = "turn_start"
    EventTurnEnd         EventType = "turn_end"
    EventPreGenerate     EventType = "pre_generate"
    EventPostGenerate    EventType = "post_generate"

    // 工具事件
    EventPreToolUse      EventType = "pre_tool_use"
    EventPostToolUse     EventType = "post_tool_use"
    EventToolError       EventType = "tool_error"

    // 子 Agent 事件
    EventSubagentStart   EventType = "subagent_start"
    EventSubagentEnd     EventType = "subagent_end"

    // 上下文事件
    EventPreCompact      EventType = "pre_compact"
    EventPostCompact     EventType = "post_compact"

    // 消息事件
    EventMessageStream   EventType = "message_stream"
    EventMessageComplete EventType = "message_complete"
)
```

具体事件类型携带类型化的 payload：

```go
// PreToolUseEvent 在工具执行前触发。
type PreToolUseEvent struct {
    BaseEvent
    ToolName string
    ToolID   string
    Input    string
}

// PostToolUseEvent 在工具执行后触发。
type PostToolUseEvent struct {
    BaseEvent
    ToolName string
    ToolID   string
    Input    string
    Output   string
    Duration time.Duration
    Err      error
}

// PreGenerateEvent 在模型调用前触发。
type PreGenerateEvent struct {
    BaseEvent
    Model       string
    MessageCount int
    TokenEstimate int64
}

// PostGenerateEvent 在模型调用后触发。
type PostGenerateEvent struct {
    BaseEvent
    Model      string
    TokenUsage TokenUsage
    Message    *Message
}

// TurnStartEvent 在每轮开始时触发。
type TurnStartEvent struct {
    BaseEvent
    TurnCount int
    Snapshot  TurnSnapshot
}

// SubagentStartEvent 在子 Agent 启动时触发。
type SubagentStartEvent struct {
    BaseEvent
    AgentName string
    Strategy  SpawnStrategy
}
```

### 4.2 Hook 系统

```go
// HookEmitter 是事件触发的接口抽象。
// LoopCallbacks.EmitEvent 接受此接口，测试时可替换为 no-op 实现。
type HookEmitter interface {
    Emit(ctx context.Context, event Event) (HookResult, error)
}

// Hook 是事件处理器，可以拦截和修改 Agent 行为。
type Hook interface {
    Handle(ctx context.Context, event Event) (HookResult, error)
}

// HookResult 控制 Hook 处理后的行为。
// 零值表示"继续执行，不做任何修改"（ShouldStop=false）。
type HookResult struct {
    // ShouldStop 为 true 时阻止后续执行（如阻止工具调用）。
    // 零值 false 表示继续，避免遗漏设置导致意外阻断。
    ShouldStop bool
    // SystemMessage 注入系统消息到对话中。
    SystemMessage string
    // ModifiedInput 修改工具输入（仅 PreToolUse 有效）。
    ModifiedInput string
}

// HookFunc 是 Hook 的函数适配器。
type HookFunc func(ctx context.Context, event Event) (HookResult, error)

func (f HookFunc) Handle(ctx context.Context, event Event) (HookResult, error) {
    return f(ctx, event)
}

// HookRegistry 是 HookEmitter 的默认实现。
type HookRegistry struct {
    hooks map[EventType][]Hook
}

func NewHookRegistry() *HookRegistry

// On 注册一个 Hook 到指定事件类型。
func (r *HookRegistry) On(eventType EventType, hook Hook)

// Emit 触发指定事件，按注册顺序执行所有 Hook。
// 任何 Hook 返回 ShouldStop=true 时短路后续 Hook。
func (r *HookRegistry) Emit(ctx context.Context, event Event) (HookResult, error)

// NoopEmitter 是不做任何事的 HookEmitter，用于测试。
type NoopEmitter struct{}

func (NoopEmitter) Emit(context.Context, Event) (HookResult, error) {
    return HookResult{}, nil
}
```

事件流转：

```
SessionStart
  │
  ├── AgentStart
  │     │
  │     ├── TurnStart
  │     │     ├── ContextTransform
  │     │     ├── PreCompact → PostCompact (如果触发压缩)
  │     │     ├── PreGenerate
  │     │     ├── MessageStream (多次)
  │     │     ├── PostGenerate / MessageComplete
  │     │     ├── PreToolUse → PostToolUse / ToolError (每个工具)
  │     │     └── TurnEnd
  │     │
  │     ├── SubagentStart → SubagentEnd (如果有子 Agent)
  │     │
  │     └── AgentEnd
  │
  └── SessionEnd
```

### 4.3 Session

```go
// Session 持有对话状态和消息历史。
type Session interface {
    ID() string
    State() State
    SetState(key string, value any)
    Append(ctx context.Context, msg *Message) error
    // History 返回为下一次模型调用准备的消息历史。
    // 配置了 ContextCompressor 时，历史会先经过压缩。
    History(ctx context.Context) ([]*Message, error)
}

// SessionStore 是 Session 的持久化后端。
type SessionStore interface {
    // Save 持久化 session 状态。
    Save(ctx context.Context, session Session) error
    // Load 从持久化存储恢复 session。
    Load(ctx context.Context, sessionID string) (Session, error)
    // List 列出所有 session。
    List(ctx context.Context) ([]SessionMeta, error)
}

// SessionMeta 是 session 的元数据摘要。
type SessionMeta struct {
    ID        string
    CreatedAt time.Time
    UpdatedAt time.Time
    TurnCount int
    Title     string
}
```

Session 持久化格式采用 append-only JSONL：

```
每条消息一行 JSON，追加写入，并发安全。
恢复时按顺序读取，最后一条同 ID 消息为最终状态。

session-{id}.jsonl:
  {"id":"msg-1","role":"user","parts":[...],"timestamp":"..."}
  {"id":"msg-2","role":"assistant","parts":[...],"timestamp":"..."}
  {"id":"msg-3","role":"tool","parts":[...],"timestamp":"..."}

session-{id}.meta.json:
  {"id":"...","state":{...},"createdAt":"...","updatedAt":"..."}
```

```go
// JSONLSessionStore 基于 JSONL 文件的 Session 持久化实现。
type JSONLSessionStore struct {
    Dir string // session 文件存储目录
}

func NewJSONLSessionStore(dir string) *JSONLSessionStore
```

### 4.4 Session 分支与恢复

```go
// Branch 从当前 session 创建一个分支。
// 分支共享历史消息，但后续消息独立。
// 用于 A/B 测试不同的对话路径。
func (s *session) Branch() Session

// ForkSession 创建缓存感知的子 session。
// 共享父 session 的 prompt cache 前缀，但消息独立。
func ForkSession(parent Session, opts ...SessionOption) Session
```

### 4.5 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 事件系统 | `Event` 接口 + `BaseEvent` 嵌入 + 具体事件类型 | 类型安全的 payload，Hook 通过类型断言获取事件数据 |
| Hook 抽象 | `HookEmitter` 接口 + `HookRegistry` 默认实现 | 接口便于测试替换（NoopEmitter），符合 Go 惯用法 |
| Hook 语义 | 值类型 `HookResult`，`ShouldStop` 零值为 false | 避免 nil 指针歧义，零值即"继续执行" |
| Session 持久化 | Append-only JSONL + `SessionStore` 接口 | 并发安全、增量写入，接口支持多种后端 |
| Session 分支 | Branch + Fork | 支持对话路径分支和缓存感知子 session |

---

## 支柱五：Message & Provider

### 设计目标

定义统一的多模态消息协议和 LLM Provider 抽象层，使框架可以无缝切换不同的模型提供商。

### 5.1 消息协议

```go
// Role 表示消息的作者角色。
type Role string

const (
    RoleUser      Role = "user"
    RoleSystem    Role = "system"
    RoleAssistant Role = "assistant"
    RoleTool      Role = "tool"
)

// Status 表示消息的生成状态。
type Status string

const (
    StatusInProgress Status = "in_progress"
    StatusIncomplete Status = "incomplete"
    StatusCompleted  Status = "completed"
)

// Part 是消息的内容单元，支持多模态。
type Part interface{ isPart() }

type TextPart struct {
    Text string `json:"text"`
}

type FilePart struct {
    Name     string   `json:"name"`
    URI      string   `json:"uri"`
    MIMEType MIMEType `json:"mimeType"`
}

type DataPart struct {
    Name     string   `json:"name"`
    Bytes    []byte   `json:"bytes"`
    MIMEType MIMEType `json:"mimeType"`
}

type ToolPart struct {
    ID        string `json:"id"`
    Name      string `json:"name"`
    Request   string `json:"arguments"`
    Response  string `json:"result,omitempty"`
    Completed bool   `json:"completed,omitempty"`
}

// Message 是对话中的单条消息。
type Message struct {
    ID           string         `json:"id"`
    Role         Role           `json:"role"`
    Parts        []Part         `json:"parts"`
    Author       string         `json:"author"`
    InvocationID string         `json:"invocationId,omitempty"`
    Status       Status         `json:"status"`
    FinishReason string         `json:"finishReason,omitempty"`
    TokenUsage   TokenUsage     `json:"tokenUsage,omitempty"`
    Actions      map[string]any `json:"actions,omitempty"`
    Metadata     map[string]any `json:"metadata,omitempty"`
}

// TokenUsage 跟踪 token 消耗。
type TokenUsage struct {
    InputTokens  int64 `json:"inputTokens"`
    OutputTokens int64 `json:"outputTokens"`
    TotalTokens  int64 `json:"totalTokens"`
    CacheHit     int64 `json:"cacheHit,omitempty"`     // prompt cache 命中的 token 数
    CacheMiss    int64 `json:"cacheMiss,omitempty"`    // prompt cache 未命中的 token 数
}
```

消息扩展机制 — 通过 `Actions` 和 `Metadata` 字段实现：

```
Actions 用于控制流信号（工具 → Agent Loop）：
  - "loop_exit": true          ExitTool 触发循环退出
  - "handoff_to_agent": "sub1" RoutingAgent 委派信号
  - "escalate": true           升级到外层循环

Metadata 用于附加元数据（不影响控制流）：
  - "model": "claude-opus-4-7"
  - "latency_ms": 1234
  - "cache_hit_rate": 0.85
```

### 5.2 Provider 抽象

```go
// ModelProvider 是 LLM 提供商的统一接口。
type ModelProvider interface {
    // Name 返回模型名称。
    Name() string
    // Generate 执行请求并返回单个助手响应。
    Generate(ctx context.Context, req *ModelRequest) (*ModelResponse, error)
    // NewStreaming 执行请求并返回流式响应。
    NewStreaming(ctx context.Context, req *ModelRequest) Generator[*ModelResponse, error]
}

// ModelRequest 是多模态聊天请求。
type ModelRequest struct {
    Tools        []tools.Tool       `json:"tools,omitempty"`
    Messages     []*Message         `json:"messages"`
    Instruction  *Message           `json:"instruction,omitempty"`
    InputSchema  *jsonschema.Schema `json:"inputSchema,omitempty"`
    OutputSchema *jsonschema.Schema `json:"outputSchema,omitempty"`
}

// ModelResponse 是单条助手消息响应。
type ModelResponse struct {
    Message *Message `json:"message"`
}
```

### 5.3 Provider 注册与发现

```go
// ModelRegistry 管理模型提供商的注册和发现。
type ModelRegistry struct {
    providers map[string]ModelProvider
}

func NewModelRegistry() *ModelRegistry

// Register 注册一个模型提供商。
func (r *ModelRegistry) Register(name string, provider ModelProvider)

// Resolve 根据模型名称解析提供商。
func (r *ModelRegistry) Resolve(name string) (ModelProvider, error)
```

### 5.4 流式协议

```
流式响应通过 Generator[*ModelResponse, error] 传递：

  for response, err := range model.NewStreaming(ctx, req) {
      // response.Message.Status == StatusInProgress  → 流式 delta
      // response.Message.Status == StatusCompleted    → 最终消息
  }

Provider 实现负责：
  1. 将 provider-specific 的流式事件转换为统一的 ModelResponse
  2. 增量更新 Message.Parts（追加 TextPart、新增 ToolPart）
  3. 在最终消息上设置 TokenUsage 和 FinishReason
```

### 5.5 System Prompt 结构

```
┌──────────────────────────────────────────────┐
│ System Prompt (cacheable prefix)             │
│                                              │
│  ┌────────────────────────────────────────┐  │
│  │ Static Section (scope: global cache)   │  │
│  │  - Framework identity & capabilities   │  │
│  │  - Tool definitions & guidelines       │  │
│  │  - Skills instructions                 │  │
│  │  - Project context (CLAUDE.md)         │  │
│  └────────────────────────────────────────┘  │
│  ── DYNAMIC BOUNDARY ──                      │
│  ┌────────────────────────────────────────┐  │
│  │ Dynamic Section (per-turn)             │  │
│  │  - Current working directory           │  │
│  │  - Active file states                  │  │
│  │  - Ephemeral instructions              │  │
│  │  - Turn-specific context               │  │
│  └────────────────────────────────────────┘  │
└──────────────────────────────────────────────┘

将 system prompt 分为静态可缓存前缀和动态后缀，
最大化 prompt cache 命中率。
```

### 5.6 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 消息格式 | 多模态 Part 联合类型 | 统一文本、文件、数据、工具调用的表示 |
| 扩展机制 | Actions + Metadata map | 无需修改 Message 结构即可扩展控制流和元数据 |
| 流式协议 | `iter.Seq2` Generator | Go 惯用模式，与 Agent.Run 接口一致 |
| Provider 抽象 | Generate + NewStreaming 双方法 | 同时支持同步和流式场景 |
| System Prompt | 静态前缀 + 动态后缀 | 最大化 prompt cache 命中率（来自 Claude Code） |
| Token 追踪 | 包含 CacheHit/CacheMiss | 支持 prompt cache 效率监控 |

---

## 支柱六：Sandbox & Executor

### 设计目标

提供安全的代码执行环境，通过可插拔的执行后端支持本地、Docker、SSH 等不同的隔离级别。结合权限系统控制工具的执行范围。

### 6.1 Executor 接口

```go
// Executor 是代码执行的统一接口。
// 不同的实现提供不同的隔离级别。
type Executor interface {
    // Exec 在沙箱中执行命令。
    Exec(ctx context.Context, req *ExecRequest) (*ExecResult, error)
    // ExecStream 在沙箱中流式执行命令。
    ExecStream(ctx context.Context, req *ExecRequest) Generator[*ExecUpdate, error]
    // Close 释放执行器资源。
    Close() error
}

// ExecRequest 定义执行请求。
type ExecRequest struct {
    Command    string            // 要执行的命令
    Args       []string          // 命令参数
    WorkDir    string            // 工作目录
    Env        map[string]string // 环境变量
    Stdin      io.Reader         // 标准输入
    Timeout    time.Duration     // 执行超时
    Limits     *ResourceLimits   // 资源限制
}

// ExecResult 是执行结果。
type ExecResult struct {
    ExitCode int
    Stdout   string
    Stderr   string
    Duration time.Duration
}

// ExecUpdate 是流式执行的增量更新。
type ExecUpdate struct {
    Stream string // "stdout" 或 "stderr"
    Data   string
    Done   bool
    Result *ExecResult // 仅在 Done=true 时有值
}

// ResourceLimits 定义资源限制。
type ResourceLimits struct {
    MaxMemoryMB   int64         // 最大内存（MB）
    MaxCPUPercent int           // 最大 CPU 百分比
    MaxDiskMB     int64         // 最大磁盘使用（MB）
    MaxOutputSize int64         // 最大输出大小（bytes）
    Timeout       time.Duration // 执行超时
}
```

### 6.2 执行后端

```
┌─────────────────────────────────────────────────┐
│              Executor Backends                   │
│                                                  │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────┐ │
│  │   Local      │  │   Docker    │  │   SSH   │ │
│  │             │  │             │  │         │ │
│  │ os/exec     │  │ container   │  │ remote  │ │
│  │ 无隔离      │  │ 完全隔离    │  │ 远程    │ │
│  │ 最快        │  │ 最安全      │  │ 分布式  │ │
│  └─────────────┘  └─────────────┘  └─────────┘ │
└─────────────────────────────────────────────────┘
```

```go
// LocalExecutor 在本地进程中执行命令。
// 无隔离，最快，适用于受信任的工具。
type LocalExecutor struct {
    DefaultWorkDir string
    DefaultEnv     map[string]string
}

// DockerExecutor 在 Docker 容器中执行命令。
// 完全隔离，适用于不受信任的代码执行。
type DockerExecutor struct {
    Image      string            // 容器镜像
    Volumes    map[string]string // 挂载卷
    Network    string            // 网络模式
    DefaultEnv map[string]string
}

// SSHExecutor 在远程主机上执行命令。
// 适用于分布式执行场景。
type SSHExecutor struct {
    Host       string
    User       string
    KeyFile    string
    DefaultEnv map[string]string
}
```

### 6.3 权限系统

```go
// PermissionChecker 检查工具执行权限。
type PermissionChecker interface {
    // Check 检查是否允许执行指定的工具调用。
    // 返回零值 PermissionDecision{Decided: false} 表示"无意见，交给下一个 checker"。
    Check(ctx context.Context, req PermissionRequest) (PermissionDecision, error)
}

// PermissionRequest 是权限检查请求。
type PermissionRequest struct {
    Tool     string // 工具名称
    Input    string // 工具输入
    ReadOnly bool   // 工具是否只读
    Agent    string // 发起调用的 Agent 名称
}

// PermissionDecision 是权限检查结果。
// 零值表示"未做出决策"（Decided=false），链式 checker 会继续到下一个。
type PermissionDecision struct {
    Decided bool   // 是否做出了明确决策
    Allowed bool   // 是否允许（仅 Decided=true 时有意义）
    Reason  string // 决策原因
}

// PermissionMode 定义权限模式。
type PermissionMode int

const (
    PermissionDefault       PermissionMode = iota // 默认：危险操作需确认
    PermissionAcceptEdits                          // 自动接受文件编辑
    PermissionBypass                               // 跳过所有权限检查
    PermissionReadOnly                             // 仅允许只读操作
    PermissionAuto                                 // 使用分类器自动判断
)
```

权限决策链（优先级从高到低）：

```
1. Rule       — 静态规则匹配（allowlist/denylist）
2. Mode       — 权限模式判断
3. Hook       — PreToolUse Hook 覆盖
4. Classifier — 自动模式下的 LLM 分类器
5. Default    — 回退到用户确认
```

```go
// PermissionChain 组合多个 PermissionChecker，按优先级短路。
type PermissionChain struct {
    Checkers []PermissionChecker
}

func (c *PermissionChain) Check(ctx context.Context, req PermissionRequest) (PermissionDecision, error) {
    for _, checker := range c.Checkers {
        decision, err := checker.Check(ctx, req)
        if err != nil {
            return PermissionDecision{}, err
        }
        if decision.Decided {
            return decision, nil // 短路：第一个有明确决策的 checker 生效
        }
    }
    return PermissionDecision{}, nil // 所有 checker 都未做出决策，回退到默认行为
}
```

### 6.4 工具执行管道

将 Sandbox 和权限系统集成到工具执行管道中：

```
Tool Execution Pipeline:

  ToolCall
    │
    ├── 1. Input Validation ──── Zod/JSON Schema 校验
    │
    ├── 2. Permission Check ──── PermissionChain 决策
    │     ├── Rule Match → allow/deny
    │     ├── Mode Check → allow/deny/continue
    │     ├── Hook Check → allow/deny/modify/continue
    │     ├── Classifier → allow/deny (auto mode)
    │     └── User Prompt → allow/deny (fallback)
    │
    ├── 3. PreToolUse Hook ──── 可修改输入或阻止执行
    │
    ├── 4. Executor Dispatch ── 根据工具类型选择执行后端
    │     ├── ReadOnly tool → LocalExecutor
    │     ├── Trusted tool  → LocalExecutor + ResourceLimits
    │     └── Untrusted     → DockerExecutor
    │
    ├── 5. Execute ──────────── 实际执行
    │
    ├── 6. Result Budget ────── 裁剪超大结果
    │
    └── 7. PostToolUse Hook ── 可修改结果
```

### 6.5 Operations 抽象

借鉴 pi-agent 的 Operations 模式，工具通过 Operations 接口与执行后端交互，而非直接调用 Executor：

```go
// BashOperations 定义 Bash 工具需要的操作集。
type BashOperations interface {
    Exec(ctx context.Context, command string, opts ...ExecOption) (*ExecResult, error)
    ExecStream(ctx context.Context, command string, opts ...ExecOption) Generator[*ExecUpdate, error]
}

// FileOperations 定义文件工具需要的操作集。
type FileOperations interface {
    Read(ctx context.Context, path string) ([]byte, error)
    Write(ctx context.Context, path string, content []byte) error
    Edit(ctx context.Context, path string, edits []TextEdit) error
    Glob(ctx context.Context, pattern string) ([]string, error)
    Grep(ctx context.Context, pattern string, opts ...GrepOption) ([]GrepMatch, error)
}

// 工具接收 Operations 接口，执行后端可替换（本地、Docker、SSH）。
// 工具逻辑不变，只需替换 Operations 实现。
type BashTool struct {
    ops BashOperations
}

type FileReadTool struct {
    ops FileOperations
}
```

### 6.6 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 执行后端 | 可插拔 Executor 接口 | 统一本地/Docker/SSH 的执行抽象 |
| 权限系统 | 链式决策 + 5 种模式 | 灵活的分层权限控制（来自 Claude Code 简化版） |
| 工具-后端解耦 | Operations 接口 | 工具逻辑与执行环境解耦（来自 pi-agent） |
| 资源限制 | ResourceLimits 结构 | 统一的资源约束定义 |

---

## 附录：Recipe 声明式构建

Recipe 系统提供 YAML 声明式的 Agent 构建能力，将上述六大支柱的配置统一到一个声明文件中：

```yaml
version: "1"
name: coding-assistant
model: claude-opus-4-7
instruction: |
  You are a coding assistant...

context:
  strategy: summarize
  max_tokens: 100000
  keep_recent: 10

tools:
  - bash
  - file_read
  - file_write
  - file_edit

skills:
  - go-best-practices
  - security-review

middlewares:
  - retry:
      attempts: 3
  - confirm:
      tools: [bash, file_write]

sandbox:
  executor: docker
  image: golang:1.24
  limits:
    max_memory_mb: 512
    timeout: 300s

sub_agents:
  - name: reviewer
    model: claude-sonnet-4-6
    instruction: Review the code...
    execution: tool

hooks:
  pre_tool_use:
    - command: "echo 'Tool: {{.tool_name}}'"
  post_compact:
    - command: "echo 'Compacted at turn {{.turn}}'"
```

Recipe Builder 将 YAML 解析为完整的 Agent 配置，通过 Registry 解析模型、工具和中间件引用，最终调用 `NewAgent()` 或 Flow 构造函数创建 Agent 实例。

---

## 附录：Graph DAG 引擎

Graph 引擎作为独立的工作流编排层，与 Agent Loop 互补：

```go
package graph

// Graph 是有向无环图的构建器。
type Graph struct { ... }

func New() *Graph
func (g *Graph) AddNode(name string, handler Handler, opts ...NodeOption) *Graph
func (g *Graph) AddEdge(from, to string, opts ...EdgeOption) *Graph
func (g *Graph) SetEntryPoint(name string) *Graph
func (g *Graph) SetFinishPoint(name string) *Graph
func (g *Graph) Compile() (*Executor, error)

// Executor 执行编译后的图。
type Executor struct { ... }

func (e *Executor) Execute(ctx context.Context, state State) (State, error)
func (e *Executor) Resume(ctx context.Context, state State) (State, error)
```

特性：
- 编译时环检测和可达性验证
- 条件边和并行扇出
- Checkpoint 支持中断恢复
- 节点级 Retry 中间件

---

## 附录：Memory 系统

```go
package memory

// Memory 是一条记忆条目。
type Memory struct {
    Content  *blades.Message
    Metadata map[string]any
}

// MemoryStore 是记忆的存储后端。
type MemoryStore interface {
    AddMemory(ctx context.Context, memory *Memory) error
    SearchMemory(ctx context.Context, query string) ([]*Memory, error)
}

// MemoryTool 将 MemoryStore 包装为 Tool，供 Agent 在对话中检索记忆。
func NewMemoryTool(store MemoryStore) tools.Tool
```

分层记忆加载（优先级从高到低）：

```
1. User Memory     — ~/.blades/CLAUDE.md (用户级)
2. Project Memory  — 从 cwd 向上遍历 CLAUDE.md / .claude/CLAUDE.md
3. Local Memory    — CLAUDE.local.md (不提交到 git)
4. Auto Memory     — ~/.blades/memories/*.md (自动提取)
5. Session Memory  — JSONL session 文件 (单次会话)
```

自动记忆提取作为 fire-and-forget 后台任务运行：每轮结束后 fork 一个受限的子 Agent（只读 + 写入 memory 目录），从对话中提取持久化事实。
