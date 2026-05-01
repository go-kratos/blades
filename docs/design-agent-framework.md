---
type: design
title: Blades Agent Framework 设计蓝图
date: 2026-05-01
status: draft
author: chenzhihui
related: [reference-claude-code-agent.md, reference-pi-agent-framework.md]
tags: [agent, framework, architecture, context-management, memory, tools, permissions, hooks, session, streaming]
---

# Blades Agent Framework 设计蓝图

## 背景与动机

### 现状

Blades 是一个基于 Go 构建的 Agent 框架，当前已具备：

- `Agent` 接口 + `Generator[T,E]`（`iter.Seq2`）流式原语
- `Invocation` 调用上下文 + `Session` 会话管理
- `Middleware` 洋葱模型（agent/tool/graph 三层）
- `flow/` 组合模式（Sequential、Parallel、Loop、Routing、Deep）
- `graph/` DAG 执行器（编译时验证）
- `tools/` 工具系统（`NewFunc[I,O]` 泛型构造 + `Resolver` 动态发现）
- `skills/` 技能系统（SKILL.md + frontmatter）
- `memory/` 内存存储（InMemoryStore + 子串搜索）
- `recipe/` 声明式 YAML 构建
- `contrib/` 多 Provider（Anthropic、OpenAI、Gemini、MCP、OTel）

### 问题

通过对 Claude Code Agent 和 pi-agent 两个成熟框架的深入分析，发现当前 Blades 在以下方面存在差距：

1. **Agent Loop 过于简单** — 当前 `handle()` 是平坦的迭代循环，缺少 steering/follow-up 注入、多策略压缩、事件发射
2. **无上下文压缩管线** — 仅有单一 `ContextCompressor` 接口，缺少 Claude Code 的多策略分层压缩
3. **工具执行无流式重叠** — 必须等模型完成才执行工具，无法在流式输出时提前启动并发安全工具
4. **无 Hook/事件系统** — 仅有 Middleware 洋葱模型，缺少生命周期事件订阅
5. **无权限系统** — 仅有 `Confirm` 中间件，缺少分层权限决策链
6. **会话无持久化** — 仅有内存实现，无 JSONL 持久化、无分支、无树形结构
7. **Memory 系统原始** — 仅有简单的内存存储和子串搜索，缺少层级 Memory、自动提取
8. **消息类型扩展机制待定** — `Part` 是密封接口，后续可考虑开放注册（本次设计暂不处理）

### 目标

- 设计一个融合 Claude Code 和 pi-agent 最佳实践的全新 Agent 框架
- 保持 Go 惯用风格（接口、iter.Seq2、context.Context、Option 函数）
- 不考虑向后兼容，以最新框架理念为主
- 流式优先、缓存感知、可扩展、可组合

---

## 方案设计

### 整体架构

```
┌─────────────────────────────────────────────────────────────────┐
│  应用层                                                          │
│    ├── CLI / REPL          终端交互入口                          │
│    ├── SDK                 程序化调用入口                        │
│    └── Recipe              声明式 YAML 构建                      │
├─────────────────────────────────────────────────────────────────┤
│  blades/（根包：用户 API）                                       │
│    ├── Agent 接口           Run(ctx, <-chan InputEvent) ...      │
│    ├── InputEvent          用户输入事件（Prompt/Steer/Control）  │
│    └── OutputEvent         Agent 输出事件（Text/Tool/Turn/Done） │
├─────────────────────────────────────────────────────────────────┤
│  Agent Loop（状态机，根包内部实现）                               │
│    ├── 状态转换            Idle → Preparing → Streaming → Acting │
│    ├── TurnState           不可变每轮状态                        │
│    └── Steer Queue         中途指令队列（FIFO）                  │
├─────────────────────────────────────────────────────────────────┤
│  Internal Service Layer（Agent Loop 私有实现）                    │
│    ├── ContextBuilder      Session → 压缩 → 过滤 → model.Request│
│    └── streamAndRecord     Provider Stream → Event + Session     │
├─────────────────────────────────────────────────────────────────┤
│  Capability Service Layer（用户可配置能力层）                     │
│    ├── Compression         5 策略分层压缩管线                    │
│    ├── Tool Orchestrator   流式执行 + 并发分区                   │
│    ├── Permission Chain    分层权限决策                           │
│    ├── Hook Registry       生命周期事件订阅                      │
│    ├── Retry Policy        API 错误处理与重试                    │
│    └── Sub-Agent Manager   Fork/Background/Worktree              │
├─────────────────────────────────────────────────────────────────┤
│  基础设施层                                                      │
│    ├── model.*             Message + Provider + Request/Response │
│    ├── session.Store       JSONL 追加式持久化 + 消息树           │
│    ├── memory.Store        5 层 Memory 层级 + 自动提取           │
│    ├── prompt.Builder      缓存感知构建（静态前缀 + 动态后缀）   │
│    ├── token.Counter       Token 计数（Provider 原生/本地/估算） │
│    └── settings.Loader     多级优先级配置合并                    │
├─────────────────────────────────────────────────────────────────┤
│  Provider 实现层（contrib/）                                     │
│    ├── contrib/anthropic   实现 model.Provider + 内部格式转换    │
│    ├── contrib/openai      实现 model.Provider + 内部格式转换    │
│    ├── contrib/gemini      实现 model.Provider + 内部格式转换    │
│    ├── contrib/mcp         MCP 协议工具桥接                      │
│    └── contrib/otel        OpenTelemetry 可观测性                │
└─────────────────────────────────────────────────────────────────┘
```

### 设计原则

| 原则 | 说明 | 参考来源 |
|------|------|---------|
| 极简根包 | 根包只放 Agent 接口 + Event 类型，Message/Provider 下沉到 model/ 包 | 原创设计 |
| Event 类型安全边界 | 用户侧只接触 InputEvent/OutputEvent，Message 是 model/ 包的内部类型 | 原创设计 |
| 双向实时流 | `<-chan InputEvent` 进、`<-chan OutputEvent` 出，Agent 运行中可注入指令 | 原创设计 |
| 显式状态机 | Agent Loop 通过 AgentState + 转换规则驱动，可声明可测试 | Claude Code query loop + pi-agent 双循环 |
| 不可变轮次 | 每轮创建新 TurnState，不原地修改消息数组 | Claude Code State 对象 |
| 缓存感知 | System Prompt 分静态/动态两段，工具按名称排序保证缓存稳定 | Claude Code 静态/动态分界 |
| 极简核心 | 核心只做 Loop + Event + Tool 执行，权限/MCP/Memory 全部可插拔 | pi-agent 极简核心哲学 |
| 双层 Service Layer | Internal Service Layer（私有实现）与 Capability Service Layer（用户可配置）分离 | 原创设计 |
| model/ 纯类型包 | model/ 只放类型定义和接口，适配/转换逻辑在使用侧（Service Layer 或 contrib/） | 原创设计 |
| 消息边界 | 应用层 Event 与 LLM 层 model.Message 通过 ContextBuilder 内部转换，不暴露独立接口 | pi-agent convertToLlm |
| 渐进式扩展 | 从 Prompt 模板到 Skill 到 Extension 到 Package，复杂度渐进 | pi-agent 四层扩展 |

### 命名规范

遵循 Go 惯用的 `package.Role` 模式：包名是名词（领域），类型名是角色（动作者）。与 Go 标准库一致：`io.Reader`、`http.Handler`、`sql.Scanner`。

```
model.Message
model.Provider
model.Request
model.Response
model.TextPart
session.Store
session.Session
prompt.Builder
tools.Resolver
compact.Pipeline
graph.Checkpointer
hook.Registry
token.Counter
permission.Chain
```

---

## Event 系统设计

Event 系统是整个框架的顶层架构。核心思想：**类型安全的双向 Event 通信，Agent 是纯函数**。

### 三层架构

```
┌──────────────────────────────────────────────────────────┐
│  User Layer（blades/ 根包）                               │
│    <-chan InputEvent  ──→  Agent  ──→  <-chan OutputEvent  │
│    输入 channel                       输出 channel        │
├──────────────────────────────────────────────────────────┤
│  Agent Loop（状态机，根包内部实现）                        │
│    States: Idle → Preparing → Streaming → Acting          │
│    从 input channel 读取，向 output channel 写入          │
│    编排 Service Layer 完成具体工作                        │
├──────────────────────────────────────────────────────────┤
│  Internal Service Layer（Agent Loop 私有实现）             │
│    ContextBuilder:    Session → 压缩 → 过滤 → model.Request│
│    streamAndRecord:   Provider Stream → Event + Session    │
├──────────────────────────────────────────────────────────┤
│  Capability Service Layer（用户可配置能力层）              │
│    Compression:       5 策略分层压缩管线                  │
│    ToolOrchestrator:  流式执行 + 并发分区                 │
│    PermissionChain:   分层权限决策                         │
│    HookRegistry:      生命周期事件订阅                     │
│    RetryPolicy:       API 错误处理与重试                   │
├──────────────────────────────────────────────────────────┤
│  model/（纯类型包：Message + Provider 接口 + Request/Response）
│    model.Provider.NewStreaming(ctx, *model.Request)        │
├──────────────────────────────────────────────────────────┤
│  contrib/（Provider 实现，各自处理格式转换）               │
│    Anthropic / OpenAI / Gemini 实现 model.Provider        │
└──────────────────────────────────────────────────────────┘
```

**为什么需要这个分层？**

当前 Blades 中 `*Message` 贯穿全栈——用户发送 `*Message`、Agent 返回 `Generator[*Message, error]`、Session 存储 `[]*Message`、Provider 接收 `*Message`。一个类型承担了三个不同职责（用户 I/O、对话历史、Provider 通信），导致用户需要理解 `Role`、`Status`、`Parts` 等内部概念才能使用框架。

新设计将职责分离到不同层：Event 是用户协议（blades/ 根包），`model.Message` 是 Service Layer 和 Provider 层的内部表示。用户只需要知道"InputEvent 进，OutputEvent 出"。`model/` 包是纯类型定义，适配和转换逻辑在使用侧（Internal Service Layer 或 contrib/）。

Service Layer 进一步拆分为两层：
- **Internal Service Layer**（ContextBuilder、streamAndRecord）— Agent Loop 的私有实现，不暴露给用户。ContextBuilder 负责 session → model.Request（含压缩和消息过滤），streamAndRecord 负责 provider stream → OutputEvent + session 记录。
- **Capability Service Layer**（Compression、ToolOrchestrator、PermissionChain、HookRegistry、RetryPolicy）— 用户可配置的能力层

### Agent 接口

```go
type Agent interface {
    Name() string
    Description() string
    Run(context.Context, <-chan InputEvent) (<-chan OutputEvent, error)
}
```

三个方法。`Run` 接收 InputEvent 输入 channel，返回 OutputEvent 输出 channel。启动失败返回 error，运行时错误通过 `ErrorEvent` / `DoneEvent` 传递。输入和输出使用不同的 Event 接口，编译期防止方向错误。

### Event 类型

Event 分为 `InputEvent` 和 `OutputEvent` 两个接口，通过不同的 marker method 区分方向。编译器可以阻止将 `TextEvent` 塞进 input channel 或将 `PromptEvent` 从 output channel 读出来。

```go
type InputEvent interface{ inputEvent() }
type OutputEvent interface{ outputEvent() }
```

**输入方向（用户 → Agent）：**

```go
// PromptEvent 发送一条消息。
type PromptEvent struct {
    Content     string       `json:"content"`
    Attachments []Attachment `json:"attachments,omitempty"`
}

// SteerEvent 在 Agent 工作中途注入指令。
// 精确语义：Steer 在当前轮次的所有工具执行完成后、下一轮
// ContextBuilder.Build 之前，按 FIFO 顺序注入为 user message。
// 如果模型正在 streaming，Steer 不会中断当前 streaming，
// 而是排队等待当前轮次结束。多个 Steer 按到达顺序排列。
type SteerEvent struct {
    Content string `json:"content"`
}

// ControlEvent 控制 Agent 行为。
type ControlEvent struct {
    Action ControlAction `json:"action"`
}

type ControlAction string
const (
    ActionAbort  ControlAction = "abort"
    ActionPause  ControlAction = "pause"
    ActionResume ControlAction = "resume"
)
```

**输出方向（Agent → 用户）：**

```go
// TextEvent 模型输出的文本片段。
type TextEvent struct {
    Delta string `json:"delta"`
}

// ThinkingEvent 模型的思考过程（如 Claude extended thinking）。
type ThinkingEvent struct {
    Delta string `json:"delta"`
}

// ToolStartEvent 开始执行工具调用。
type ToolStartEvent struct {
    CallID string `json:"callId"`
    Name   string `json:"name"`
    Args   string `json:"args"`
}

// ToolEndEvent 工具执行完成。
type ToolEndEvent struct {
    CallID string `json:"callId"`
    Name   string `json:"name"`
    Result string `json:"result"`
    Err    error  `json:"-"`
}

// TurnEndEvent 一个完整轮次结束（含工具调用的轮次，或模型正常回复结束）。
// 多轮对话中，用户在收到 TurnEndEvent 后决定是否继续发送 PromptEvent。
type TurnEndEvent struct {
    Turn    int         `json:"turn"`
    Usage   *TokenUsage `json:"usage,omitempty"`
    HasText bool        `json:"hasText"` // true = 模型正常回复结束（无工具调用）
}

// ErrorEvent 可恢复的运行时错误（如 API 限流重试）。
type ErrorEvent struct {
    Err     error         `json:"-"`
    Retry   bool          `json:"retry"`
    RetryIn time.Duration `json:"retryIn,omitempty"`
}

// DoneEvent Agent 生命周期结束。
// 这是 output channel 关闭前的最后一个事件。
// 注意：DoneEvent 严格表示 Agent 终止，不用于表示"一轮完成"。
// 模型正常回复结束发 TurnEndEvent{HasText: true}，不发 DoneEvent。
type DoneEvent struct {
    Reason TerminalReason `json:"reason"`
    Text   string         `json:"text"`
    Usage  *TokenUsage    `json:"usage,omitempty"`
}

type TerminalReason string
const (
    ReasonMaxTurns  TerminalReason = "max_turns"
    ReasonAborted   TerminalReason = "aborted"
    ReasonError     TerminalReason = "error"
)
```

**为什么拆分 InputEvent / OutputEvent？**

- 编译期类型安全——`chan<- InputEvent` 和 `<-chan OutputEvent` 防止方向错误
- Middleware 语义清晰——`InputMiddleware` 过滤用户指令，`OutputMiddleware` 过滤模型输出
- 概念仍然简洁——用户只需理解"InputEvent 进，OutputEvent 出"
- 方向由类型和 channel 双重表达，不会误用

### 便捷函数

```go
// Prompt 创建 PromptEvent。
func Prompt(content string, attachments ...Attachment) *PromptEvent

// Steer 创建 SteerEvent。
func Steer(content string) *SteerEvent

// Abort 创建中止 ControlEvent。
func Abort() *ControlEvent
```

### 使用方式

**简单场景——单次调用：**

```go
input := make(chan InputEvent, 1)
input <- Prompt("hello")
close(input)

output, err := agent.Run(ctx, input)
if err != nil {
    log.Fatal(err)
}
for event := range output {
    switch e := event.(type) {
    case *TextEvent:    fmt.Print(e.Delta)
    case *ErrorEvent:   log.Printf("error: %v", e.Err)
    case *TurnEndEvent: fmt.Println() // 模型回复结束
    }
}
// output channel 关闭，for range 自然退出
```

**Live 场景——中途注入 Steer：**

```go
input := make(chan InputEvent, 1)
input <- Prompt("分析这段代码")

output, err := agent.Run(ctx, input)
if err != nil {
    log.Fatal(err)
}
for event := range output {
    switch e := event.(type) {
    case *TextEvent:
        fmt.Print(e.Delta)
    case *ToolStartEvent:
        input <- Steer("同时检查测试覆盖率") // 排队，当前轮工具执行完后下一轮生效
    case *TurnEndEvent:
        if !e.HasText {
            continue // 工具轮结束，等待下一轮
        }
        close(input) // 模型回复结束，关闭 input
    }
}
```

**多轮对话——同一个 channel：**

```go
input := make(chan InputEvent, 1)
input <- Prompt("hello")

output, err := agent.Run(ctx, input)
if err != nil {
    log.Fatal(err)
}
for event := range output {
    switch e := event.(type) {
    case *TextEvent:
        fmt.Print(e.Delta)
    case *TurnEndEvent:
        if e.HasText {
            if wantMore {
                input <- Prompt("继续上面的话题") // 新一轮，同一个循环
            } else {
                close(input) // 关闭 input，Agent 结束，output 关闭，循环退出
            }
        }
    case *DoneEvent:
        log.Printf("agent terminated: %s", e.Reason)
    }
}
```

### Middleware

Middleware 分为输入和输出两种，类型签名不同，语义清晰：

```go
// InputMiddleware 过滤/转换用户指令。
type InputMiddleware func(<-chan InputEvent) <-chan InputEvent

// OutputMiddleware 过滤/转换模型输出。
type OutputMiddleware func(<-chan OutputEvent) <-chan OutputEvent
```

```go
// 日志中间件（输出方向）
func LogOutputEvents(in <-chan OutputEvent) <-chan OutputEvent {
    out := make(chan OutputEvent)
    go func() {
        defer close(out)
        for e := range in {
            log.Printf("event: %T", e)
            out <- e
        }
    }()
    return out
}
```

### Service Layer 设计

Service Layer 分为两层：Internal Service Layer 是 Agent Loop 的私有实现细节，Capability Service Layer 是用户可配置的能力层。

#### Internal Service Layer（Agent Loop 私有实现）

##### ContextBuilder（Session → model.Request）

```go
// ContextBuilder 从 Session 构建 Provider 请求。
// 内部通过 filterForProvider 私有方法处理消息过滤/转换：
//   - ThinkingPart → 根据 provider 能力决定保留或转为文本
//   - CompactionSummaryPart → 转为 system message
//   - BranchMarkerPart → 过滤掉
// 不暴露独立的 MessageConverter 接口，转换规则与构建逻辑紧密耦合。
type ContextBuilder struct {
    compression *compact.Pipeline
    prompt      *prompt.Builder
}

func (b *ContextBuilder) Build(ctx context.Context, session session.Session, tools []tools.Tool) (*model.Request, error)
```

##### streamAndRecord（Provider Stream → Event + Session）

```go
// streamAndRecord 是 agent loop 的私有方法，同时完成三件事：
// 1. 从 provider stream 读取 → 转为 OutputEvent 写入 output channel
// 2. 累积完整的 model.Message（含 tool calls）
// 3. 将完整消息写入 session
//
// 这是 Claude Code processApiTurn 和 pi-agent runInference 的等价实现。
// 不作为独立接口暴露，因为它与 agent loop 状态紧密耦合。
func (a *agent) streamAndRecord(
    ctx context.Context,
    stream iter.Seq2[*model.Response, error],
    session session.Session,
    output chan<- OutputEvent,
) (msg *model.Message, toolCalls []ToolCall, err error)
```

注意：格式转换逻辑（如 Anthropic 的 tool_use/tool_result 拆分、OpenAI 的 function_call 格式）
不在 Internal Service Layer 中，而是由各 `contrib/*` 包在实现 `model.Provider` 接口时内部处理。
Internal Service Layer 只操作 `model.Message` 和 `model.Request`，不感知 Provider 特定格式。

### 数据流

```
User                                Agent Loop
  │                                     │
  │  input <- Prompt("hello")           │
  │ ──────────────────────────────→     │
  │                                     ├─→ ContextBuilder.Build(session)
  │                                     │     → *model.Request
  │                                     ├─→ provider.NewStreaming(request)
  │                                     ├─→ streamAndRecord(stream, session, output)
  │     ←──────────────────────────     │     → TextEvent
  │  output: TextEvent                  │
  │     ←──────────────────────────     │     → TextEvent
  │  output: TextEvent                  │
  │                                     │     → ToolStartEvent
  │     ←──────────────────────────     │
  │  output: ToolStartEvent             │
  │                                     ├─→ tool.Handle(ctx, args)
  │  input <- Steer("检查测试")         │
  │ ──────────────────────────────→     │  ← Steer 排队，当前轮工具完成后下一轮生效
  │                                     │
  │     ←──────────────────────────     │     → ToolEndEvent
  │  output: ToolEndEvent               │
  │     ←──────────────────────────     │     → TurnEndEvent{HasText: false}
  │  output: TurnEndEvent               │
  │                                     │  ... 下一轮（含 Steer 内容）...
  │     ←──────────────────────────     │     → TextEvent ...
  │     ←──────────────────────────     │     → TurnEndEvent{HasText: true}
  │  output: TurnEndEvent               │
  │                                     │  ← 等待用户决定是否继续
  │  close(input)                       │  → output 关闭
```

### 与现有代码的关系

| 现有类型 | 新角色 | 说明 |
|---------|--------|------|
| `*Message` | `model.Message` | 移到 model/ 包，仍用于 Session 存储和 Provider 通信，不再是用户 API |
| `Generator[*Message, error]` | 被替代 | Agent.Run 改为返回 `(<-chan OutputEvent, error)` |
| `*Invocation` | 去掉 | Session 通过 context 传递，配置在构造时确定 |
| `ModelProvider` | `model.Provider` | 移到 model/ 包，接口不变 |
| `Session` | `session.Session` | 移到 session/ 包，仍存储 `[]*model.Message` |
| `Middleware` | 拆分 | 从 `func(Handler) Handler` 变为 `InputMiddleware` + `OutputMiddleware` |

---

## 模块 1：Agent Loop 状态机

Agent Loop 是 Agent.Run 内部启动的 goroutine。它从 input channel 读取 Event，驱动状态转换，向 output channel 写入 Event。

### 状态定义

```go
type AgentState int
const (
    StateIdle      AgentState = iota // 等待输入
    StatePreparing                    // 构建上下文（压缩、组装 model.Request）
    StateStreaming                    // 模型正在生成
    StateActing                       // 执行工具调用
    StateDone                         // 终止
)
```

### 状态转换规则

```
Idle      ──[PromptEvent]──────→ Preparing     (开始新一轮)
Idle      ──[ControlEvent:Abort]→ Done          (直接终止)

Preparing ──[context ready]────→ Streaming      (调用 Provider)
Preparing ──[over budget]──────→ Preparing      (内部压缩，不产出 Event)

Streaming ──[text delta]───────→ Streaming      (yield TextEvent)
Streaming ──[thinking delta]───→ Streaming      (yield ThinkingEvent)
Streaming ──[tool calls]───────→ Acting         (yield ToolStartEvent)
Streaming ──[model stop]───────→ Idle           (yield TurnEndEvent{HasText: true})
Streaming ──[model error]──────→ Done           (yield DoneEvent{Reason: Error})

Acting    ──[tool done, more]──→ Acting         (yield ToolEndEvent, 继续下一个工具)
Acting    ──[all tools done]───→ Preparing      (yield TurnEndEvent{HasText: false}, 下一轮)
Acting    ──[exit signal]──────→ Idle           (yield TurnEndEvent{HasText: true})
Acting    ──[max turns]────────→ Done           (yield DoneEvent{Reason: MaxTurns})

Any       ──[ControlEvent:Abort]→ Done          (yield DoneEvent{Reason: Aborted})
Any       ──[SteerEvent]────────→ (queue)       (排队，当前轮工具完成后下一轮生效)
```

注意：`model stop`（模型正常结束，无工具调用）转换到 `Idle` 并发送 `TurnEndEvent`，不发送 `DoneEvent`。
`DoneEvent` 严格表示 Agent 生命周期终止，只在 `max turns`、`abort`、`error` 时发送。

### TurnState（不可变每轮状态）

```go
// TurnState 是每轮的不可变状态快照。
// 每次迭代重建，不原地修改，便于调试和回溯。
type TurnState struct {
    Messages           []*Message
    Turn               int
    TokenCount         int64
    TokenBudget        int64
    AutoCompactStats   AutoCompactStats
    MaxOutputRecovery  int
}

type AutoCompactStats struct {
    CompactionCount int
    LastCompactTurn int
    TotalSaved      int64
}
```

### 双循环结构

Agent Loop 内部采用双循环：外层等待输入 Event，内层处理 steering + tool 执行。

```go
func (a *agent) Run(ctx context.Context, input <-chan InputEvent) (<-chan OutputEvent, error) {
    if a.model == nil {
        return nil, ErrProviderRequired
    }
    output := make(chan OutputEvent, 16)
    go a.loop(ctx, input, output)
    return output, nil
}

func (a *agent) loop(ctx context.Context, input <-chan InputEvent, output chan<- OutputEvent) {
    defer close(output)
    state := a.buildInitialState()

    for {
        // 外循环：等待输入 Event
        select {
        case <-ctx.Done():
            output <- &DoneEvent{Reason: ReasonAborted}
            return
        case event, ok := <-input:
            if !ok { return } // input 关闭，Agent 结束
            switch e := event.(type) {
            case *PromptEvent:
                a.handlePrompt(ctx, e, input, output, state)
            case *ControlEvent:
                if e.Action == ActionAbort {
                    output <- &DoneEvent{Reason: ReasonAborted}
                    return
                }
            }
        }
    }
}

func (a *agent) handlePrompt(ctx context.Context, prompt *PromptEvent,
    input <-chan InputEvent, output chan<- OutputEvent, state *TurnState) {

    var steerQueue []*SteerEvent

    // 内循环：steering + tool
    for state.Turn < a.maxTurns {
        state = a.rebuildTurnState(state)
        state = a.applyCompression(ctx, state)

        // 注入排队的 steer 消息（FIFO 顺序）
        for _, steer := range steerQueue {
            state.Messages = append(state.Messages, UserMessage(steer.Content))
        }
        steerQueue = steerQueue[:0]

        // 调用 Provider，流式输出 + 记录到 session
        req, _ := a.contextBuilder.Build(ctx, a.session, a.tools)
        stream := a.model.NewStreaming(ctx, req)
        msg, toolCalls, _ := a.streamAndRecord(ctx, stream, a.session, output)

        if len(toolCalls) > 0 {
            a.executeTools(ctx, output) // 写入 ToolStartEvent/ToolEndEvent
            output <- &TurnEndEvent{Turn: state.Turn, HasText: false}

            // 非阻塞读取：检查是否有新的 SteerEvent
            for {
                select {
                case event := <-input:
                    if s, ok := event.(*SteerEvent); ok {
                        steerQueue = append(steerQueue, s)
                    }
                default:
                    goto drained
                }
            }
        drained:
            state.Turn++
            continue
        }

        // 模型正常结束（无工具调用）——发 TurnEndEvent，不发 DoneEvent
        output <- &TurnEndEvent{Turn: state.Turn, HasText: true}
        return
    }

    // 超过最大轮次——这是 Agent 终止，发 DoneEvent
    output <- &DoneEvent{Reason: ReasonMaxTurns}
}
```

### 关键设计决策

1. **状态机而非隐式循环** — 当前 `handle()` 的状态转换埋在 if/continue/break 中。新设计通过显式 `AgentState` 和转换规则表，让状态流可声明、可测试、可可视化。

2. **不可变 TurnState** — 当前原地修改 `localMessages` 切片。新设计每轮重建 `TurnState`，压缩策略接收旧状态返回新状态，状态流清晰可追踪。

3. **Channel 驱动** — Agent.Run 启动 goroutine，从 input channel 读取，向 output channel 写入。channel 的 close 语义天然控制生命周期：用户 close(input) → Agent 结束 → close(output) → for range 退出。

4. **Steer 非阻塞读取** — 工具执行完成后，通过 select + default 非阻塞读取 input channel 中排队的 SteerEvent。不阻塞等待，有就注入，没有就继续下一轮。

---

## 包结构设计

### 现有结构的问题

根包 `blades/` 承载了 Agent、Message、Session、Runner、Middleware、State、Invocation、Compressor 等所有核心类型。Go 不允许循环依赖，互相引用的类型被迫放在同一个包，导致根包职责过重。

新设计引入 Event 作为核心类型、去掉 Invocation，是重新组织包结构的好时机。

### 设计原则

1. **根包只放用户 API** — `Agent` 接口、`InputEvent`/`OutputEvent`、`NewAgent()`
2. **model/ 是纯类型包** — Message、Provider、Request/Response，不含适配逻辑
3. **依赖方向单一** — 上层依赖下层，不反向，无循环
4. **`package.Role` 命名** — 包名是名词，类型名是角色

### 包结构

```
blades/                         根包：用户 API（Agent + Event）
├── agent.go                    Agent 接口 + NewAgent() 构造函数
├── event.go                    InputEvent / OutputEvent + 所有 Event 类型
├── errors.go                   公共错误
│
├── model/                      LLM 模型层（纯类型定义 + 接口）
│   ├── message.go              Message, Role, Status, 构造函数
│   ├── part.go                 Part 接口, TextPart, FilePart, DataPart, ToolPart
│   ├── provider.go             Provider 接口
│   ├── request.go              Request, Response
│   └── token.go                TokenUsage
│
├── session/                    会话持久化
│   ├── store.go                session.Store 接口 + Session 接口
│   ├── memory.go               内存实现
│   ├── file.go                 JSONL 文件实现
│   ├── entry.go                session.Entry / session.Header / session.Snapshot
│   └── tree.go                 session.Tree（消息树）
│
├── tools/                      工具系统（不依赖 model/）
│   ├── tool.go                 tools.Tool 核心接口 + 可选能力接口
│   ├── handler.go              tools.Handler
│   ├── resolver.go             tools.Resolver
│   ├── context.go              tools.Context
│   └── exit.go                 tools.ExitTool
│
├── memory/                     Memory 系统
│   ├── store.go                memory.Store 接口
│   ├── loader.go               memory.Loader
│   ├── entry.go                memory.Entry / memory.Type
│   ├── extractor.go            memory.Extractor（自动提取）
│   └── section.go              memory.Section（prompt 注入）
│
├── prompt/                     System Prompt 构建
│   ├── builder.go              prompt.Builder
│   └── section.go              prompt.Section / prompt.Breakpoint
│
├── compact/                    上下文压缩
│   ├── pipeline.go             compact.Pipeline
│   ├── strategy.go             compact.Strategy 接口
│   ├── snip.go                 Snip 策略
│   ├── window.go               滑动窗口策略
│   ├── summary.go              LLM 摘要策略（接受 Summarizer 函数）
│   ├── budget.go               工具结果预算策略
│   └── auto.go                 自动压缩策略
│
├── token/                      Token 计数
│   ├── counter.go              token.Counter 接口
│   ├── char.go                 字符估算实现（1 token ≈ 4 chars）
│   └── provider.go             Provider 原生计数适配
│
├── hook/                       Hook 系统
│   ├── event.go                hook.Event 类型（Agent/Model/Tool 核心事件）
│   ├── registry.go             hook.Registry
│   └── handler.go              hook.ObserveHandler / 拦截型 Handler
│
├── permission/                 权限系统
│   ├── chain.go                permission.Chain
│   ├── rule.go                 permission.Rule
│   └── mode.go                 permission.Mode
│
├── retry/                      API 错误处理与重试
│   ├── policy.go               retry.Policy
│   └── backoff.go              retry.Backoff
│
├── skills/                     技能系统
│   ├── skill.go                skills.Skill 接口
│   ├── loader.go               skills.Loader
│   ├── toolset.go              skills.Toolset
│   └── models.go               skills.Frontmatter
│
├── flow/                       组合 Agent
│   ├── sequential.go           flow.SequentialAgent
│   ├── parallel.go             flow.ParallelAgent
│   ├── loop.go                 flow.LoopAgent
│   ├── routing.go              flow.RoutingAgent
│   ├── deep.go                 flow.DeepAgent
│   └── graph.go                flow.GraphAgent（graph 桥接）
│
├── graph/                      DAG 执行器（独立子系统）
│   ├── graph.go                graph.Graph
│   ├── executor.go             graph.Executor
│   ├── task.go                 graph.Task
│   └── checkpoint.go           graph.Checkpointer
│
├── middleware/                  中间件
│   ├── retry.go                middleware.Retry（Agent 级重试）
│   ├── logging.go              middleware.Logging
│   └── otel.go                 middleware.OTel（可观测性集成）
│
├── recipe/                     声明式构建
│   ├── spec.go                 recipe.Spec
│   ├── builder.go              recipe.Builder
│   └── registry.go             recipe.Registry
│
├── evaluator/                  评估器
│   ├── evaluator.go            evaluator.Evaluator
│   └── criteria.go             evaluator.Criteria
│
├── contrib/                    Provider 实现（各自实现 model.Provider + 内部格式转换）
│   ├── anthropic/              contrib/anthropic.Claude
│   ├── openai/                 contrib/openai.Chat
│   ├── gemini/                 contrib/gemini.Gemini
│   ├── mcp/                    contrib/mcp.Resolver
│   └── otel/                   contrib/otel.Tracing
│
├── internal/                   内部实现
│   ├── handoff/                路由工具
│   └── deep/                   深度 Agent 工具
│
├── cmd/blades/                 CLI 入口
└── examples/                   示例
```

### 与现有结构的变化

| 变化 | 原因 |
|------|------|
| 根包精简为 Agent + Event | Message/Provider 下沉到 model/ 包，根包只保留用户 API |
| 新增 `model/` | 合并 Message + Provider 为纯类型包，是依赖图的叶子节点 |
| `context/` → `compact/` | 避免与标准库 `context` 冲突，且与文档术语（AutoCompact、CompactionData）一致 |
| Session 接口移到 `session/` | 根包不再承载 Session，session/ 包同时定义接口和实现 |
| 新增 `token/` | Token 计数从 internal/counter 提升为公开包，支持多种计数策略 |
| 新增 `retry/` | API 错误处理与重试策略独立为包 |
| 去掉根包 `model.go`、`message.go`、`session.go` | 这些类型分别移到 model/ 和 session/ 包 |

### 依赖关系

```
model/（叶子包：Message + Provider + Request/Response，不依赖任何 blades 子包）
  ↑
  ├── session/（依赖 model/：存储 []*model.Message）
  ├── compact/（依赖 model/：压缩 []*model.Message）
  ├── token/（依赖 model/：计数 model.Message 的 token）
  ├── tools/（独立，不依赖 model/）
  ├── hook/（独立）
  ├── permission/（独立）
  ├── prompt/（独立）
  ├── retry/（独立）
  ├── memory/（独立）
  ↑
  ├── skills/（依赖 tools/）
  ↑
  ├── blades/（根包：依赖 model/, session/, compact/, tools/, hook/, permission/, prompt/, retry/）
  │   └── Agent Loop 内部实现 ContextBuilder（含消息过滤）和 streamAndRecord
  ↑
  ├── flow/（依赖 blades/ 根包：Agent 接口 + Event 类型）
  ├── middleware/（依赖 blades/ 根包）
  ├── recipe/（依赖 blades/, tools/, flow/, model/）
  ├── evaluator/（依赖 blades/）
  ↑
  ├── contrib/*（依赖 model/：实现 model.Provider，各自处理格式转换）
  ↑
  └── cmd/blades/（依赖所有）
```

依赖方向严格单向，无循环。model/ 是叶子包，不依赖任何 blades 子包。
根包依赖 model/ 是向下依赖。compact/ 通过 `Summarizer` 函数注入避免对根包的循环依赖。

### 各包的核心导出类型

| 包 | 核心类型 | 读作 |
|---|---------|------|
| `blades` | `Agent`, `InputEvent`, `OutputEvent` | `blades.Agent` |
| `model` | `Message`, `Provider`, `Request`, `Response`, `Part`, `TokenUsage` | `model.Provider` |
| `session` | `Session`, `Store`, `Entry`, `Snapshot`, `Tree` | `session.Store` |
| `tools` | `Tool`, `ConcurrentTool`, `ReadOnlyTool`, `Handler`, `Resolver` | `tools.Tool` |
| `compact` | `Pipeline`, `Strategy` | `compact.Pipeline` |
| `token` | `Counter` | `token.Counter` |
| `hook` | `Event`, `Registry`, `ObserveHandler` | `hook.Registry` |
| `permission` | `Chain`, `Rule`, `Mode`, `ModeManager`, `SafetyChecker`, `AcceptEditsChecker`, `Classifier`, `AutoModeController` | `permission.Chain` |
| `prompt` | `Builder`, `Section`, `SystemPrompt` | `prompt.Builder` |
| `retry` | `Policy`, `Backoff` | `retry.Policy` |
| `memory` | `Store`, `Loader`, `Entry`, `Extractor` | `memory.Store` |
| `flow` | `SequentialAgent`, `ParallelAgent`, `LoopAgent` | `flow.LoopAgent` |
| `graph` | `Graph`, `Executor`, `Checkpointer` | `graph.Executor` |
| `middleware` | `Retry`, `Logging`, `OTel` | `middleware.Retry` |

---

## 模块 2：消息与上下文系统

### 现状对比

| 维度 | 当前 Blades | 新设计 |
|------|------------|--------|
| 消息类型 | `Part` 密封接口（4 种类型） | 内置 7 种 Part 类型，暂不开放注册 |
| 消息过滤 | 无（直接发给 Provider） | ContextBuilder 内部 `filterForProvider` 私有方法 |
| 上下文压缩 | 单一 `ContextCompressor` | 5 策略 `CompressionPipeline` |
| System Prompt | 简单字符串 | 缓存感知 `prompt.Builder` |

### 2.1 内置消息类型

Part 保持判别联合风格，所有类型内置在 `model/` 包中。后续如有第三方扩展需求，再考虑开放注册机制。

```go
type Part interface{ part() }

// 基础类型
type TextPart struct { Text string `json:"text"` }
type FilePart struct { URI string `json:"uri"`; MimeType string `json:"mimeType"` }
type DataPart struct { Data any `json:"data"` }

// 工具调用
type ToolUsePart struct { CallID string `json:"callId"`; Name string `json:"name"`; Args string `json:"args"` }
type ToolResultPart struct { CallID string `json:"callId"`; Content string `json:"content"`; Err error `json:"-"` }

// 扩展内置类型
type ThinkingPart struct { Text string `json:"text"` }
type CompactionSummaryPart struct {
    Summary      string `json:"summary"`
    TokensBefore int64  `json:"tokensBefore"`
    TokensAfter  int64  `json:"tokensAfter"`
}
```

消息过滤/转换在 `ContextBuilder.Build()` 内部完成（参见 Service Layer 设计），不暴露独立接口：
- TextPart, FilePart, DataPart, ToolUsePart, ToolResultPart → 保留
- ThinkingPart → 根据 provider 能力决定保留或转为文本
- CompactionSummaryPart → 转为 system message

### 2.2 多策略压缩管线

```go
// CompressionStrategy 是单个压缩策略。
type CompressionStrategy interface {
    Name() string
    ShouldApply(ctx context.Context, state *CompressionState) bool
    Apply(ctx context.Context, state *CompressionState) (*CompressionState, error)
}

// CompressionState 携带压缩管线所需的全部信息。
type CompressionState struct {
    Messages       []*Message
    SystemPrompt   string
    TokenCount     int64
    TokenBudget    int64
    TurnCount      int
    CompactionHist []CompactionRecord
}

type CompactionRecord struct {
    Turn         int
    Strategy     string
    TokensBefore int64
    TokensAfter  int64
    Timestamp    int64
}

// CompressionPipeline 按顺序应用策略，token 降到预算内即短路。
type CompressionPipeline struct {
    strategies []CompressionStrategy
    counter    TokenCounter
}

func (p *CompressionPipeline) Compress(
    ctx context.Context, state *CompressionState,
) (*CompressionState, error) {
    for _, s := range p.strategies {
        if state.TokenCount <= state.TokenBudget {
            break // 已在预算内，短路
        }
        if s.ShouldApply(ctx, state) {
            var err error
            state, err = s.Apply(ctx, state)
            if err != nil {
                return state, err
            }
        }
    }
    return state, nil
}
```

#### 5 种内置策略

| 策略 | 触发条件 | 作用范围 | 说明 |
|------|---------|---------|------|
| `ToolResultBudget` | 每轮开始 | 单个工具结果 | 超大结果持久化到磁盘，向模型发送截断预览 + 磁盘路径 |
| `Snip` | 每轮开始 | 最旧消息 | 硬限制：当消息数超过阈值时丢弃最旧消息 |
| `MicroCompact` | 每轮开始 | 小窗口旧消息 | 对小窗口内的旧消息做内联摘要替换，不调用 LLM |
| `AutoCompact` | token 阈值 | 全部/部分对话 | 通过 Fork Agent 调用 LLM 生成完整摘要 |
| `ReactiveCompact` | API 413 错误 | 全部对话 | 紧急恢复：强制全量压缩 |

```go
// ToolResultBudgetStrategy 处理超大工具结果。
type ToolResultBudgetStrategy struct {
    MaxResultChars int    // 每个工具结果的字符上限，默认 30000
    PersistDir     string // 完整结果持久化目录
}

// SnipStrategy 硬限制丢弃最旧消息。
type SnipStrategy struct {
    MaxMessages int // 消息数上限
}

// MicroCompactStrategy 对小窗口旧消息做内联摘要。
type MicroCompactStrategy struct {
    WindowSize int // 每次处理的消息窗口大小
}

// AutoCompactStrategy 通过 LLM 生成摘要。
// 注意：不直接持有 Agent 引用，避免 compact 包与根包循环依赖。
// 改为接受 Summarizer 函数，由 Agent Loop 在构造时注入具体实现
//（可以是 ForkAgent，也可以是直接的 LLM 调用）。
type AutoCompactStrategy struct {
    TokenThreshold    int64                                                    // 触发阈值（tokenBudget - bufferTokens）
    BufferTokens      int64                                                    // 预留 buffer，默认 13000
    MaxFilesToRestore int                                                      // 压缩后恢复的最近文件数，默认 5
    FileBudgetTokens  int64                                                    // 文件恢复 token 预算，默认 50000
    Summarize         func(ctx context.Context, messages []*Message) (string, error) // 由 Agent Loop 注入
}

// ReactiveCompactStrategy 紧急恢复压缩。
type ReactiveCompactStrategy struct {
    Summarize func(ctx context.Context, messages []*Message) (string, error) // 由 Agent Loop 注入
}
```

### 2.3 缓存感知 System Prompt

```go
package prompt

// Builder 将 system prompt 分为静态可缓存前缀和动态后缀。
// 静态部分跨会话缓存（如工具描述、行为指南），动态部分每会话变化（如 Memory、环境信息）。
type Builder struct {
    staticSections  []Section
    dynamicSections []Section
}

type Section struct {
    Name     string
    Priority int // 数字越小优先级越高
    Provider func(ctx context.Context) (string, error)
}

type SystemPrompt struct {
    Static       string        // 可缓存前缀
    Dynamic      string        // 每会话变化后缀
    Full         string        // Static + Dynamic
    CacheControl []Breakpoint
}

type Breakpoint struct {
    Offset int
    Scope  CacheScope
}

type CacheScope string
const (
    ScopeGlobal  CacheScope = "global"  // 跨组织可缓存
    ScopeSession CacheScope = "session" // 会话内缓存
)

// Build 构建完整的 system prompt，工具按名称排序以保证缓存稳定性。
func (b *Builder) Build(ctx context.Context) (*SystemPrompt, error)

// 静态 section 示例：
// - intro: "You are an agent that..."
// - tool_rules: 工具使用规则
// - task_guidance: 任务方法指导
// - safety: 安全指导
// - style: 输出风格

// 动态 section 示例：
// - memory: BLADES.md 文件内容
// - env_info: CWD、git 状态、OS、模型名
// - mcp_instructions: MCP 服务器指令
// - skills: 可用技能列表
```

### 关键设计决策

1. **内置 Part 类型而非开放注册** — 当前阶段需要的 Part 类型是确定的（7 种），直接作为 `model/` 包的具体类型。不引入 `CustomPart` 接口和 `PartRegistry` 注册表，避免过早抽象。后续如有第三方扩展需求，再考虑开放注册机制。

2. **消息过滤内聚于 ContextBuilder** — 消息过滤/转换（ThinkingPart 处理、CompactionSummaryPart 转换等）作为 `ContextBuilder.Build()` 的私有方法实现，不暴露独立的 `MessageConverter` 接口。Provider 特定的格式差异（Anthropic tool_use/tool_result 拆分、OpenAI function_call 格式等）由各 `contrib/*` 包在实现 `model.Provider` 时内部处理。

3. **管线式压缩而非单一压缩器** — 当前 `ContextCompressor` 是全有或全无的单一接口。新设计将压缩分解为 5 个独立策略，按成本从低到高排列，token 降到预算内即短路。轻量策略（Snip、MicroCompact）每轮都运行，重量策略（AutoCompact）仅在阈值触发时运行。压缩策略通过 `Summarizer` 函数注入 LLM 能力，避免与根包循环依赖。

4. **缓存感知 System Prompt** — 当前 system prompt 是简单字符串，每次调用都完整发送。新设计将 prompt 分为静态前缀（跨会话不变）和动态后缀（每会话变化），配合 Provider 的 prompt cache 机制（如 Anthropic 的 cache_control），显著降低重复 token 消耗。

---

## 模块 3：工具系统

### 现状对比

| 维度 | 当前 Blades | 新设计 |
|------|------------|--------|
| 并发控制 | 全部并发（errgroup） | 自声明 ConcurrencyMode + 自动分区 |
| 流式执行 | 等模型完成才执行 | StreamingToolExecutor 重叠执行 |
| 生命周期 | 无 Hook | BeforeToolHook / AfterToolHook |
| 结果管理 | 无限制 | ToolResultBudget 截断 + 持久化 |
| 安全声明 | 无 | IsReadOnly / IsDestructive |

### 3.1 Tool 接口（精简核心 + 可选能力）

核心 `Tool` 接口保持精简（4 个方法），扩展能力通过可选接口（interface assertion）实现。
这是 Go 惯用的可选接口模式（类似 `io.WriterTo`、`io.ReaderFrom`）。

```go
// Tool 核心接口，所有工具必须实现。
type Tool interface {
    Name() string
    Description() string
    InputSchema() *jsonschema.Schema
    Handle(ctx context.Context, input string) (string, error)
}

// --- 可选能力接口（通过 type assertion 检查）---

// ConcurrentTool 声明此工具是否可并发执行。
// 未实现此接口的工具默认 Sequential（安全默认值）。
type ConcurrentTool interface {
    ConcurrencyMode() ConcurrencyMode
}

// ReadOnlyTool 声明此工具是否只读。
// 用于权限系统快速判断和 plan 模式过滤。
type ReadOnlyTool interface {
    IsReadOnly() bool
}

// DestructiveTool 声明此工具对给定输入是否有破坏性。
// 用于权限系统决定是否需要确认。
type DestructiveTool interface {
    IsDestructive(input string) bool
}

// PromptContributor 贡献此工具的描述到 system prompt。
// 工具按名称排序注入，保证 prompt cache 稳定性。
type PromptContributor interface {
    Prompt(ctx context.Context) string
}

// BudgetedTool 定义结果大小上限。超出则持久化到磁盘，发送预览。
type BudgetedTool interface {
    MaxResultChars() int
}

// SchemaOutputTool 定义输出 schema（大多数工具不需要）。
type SchemaOutputTool interface {
    OutputSchema() *jsonschema.Schema
}

type ConcurrencyMode int
const (
    Sequential ConcurrencyMode = iota // 必须串行执行
    Concurrent                         // 可安全并发
)
```

执行器通过 type assertion 检查能力，未实现的接口使用安全默认值：

```go
func getConcurrencyMode(t Tool) ConcurrencyMode {
    if ct, ok := t.(ConcurrentTool); ok {
        return ct.ConcurrencyMode()
    }
    return Sequential // 安全默认值
}

func isReadOnly(t Tool) bool {
    if rt, ok := t.(ReadOnlyTool); ok {
        return rt.IsReadOnly()
    }
    return false // 安全默认值
}
```

```go
// ToolBuilder 提供安全默认值，降低新工具实现成本。
// Build() 返回的工具自动实现所有可选接口。
type ToolBuilder struct {
    name            string
    description     string
    inputSchema     *jsonschema.Schema
    outputSchema    *jsonschema.Schema
    handler         ToolHandler
    concurrency     ConcurrencyMode
    readOnly        bool
    destructive     func(string) bool
    prompt          func(context.Context) string
    maxResultChars  int
    middleware      []ToolMiddleware
}

func NewToolBuilder(name, description string) *ToolBuilder
func (b *ToolBuilder) WithConcurrency(mode ConcurrencyMode) *ToolBuilder
func (b *ToolBuilder) WithReadOnly(readOnly bool) *ToolBuilder
func (b *ToolBuilder) WithMaxResultChars(max int) *ToolBuilder
func (b *ToolBuilder) Build() Tool
```

### 3.2 流式工具执行

```go
// StreamingToolExecutor 在模型仍在流式输出时就开始执行工具。
// 并发安全的工具在 tool call 参数完整后立即启动，
// 串行工具排队等待。执行与模型生成重叠，降低端到端延迟。
type StreamingToolExecutor struct {
    tools   map[string]Tool
    hooks   *HookRegistry
    budget  *ToolResultBudget
    maxConc int // 最大并发数，默认 10
}

// ExecuteStreaming 接收模型流式输出中逐步到达的 tool call。
// 返回按原始顺序排列的结果流。
func (e *StreamingToolExecutor) ExecuteStreaming(
    ctx context.Context,
    toolCalls <-chan ToolCall,
) Generator[*ToolResult, error]
```

执行流程：

```
模型流式输出:  [text...] [tool_call_1 ✓] [tool_call_2 ...] [tool_call_3 ✓] [done]
                              │                                    │
工具执行:              start(1) ──────────────────────────── start(3)
                       (concurrent)                          (concurrent)
                                          tool_call_2 完整后 → start(2) (sequential)
结果缓冲:              [result_1] ──────── [result_2] ──────── [result_3]
                       (按原始顺序 yield)
```

### 3.3 自动并发分区

```go
// partitionToolCalls 将连续的工具调用按并发模式分组。
// 同一分区内的并发工具并行执行，串行工具顺序执行。
//
// 示例：[bash, read, read, edit, grep, grep]
//   partition 0: [bash]       → sequential
//   partition 1: [read, read] → concurrent
//   partition 2: [edit]       → sequential
//   partition 3: [grep, grep] → concurrent
func partitionToolCalls(
    calls []ToolCall, tools map[string]Tool,
) []toolPartition

type toolPartition struct {
    Mode  ConcurrencyMode
    Calls []ToolCall
}

// runPartitions 按分区顺序执行，分区内按模式并发或串行。
func runPartitions(
    ctx context.Context,
    partitions []toolPartition,
    executor func(context.Context, ToolCall) (*ToolResult, error),
) ([]*ToolResult, error)
```

### 3.4 工具生命周期 Hook

```go
// BeforeToolHook 在工具执行前调用。可阻止执行或修改输入。
type BeforeToolHook func(ctx context.Context, call *ToolCall) (*BeforeToolResult, error)

type BeforeToolResult struct {
    Block        bool   // true = 阻止执行
    Reason       string // 阻止原因
    ModifiedArgs string // 修改后的参数（空 = 不修改）
}

// AfterToolHook 在工具执行后调用。可修改结果。
type AfterToolHook func(ctx context.Context, call *ToolCall, result *ToolResult) (*ToolResult, error)
```

### 3.5 工具执行完整生命周期

```
1. 参数校验          ← JSON Schema 校验
2. BeforeToolHook    ← 可阻止执行或修改参数
3. 权限检查          ← PermissionChain.Check()
4. tool.Handle()     ← 实际执行，支持流式进度
5. AfterToolHook     ← 可修改结果
6. ToolResultBudget  ← 超大结果截断 + 持久化
7. 发射事件          ← EventToolExecEnd
```

### 关键设计决策

1. **默认 Sequential + 可选接口** — 当前 Blades 所有工具默认并发执行，这是不安全的默认值（如两个 bash 命令并发可能冲突）。新设计默认 Sequential，工具通过实现 `ConcurrentTool` 可选接口显式声明并发安全。核心 `Tool` 接口保持 4 个方法，扩展能力通过 type assertion 检查，这是 Go 惯用的可选接口模式（类似 `io.WriterTo`）。

2. **流式工具执行** — 当前必须等模型完整输出后才开始执行工具。新设计在模型流式输出过程中，一旦某个 tool call 的参数完整就立即启动执行（如果是并发安全的），模型生成和工具执行时间重叠，显著降低端到端延迟。

3. **ToolResultBudget** — 当前工具结果无大小限制，大文件读取可能撑爆上下文。新设计为每个工具设置结果大小上限，超出时完整结果持久化到磁盘，向模型发送截断预览 + 磁盘路径引用。

---

## 模块 4：扩展与 Hook 系统

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

---

## 模块 5：会话与持久化

### 现状对比

| 维度 | 当前 Blades | 新设计 |
|------|------------|--------|
| 存储 | 仅内存（sessionInMemory） | JSONL 文件 + 内存双实现 |
| 结构 | 线性数组 | parentId 链形成消息树 |
| 分支 | 不支持 | 支持分支、导航、摘要 |
| 压缩历史 | 丢弃 | 完整保留在 JSONL 中 |
| 并发安全 | sync.Mutex | 追加写入，天然并发安全 |

### 5.1 session.Store 接口

```go
package session

// Store 是会话持久化的抽象接口。
// 注意：不提供 LoadBranch 方法。分支加载通过 Load() 返回的 Snapshot.Tree.Path(leafID) 组合实现。
// 如果未来需要避免加载完整 Tree 的性能优化（如超大会话），可以在具体实现中添加优化路径，
// 但不在接口层暴露，保持接口精简。
type Store interface {
    Create(ctx context.Context, header Header) error
    Append(ctx context.Context, sessionID string, entries ...Entry) error
    Load(ctx context.Context, sessionID string) (*Snapshot, error)
    List(ctx context.Context) ([]Header, error)
}

type Header struct {
    Version   int    `json:"version"`
    ID        string `json:"id"`
    CreatedAt int64  `json:"createdAt"`
    CWD       string `json:"cwd"`
    Title     string `json:"title,omitempty"`
    Leaf      string `json:"leaf"` // 当前位置指针
}
```

### 5.2 session.Entry 联合类型

```go
// Entry 是 JSONL 文件中每行的结构。
// 通过 ID + ParentID 构成树形结构。
type Entry struct {
    Type      EntryType       `json:"type"`
    ID        string          `json:"id"`
    ParentID  string          `json:"parentId,omitempty"`
    Timestamp int64           `json:"timestamp"`
    Data      json.RawMessage `json:"data"`
}

type EntryType string
const (
    EntryMessage          EntryType = "message"           // 对话消息
    EntryCompaction       EntryType = "compaction"        // 压缩摘要
    EntryModelChange      EntryType = "model_change"      // 模型切换
    EntryBranchSummary    EntryType = "branch_summary"    // 分支摘要
    EntryTitle            EntryType = "title"             // 会话标题
    EntryConfigChange     EntryType = "config_change"     // 配置变更
    EntryCustom           EntryType = "custom"            // 扩展自定义状态
    EntryContentReplace   EntryType = "content_replace"   // 工具结果存根替换
)

// CompactionData 是 EntryCompaction 的 Data 结构。
type CompactionData struct {
    Summary          []*model.Message `json:"summary"`
    FirstKeptEntryID string            `json:"firstKeptEntryId"`
    TokensBefore     int64             `json:"tokensBefore"`
    TokensAfter      int64             `json:"tokensAfter"`
}

// MessageData 是 EntryMessage 的 Data 结构。
type MessageData struct {
    Message     *model.Message `json:"message"`
    IsSidechain bool            `json:"isSidechain,omitempty"`
    AgentID     string          `json:"agentId,omitempty"`
    AgentName   string          `json:"agentName,omitempty"`
    QuerySource string          `json:"querySource,omitempty"`
}
```

### 5.3 消息树

```go
// Tree 通过 ParentID 链构建消息树。
// 当前阶段只实现线性路径恢复，分支导航能力后续按需添加。
type Tree struct {
    Root     *TreeNode
    nodeByID map[string]*TreeNode
    leaf     string // 当前位置
}

type TreeNode struct {
    Entry    Entry
    Children []*TreeNode
    Parent   *TreeNode
}

// Branch 移动 leaf 指针到指定节点，不修改历史。
func (t *Tree) Branch(nodeID string) error

// Path 返回从根到指定节点的消息序列。
func (t *Tree) Path(nodeID string) []*model.Message

// BranchWithSummary、Branches 等分支导航方法后续按需添加。
// 数据格式（ParentID 链）已前向兼容，不影响未来扩展。
```

### 5.4 session.Snapshot

```go
// Snapshot 是加载会话后的完整快照。
type Snapshot struct {
    Header   Header
    Messages []*model.Message // 当前分支的消息序列
    Tree     *Tree             // 完整消息树（用于导航）
    State    blades.State      // 会话状态（key-value）
}
```

### 5.5 文件实现

```go
// fileStore 使用 JSONL 文件实现 session.Store。
// 文件位置：~/.blades/sessions/<project-slug>/<sessionId>.jsonl
//
// JSONL 格式：
//   第 1 行：Header（JSON）
//   第 2+ 行：Entry（每行一个 JSON）
//
// 追加写入，天然并发安全。元数据（title、leaf）采用 last-wins 读取策略。
type fileStore struct {
    baseDir string
}

func NewFileStore(baseDir string) Store
```

### 5.6 会话恢复流程

```
1. 读取 JSONL 文件
2. 解析 session.Header
3. 通过 ParentID 链重建 session.Tree
4. 定位 Leaf 节点，提取当前分支路径
5. 回放 CompactionData（压缩边界之前的消息替换为摘要）
6. 回放 ContentReplace（工具结果存根替换）
7. 恢复 State（从 custom 条目）
8. 返回 session.Snapshot
```

### 关键设计决策

1. **JSONL 追加写入** — 当前内存实现在进程退出后丢失所有状态。JSONL 追加写入天然并发安全（多个 goroutine 可同时追加），且支持增量恢复（不需要读取整个文件来追加新条目）。

2. **树形结构而非线性** — 通过 `ParentID` 链形成树，支持原地分支而无需创建新文件。用户可以回溯到任意历史节点创建新分支，旧分支保留在同一文件中。

3. **压缩历史完整保留** — 压缩时不删除旧消息，而是追加 `CompactionData` 条目标记压缩边界。加载时用摘要替换边界之前的消息。完整历史始终可从 JSONL 文件恢复。

4. **session.Store 接口 + 双实现** — 保留现有的内存实现用于测试和无状态场景，新增文件实现用于持久化场景。通过接口抽象，未来可扩展到数据库等其他存储后端。

---

## 模块 6：权限系统

### 现状对比

| 维度 | 当前 Blades | 新设计 |
|------|------------|--------|
| 权限控制 | 仅 `Confirm` 中间件 | 分层决策链（7 层） |
| 权限模式 | 无 | 4 种核心模式（default/accept_edits/plan/auto） |
| 规则配置 | 无 | 多来源规则（CLI/session/project/user/policy） |
| 安全检查 | 无 | bypass-immune SafetyChecker |
| 模式管理 | 无 | ModeManager 状态机（含 plan 暂存/恢复） |

### 6.1 权限决策类型

```go
type PermissionDecision string
const (
    PermissionAllow       PermissionDecision = "allow"
    PermissionDeny        PermissionDecision = "deny"
    PermissionAsk         PermissionDecision = "ask"
    PermissionPassthrough PermissionDecision = "passthrough"
)

// Mode 控制整体权限行为。
// 当前实现 4 种核心模式，其余预留枚举值供后续扩展。
type Mode string
const (
    // ModeDefault 是标准交互模式。
    // 只读工具自动批准，破坏性操作需用户确认。
    ModeDefault Mode = "default"

    // ModeAcceptEdits 自动批准工作目录内的文件写入工具。
    // Bash、MCP 等其他工具仍需确认。
    // 与 Claude Code 的 acceptEdits 模式对应。
    ModeAcceptEdits Mode = "accept_edits"

    // ModePlan 是只读探索模式。
    // 非只读工具被拒绝，工具列表被过滤（只发送只读工具给模型）。
    // 通过 EnterPlanModeTool/ExitPlanModeTool 进入/退出。
    // 完整生命周期设计见模块 14。
    ModePlan Mode = "plan"

    // ModeAuto 使用可插拔 AI 分类器自主决策。
    // 有 acceptEdits 快速路径（跳过分类器）和熔断器（连续拒绝后降级到 default）。
    // 分类器是纯接口，框架不提供内置实现。
    // 详细设计见模块 14。
    ModeAuto Mode = "auto"

    // --- 预留模式（后续按需实现）---
    // ModeBypassPermissions Mode = "bypass_permissions" // 跳过所有检查（安全检查除外）
    // ModeDenyAll           Mode = "deny_all"           // 拒绝所有，仅规则放行
    // ModeDontAsk           Mode = "dont_ask"           // ask → deny，headless/CI
    // ModeEscalate          Mode = "escalate"           // 子 Agent 决策上报父 Agent
)
```

### 6.2 权限规则

```go
// PermissionRule 是配置的 allow/deny 规则。
type PermissionRule struct {
    Source   PermissionRuleSource
    Behavior PermissionDecision // allow 或 deny
    ToolName string             // 工具名，支持 glob（如 "bash*"）
    Pattern  string             // 输入匹配模式（glob/正则）
}

type PermissionRuleSource string
const (
    SourceCLI     PermissionRuleSource = "cli"      // CLI 参数
    SourceSession PermissionRuleSource = "session"   // 会话内授权
    SourceProject PermissionRuleSource = "project"   // .blades/settings.json
    SourceUser    PermissionRuleSource = "user"      // ~/.blades/settings.json
    SourcePolicy  PermissionRuleSource = "policy"    // 组织策略
)
```

### 6.3 权限决策链

```go
// PermissionChain 通过分层链式判断评估权限。
// 7 层决策管线，每层可短路返回 allow/deny，或 passthrough 到下一层。
type PermissionChain struct {
    rules       []PermissionRule
    modeManager *ModeManager
    safety      SafetyChecker
    acceptEdits *AcceptEditsChecker
    autoCtrl    *AutoModeController
    hooks       *HookRegistry
    promptUser  UserPromptFunc
}

func NewPermissionChain(opts ...PermissionOption) *PermissionChain

// Check 评估工具调用的权限。
func (c *PermissionChain) Check(
    ctx context.Context, toolName string, input string,
) (PermissionDecision, error)
```

决策流程（7 层）：

```
1. 安全检查（bypass-immune，SafetyChecker）
   → SeverityBlock 违规: DENY（绝对，任何模式都无法绕过）
   → SeverityConfirm 违规: ASK（跳过第 2-6 层，直接进入第 7 层后处理）
   → 无违规: 继续

2. 规则匹配（首次匹配生效）
   → allow/deny: 短路返回
   → 无匹配: passthrough

3. 模式决策
   → plan: 非只读工具 → DENY
   → accept_edits: 工作目录内文件写入 → ALLOW，否则 passthrough
   → default/auto: passthrough

4. Hook 拦截（HookPreToolUse）
   → Hook 返回 allow/deny: 短路返回
   → 无 Hook 或 passthrough: 继续

5. 工具自声明检查
   → ReadOnlyTool: ALLOW
   → DestructiveTool(input)=true: ASK
   → 其他: passthrough

6. 默认决策 → ASK

7. 后处理（ASK 的最终处理）
   → auto: 运行 Classifier（见模块 14.3）
   → default/accept_edits: 提示用户
```

### 6.4 权限中间件集成

```go
// PermissionMiddleware 将权限链集成到 Agent 的工具执行流程中。
// 替代当前的 Confirm 中间件，提供更细粒度的控制。
func PermissionMiddleware(chain *PermissionChain) ToolMiddleware {
    return func(next ToolHandler) ToolHandler {
        return ToolHandlerFunc(func(ctx context.Context, input string) (string, error) {
            toolCtx := tools.FromContext(ctx)
            decision, err := chain.Check(ctx, toolCtx.Name(), input)
            if err != nil {
                return "", err
            }
            switch decision {
            case PermissionDeny:
                return "", ErrPermissionDenied
            case PermissionAsk:
                return "", ErrPermissionAsk
            default:
                return next.Handle(ctx, input)
            }
        })
    }
}
```

### 关键设计决策

1. **7 层决策链而非 4 层** — 在原有 4 层（规则 → 模式 → Hook → 用户确认）基础上，新增安全检查（最前面，bypass-immune）、工具自声明检查（ReadOnlyTool/DestructiveTool）、后处理层（auto 模式分类器）。每层职责单一，可独立测试。

2. **规则优先于模式** — 规则在决策链第 2 层（仅次于安全检查），可以精确覆盖特定工具的权限。例如 `allow bash "git *"` 允许所有 git 命令，即使在 default 模式下 bash 通常需要确认。

3. **4 种核心模式 + 预留扩展** — 聚焦 default/accept_edits/plan/auto 四种最常用模式。bypass_permissions、deny_all、dont_ask、escalate 预留枚举值，后续按需实现。避免过早设计不确定的模式语义。

4. **安全检查双级别** — SafetyChecker 在决策链最前面执行，区分 `SeverityBlock`（绝对禁止）和 `SeverityConfirm`（需确认但可覆盖）。路径遍历是绝对禁止的，敏感文件写入需要确认但不绝对禁止（用户可能需要配置 git hooks）。

5. **后处理层分离** — ASK 决策的最终处理（auto 模式运行分类器 vs default 模式提示用户）从决策链主流程中分离到第 7 层，使主流程（1-6 层）保持模式无关。

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
}

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

### 关键设计决策

1. **共享 Prompt Cache** — 当前子 Agent 完全隔离，每次调用都是冷缓存。新设计通过共享静态 system prompt 前缀，使子 Agent 可以命中父 Agent 的 prompt cache，压缩和 Memory 提取等高频操作成本大幅降低。

2. **Fire-and-forget 后台 Agent** — Memory 提取和任务摘要不需要阻塞主循环。BackgroundAgent 在 goroutine 中运行，主循环继续处理用户请求。Drain 机制确保关闭前等待后台任务完成。

3. **QuerySource 行为区分** — 不同来源的 fork 有不同的行为约束。例如 `compact` fork 只需要生成摘要，不需要执行工具；`extract_memory` fork 只能使用只读工具 + Memory 写入工具。QuerySource 标记使这些约束可以在权限链和 Hook 中精确匹配。

---

## 模块 8：Memory 系统

### 现状对比

| 维度 | 当前 Blades | 新设计 |
|------|------------|--------|
| 存储 | InMemoryStore（子串搜索） | 5 层层级 Memory |
| 来源 | 单一内存 | Managed/User/Project/Local/Auto |
| 自动提取 | 无 | 后台 Fork Agent 自动提取 |
| 文件处理 | 无 | @include 解析 + 截断管线 |
| 注入策略 | 全量注入 | globs 条件注入 |

### 8.1 Memory 层级

```go
package memory

// Type 定义 Memory 条目的来源和优先级。
// 加载顺序（优先级从高到低）：
//   Managed → User → Project → Local → Auto
type Type string
const (
    Managed Type = "managed" // ~/.blades/BLADES.md（框架管理）
    User    Type = "user"    // ~/.blades/BLADES.md（用户编写）
    Project Type = "project" // CWD 向上遍历：BLADES.md, .blades/BLADES.md
    Local   Type = "local"   // CWD 向上遍历：BLADES.local.md
    Auto    Type = "auto"    // ~/.blades/memories/*.md（自动提取）
)

// Entry 表示一个加载的 Memory 文件。
type Entry struct {
    Path       string   `json:"path"`
    Type       Type     `json:"type"`
    Content    string   `json:"content"`
    RawContent string   `json:"rawContent"`
    Globs      []string `json:"globs,omitempty"` // 文件匹配模式，决定何时注入
    Parent     string   `json:"parent,omitempty"` // 父文件路径（@include 链）
}
```

### 8.2 memory.Loader

```go
// Loader 发现和加载所有来源的 Memory 文件。
type Loader struct {
    homeDir    string
    projectDir string
    maxDepth   int // @include 最大深度，默认 5
    maxChars   int // 每文件字符上限，默认 40000
}

func NewLoader(homeDir, projectDir string) *Loader

// Load 加载所有 Memory 条目。
func (l *Loader) Load(ctx context.Context) ([]Entry, error)

// LoadForFile 加载与指定文件匹配的 Memory 条目（基于 globs）。
func (l *Loader) LoadForFile(ctx context.Context, filePath string) ([]Entry, error)
```

#### Memory 文件处理管线

```
1. 从磁盘读取文件
2. 剥离 HTML 注释（<!-- ... -->）
3. 解析 YAML frontmatter（globs 等元数据）
4. 解析 @include 指令（最大深度 5）
   - @path        — 绝对路径
   - @./relative  — 相对于当前文件
   - @~/home      — home 目录相对
5. 截断到 maxChars（默认 40000）
6. 返回 memory.Entry
```

### 8.3 Memory 文件格式

```markdown
---
globs: ["*.go", "**/*_test.go"]
---

# 项目约定

- 使用 Go 1.24+
- 测试文件使用 table-driven tests
- 错误处理使用 fmt.Errorf + %w

@./coding-standards.md
@./architecture-decisions.md
```

### 8.4 自动 Memory 提取

```go
// Extractor 在每轮结束后 fire-and-forget 运行，
// 从对话中提取持久性事实写入 ~/.blades/memories/。
type Extractor struct {
    loader     *Loader
    forkConfig blades.ForkConfig
    memDir     string // ~/.blades/memories/
    throttle   *Throttle
}

func NewExtractor(loader *Loader, opts ...ExtractorOption) *Extractor

// Extract 启动后台提取。
// 如果主 Agent 已在当前轮次写入 Memory 文件，则跳过（互斥）。
func (e *Extractor) Extract(ctx context.Context, messages []*model.Message) *blades.BackgroundAgent

// Drain 等待进行中的提取完成（关闭前调用）。
func (e *Extractor) Drain(timeout time.Duration) error
```

提取流程：

```
1. 检查节流（避免过于频繁提取）
2. 检查主 Agent 是否已写入 Memory（互斥）
3. Fork 新 Agent（QuerySource: extract_memory）
   - 工具限制：只读工具 + Memory 目录写入
   - 共享 prompt cache 前缀
4. 从对话中提取持久性事实
   - 用户偏好、项目约定、架构决策
   - 排除：临时状态、调试信息、代码片段
5. 写入 ~/.blades/memories/<topic>.md
   - 更新已有文件或创建新文件
   - 使用 YAML frontmatter 标记类型和描述
```

### 8.5 Memory 注入 System Prompt

```go
// Section 是 prompt.Builder 的动态 section。
// 根据当前工作目录和文件上下文，选择性注入 Memory 内容。
type Section struct {
    loader *Loader
}

func (s *Section) Build(ctx context.Context) (string, error) {
    entries, err := s.loader.Load(ctx)
    if err != nil {
        return "", err
    }

    var sb strings.Builder
    for _, entry := range entries {
        // 按类型分组，高优先级在前
        fmt.Fprintf(&sb, "# %s (%s)\n%s\n\n", entry.Path, entry.Type, entry.Content)
    }
    return sb.String(), nil
}
```

### 关键设计决策

1. **5 层层级而非单一存储** — 当前 `InMemoryStore` 是扁平的键值存储。新设计将 Memory 分为 5 层，从框架管理到自动提取，每层有明确的职责和优先级。项目级 Memory（BLADES.md）类似 Claude Code 的 CLAUDE.md，是团队共享的项目约定。

2. **@include 指令** — Memory 文件可以通过 `@include` 引用其他文件，支持模块化组织。例如项目根目录的 BLADES.md 可以 `@include` 子目录的特定约定文件，避免单文件过大。

3. **globs 条件注入** — 不是所有 Memory 都需要在每次对话中注入。通过 `globs` 字段，Memory 条目只在用户操作匹配的文件时才注入 system prompt，减少不必要的 token 消耗。

4. **自动提取互斥** — 如果主 Agent 在当前轮次已经写入了 Memory 文件（用户显式要求记住某事），自动提取器跳过本轮。避免主 Agent 和后台提取器同时写入同一文件产生冲突。

---

## 模块 9：API 错误处理与重试

### 现状对比

| 维度 | 当前 Blades | 新设计 |
|------|------------|--------|
| 重试 | `middleware/retry.go`（Agent 级） | `retry.Policy`（Provider 级，感知错误类型） |
| 错误分类 | 无 | 按 HTTP 状态码分类处理 |
| 降级 | 无 | 529 模型过载自动降级 |
| 认证刷新 | 无 | 401 自动刷新 token |

### 9.1 RetryPolicy

```go
package retry

// Policy 定义 Provider 级别的重试策略。
// 与 Agent 级 Middleware 不同，RetryPolicy 感知 Provider 的具体错误类型和 streaming 状态，
// 在 Agent Loop 内部的 Provider 调用处直接处理，不需要重建整个轮次。
type Policy struct {
    MaxRetries    int           // 最大重试次数，默认 3
    BaseDelay     time.Duration // 基础退避时间，默认 1s
    MaxDelay      time.Duration // 最大退避时间，默认 60s
    FallbackModel string        // 529 降级模型（如 claude-sonnet-4-6）
    OnRefresh     func(ctx context.Context) error // 401 认证刷新回调
}

// ErrorClassifier 将 Provider 错误分类为可重试/不可重试。
type ErrorClassifier interface {
    Classify(err error) ErrorClass
}

type ErrorClass int
const (
    ClassFatal       ErrorClass = iota // 不可重试（400 参数错误等）
    ClassRetryable                      // 可重试（5xx 服务端错误）
    ClassRateLimit                      // 限流（429），使用 Retry-After 退避
    ClassOverloaded                     // 过载（529），降级到备用模型
    ClassAuthExpired                    // 认证过期（401），刷新后重试
)

// Backoff 计算退避时间。
type Backoff struct {
    Base   time.Duration
    Max    time.Duration
    Jitter float64 // 0-1 之间的抖动因子
}

func (b *Backoff) Duration(attempt int) time.Duration
```

### 9.2 与 Agent Loop 的集成

```go
// Agent Loop 内部的 Provider 调用处：
func (a *agent) callProvider(ctx context.Context, req *model.Request) (iter.Seq2[*model.Response, error], error) {
    for attempt := 0; attempt <= a.retryPolicy.MaxRetries; attempt++ {
        stream := a.model.NewStreaming(ctx, req)
        // ... 消费 stream ...
        if err != nil {
            class := a.classifier.Classify(err)
            switch class {
            case ClassFatal:
                return nil, err
            case ClassAuthExpired:
                if refreshErr := a.retryPolicy.OnRefresh(ctx); refreshErr != nil {
                    return nil, refreshErr
                }
                continue
            case ClassOverloaded:
                if a.retryPolicy.FallbackModel != "" {
                    req.Model = a.retryPolicy.FallbackModel
                }
                fallthrough
            case ClassRetryable, ClassRateLimit:
                time.Sleep(a.backoff.Duration(attempt))
                continue
            }
        }
        return stream, nil
    }
    return nil, ErrMaxRetriesExceeded
}
```

---

## 模块 10：Token 计数

### 现状对比

| 维度 | 当前 Blades | 新设计 |
|------|------------|--------|
| 计数 | `internal/counter`（1 token ≈ 4 chars） | `token.Counter` 接口 + 多实现 |
| 精度 | 粗略估算 | Provider 原生 / tiktoken / 估算三级降级 |
| 使用 | 仅 context/window 和 context/summary | 压缩管线、TurnState、prompt.Builder 全局使用 |

### 10.1 token.Counter 接口

```go
package token

// Counter 计算消息的 token 数量。
type Counter interface {
    Count(messages ...*model.Message) int64
}

// CharCounter 字符估算实现（1 token ≈ 4 chars）。
// 作为降级方案，不需要外部依赖。
type CharCounter struct{}

// ProviderCounter 使用 Provider 原生 token 计数 API。
// 如 Anthropic 的 /v1/messages/count_tokens。
type ProviderCounter struct {
    provider model.Provider
}

// CachedCounter 缓存已计算的 token 数，避免重复计算。
// 包装任意 Counter 实现。
type CachedCounter struct {
    inner Counter
    cache map[string]int64 // key = message ID
}
```

---

## 模块 11：可观测性

### 设计

Event 系统天然适合 tracing——每个 OutputEvent 可以关联到当前 span。
可观测性通过 Hook 系统集成，不侵入核心代码。

```go
// OTelHook 通过 Hook 系统集成 OpenTelemetry。
// 注册为全局 Hook，自动为关键生命周期事件创建 span。
func RegisterOTelHooks(registry *hook.Registry, tracer trace.Tracer) {
    // Agent 生命周期
    hook.Observe(registry, func(ctx context.Context, e *hook.HookAgentStart) error {
        _, span := tracer.Start(ctx, "agent.turn",
            trace.WithAttributes(
                attribute.String("agent.name", e.AgentName),
                attribute.Int("agent.turn", e.Turn),
            ))
        // span 通过 context 传播，在 HookAgentEnd 中结束
        return nil
    })

    // Model 调用
    hook.Observe(registry, func(ctx context.Context, e *hook.HookAfterModelResponse) error {
        span := trace.SpanFromContext(ctx)
        span.SetAttributes(
            attribute.Int64("gen_ai.usage.input_tokens", e.Usage.InputTokens),
            attribute.Int64("gen_ai.usage.output_tokens", e.Usage.OutputTokens),
        )
        return nil
    })

    // Tool 执行
    hook.Observe(registry, func(ctx context.Context, e *hook.HookPreToolUse) error {
        _, _ = tracer.Start(ctx, "tool."+e.ToolName)
        return nil
    })
}
```

---

## 模块 12：graph 包定位

### 现状

`graph/` 是完全独立的 DAG 执行器，有自己的 `State`、`Handler`、`Middleware` 类型，与 `blades.Agent` 接口不兼容。

### 定位决策

`graph/` 保持独立子系统，不强制统一到 Event 系统。原因：

1. DAG 执行器的语义（编译时验证、检查点恢复、条件边）与 Agent Loop 的语义（LLM 对话、工具调用、压缩）本质不同
2. 强制统一会增加不必要的复杂度，且破坏 graph 包的独立可用性
3. 当前 `flow/deep.go` 已经通过 `DeepAgent` 桥接了 graph 和 Agent

### 桥接方式

通过 `flow/` 包提供桥接，而非在 graph 包内部集成 Event：

```go
// flow/graph.go — 将 graph.Executor 包装为 blades.Agent
func GraphAgent(name string, executor *graph.Executor, opts ...GraphAgentOption) Agent

// GraphAgent 内部：
// 1. 从 InputEvent 中提取初始 State
// 2. 调用 executor.Execute(ctx, state)
// 3. 将结果转换为 OutputEvent 序列
// 4. graph.Handler 中如果需要调用 LLM，通过注入的 model.Provider 实现
```

---

## 模块 13：迁移路径

### 从现有代码迁移

虽然设计目标是"不考虑向后兼容"，但现有代码量不小（flow/、graph/、contrib/、skills/），需要明确迁移路径。

### 13.1 核心接口迁移

| 现有 | 新 | 迁移方式 |
|------|---|---------|
| `Agent.Run(ctx, *Invocation) Generator[*Message, error]` | `Agent.Run(ctx, <-chan InputEvent) (<-chan OutputEvent, error)` | 重写签名，内部逻辑迁移到 Agent Loop 状态机 |
| `*Invocation` | 去掉 | Session 通过 context 传递，配置在 NewAgent 时确定 |
| `Generator[*Message, error]` | `<-chan OutputEvent` | 消费端从 `for m, err := range gen` 改为 `for event := range output` |
| `Middleware func(Handler) Handler` | `InputMiddleware` / `OutputMiddleware` | 按方向拆分，重写签名 |

### 13.2 各包迁移

**flow/ 包**：5 种组合 Agent 需要适配新的 `<-chan InputEvent` / `<-chan OutputEvent` 签名。
- `SequentialAgent`：内部 channel 串联
- `ParallelAgent`：fan-out/fan-in OutputEvent channel
- `LoopAgent`：内循环消费 OutputEvent，检查 TurnEndEvent 而非 `ActionLoopExit`
- `RoutingAgent`：从 OutputEvent 中提取 handoff 信号
- `DeepAgent`：保持不变（已通过 graph 桥接）

**contrib/ 包**：实现 `model.Provider` 接口，各自内部处理格式转换。
- `contrib/anthropic`：将现有 `applyEphemeralCache` 和 tool message 拆分逻辑保留在包内部
- `contrib/openai`：将 function_call 格式转换保留在包内部
- `contrib/otel`：从 Middleware 迁移到 Hook 系统集成

**skills/ 包**：接口基本不变，`Toolset.ComposeTools` 需要适配新的 `tools.Tool` 接口（精简版）。

**graph/ 包**：保持独立，通过 `flow/graph.go` 桥接。

---

## 模块 14：交互模式系统

### 背景与动机

模块 6 定义了权限决策链的基础架构，但交互模式（Plan Mode、Auto Mode、Accept Edits Mode）需要超越权限判断的完整生命周期管理：模式转换状态机、system prompt 注入、工具列表过滤、AI 分类器集成等。本模块设计这些能力。

### 现状对比

| 维度 | 当前 Blades | 新设计 |
|------|------------|--------|
| 模式管理 | 无 | ModeManager 状态机 + 转换规则 |
| Plan Mode | 权限标志 | 完整生命周期：进入/退出工具 + prompt 注入 + 工具过滤 |
| Auto Mode | 无 | 可插拔 Classifier 接口 + AutoModeController |
| Accept Edits | 无 | AcceptEditsChecker 工作目录边界检查 |
| 安全检查 | 无 | SafetyChecker bypass-immune 检查 |
| 工具过滤 | 无 | FilterToolsForMode（plan 模式隐藏写入工具） |

### 14.1 模式转换状态机

```go
package permission

// ModeState 持有当前模式和暂存状态。
// Plan 模式进入时暂存当前模式，退出时恢复。
type ModeState struct {
    Current     Mode `json:"current"`
    PrePlanMode Mode `json:"prePlanMode,omitempty"`
}

// ModeTransition 表示一次模式变更请求。
type ModeTransition struct {
    From   Mode
    To     Mode
    Source TransitionSource
}

// TransitionSource 标识模式变更的发起方。
type TransitionSource string
const (
    TransitionUser   TransitionSource = "user"   // 用户命令或 CLI 参数
    TransitionTool   TransitionSource = "tool"   // EnterPlanModeTool / ExitPlanModeTool
    TransitionSystem TransitionSource = "system" // 熔断器降级、安全回退
)

// ModeManager 管理模式状态和转换验证。
// 模式切换语义：切换立即生效于 ModeState，但对 Agent Loop 的影响
// 在下一轮 ContextBuilder.Build() 时体现（工具过滤、prompt 注入）。
// 当前轮次已在执行的工具调用不受影响，权限链在每次工具执行时
// 读取 ModeManager.Current()，因此当前轮次的后续工具调用会立即受影响。
type ModeManager struct {
    mu       sync.RWMutex
    state    ModeState
    hooks    *hook.Registry
    onChange []func(from, to Mode)
}

func NewModeManager(initial Mode) *ModeManager

func (m *ModeManager) Current() Mode
func (m *ModeManager) State() ModeState
func (m *ModeManager) Transition(t ModeTransition) error
func (m *ModeManager) OnChange(fn func(from, to Mode))
```

#### 转换规则

```go
// validTransitions 定义合法的模式转换。
// Key: (from, source) → 允许的目标模式列表。
// 不在此表中的转换被拒绝。
var validTransitions = map[Mode]map[TransitionSource][]Mode{
    ModeDefault: {
        TransitionUser:   {ModeAcceptEdits, ModePlan, ModeAuto},
        TransitionTool:   {ModePlan},       // EnterPlanModeTool
        TransitionSystem: {},               // 无系统触发的转换
    },
    ModeAcceptEdits: {
        TransitionUser:   {ModeDefault, ModePlan, ModeAuto},
        TransitionTool:   {ModePlan},
        TransitionSystem: {},
    },
    ModePlan: {
        TransitionUser: {ModeDefault, ModeAcceptEdits, ModeAuto},
        TransitionTool: {ModeDefault, ModeAcceptEdits, ModeAuto}, // ExitPlanModeTool 恢复暂存模式
    },
    ModeAuto: {
        TransitionUser:   {ModeDefault, ModeAcceptEdits, ModePlan},
        TransitionTool:   {ModePlan},
        TransitionSystem: {ModeDefault}, // 熔断器：连续拒绝 → 降级到 default
    },
}
```

#### Plan 模式暂存/恢复

```go
// enterPlan 暂存当前模式并切换到 plan。
func (m *ModeManager) enterPlan() error {
    m.mu.Lock()
    defer m.mu.Unlock()
    if m.state.Current == ModePlan {
        return nil
    }
    m.state.PrePlanMode = m.state.Current
    old := m.state.Current
    m.state.Current = ModePlan
    m.notifyChange(old, ModePlan)
    return nil
}

// exitPlan 恢复暂存的模式。PrePlanMode 为空时回退到 ModeDefault。
func (m *ModeManager) exitPlan() (Mode, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    if m.state.Current != ModePlan {
        return m.state.Current, fmt.Errorf("not in plan mode, current: %s", m.state.Current)
    }
    restore := m.state.PrePlanMode
    if restore == "" {
        restore = ModeDefault
    }
    m.state.PrePlanMode = ""
    old := m.state.Current
    m.state.Current = restore
    m.notifyChange(old, restore)
    return restore, nil
}
```

### 14.2 Plan Mode 完整生命周期

Plan Mode 不仅是权限标志，而是一个完整的工作流：模型通过工具进入 plan 模式 → 使用只读工具探索代码 → 写计划文件 → 用户审批 → 退出 plan 模式并恢复之前的模式。

#### EnterPlanModeTool

```go
package planmode

// EnterPlanModeTool 切换 Agent 到 plan 模式。
// 模型在判断任务需要先探索和规划时调用此工具。
type EnterPlanModeTool struct {
    modeManager *permission.ModeManager
    planDir     string // 计划文件目录，如 ~/.blades/plans/
}

func NewEnterPlanModeTool(mm *permission.ModeManager, planDir string) tools.Tool

func (t *EnterPlanModeTool) Handle(ctx context.Context, input string) (string, error) {
    if err := t.modeManager.Transition(permission.ModeTransition{
        From:   t.modeManager.Current(),
        To:     permission.ModePlan,
        Source: permission.TransitionTool,
    }); err != nil {
        return "", err
    }
    planPath := t.getPlanFilePath(ctx)
    return fmt.Sprintf("已进入 plan 模式。请使用只读工具探索代码，将计划写入 %s，"+
        "完成后调用 ExitPlanMode 提交审批。", planPath), nil
}

func (t *EnterPlanModeTool) IsReadOnly() bool { return true }
```

#### ExitPlanModeTool

```go
// ExitPlanModeInput 是 ExitPlanModeTool 的输入。
type ExitPlanModeInput struct {
    // AllowedPrompts 是计划需要的预批准动作描述。
    // 用户审批计划时同时审批这些动作，批准后转为 session 级 allow 规则。
    AllowedPrompts []AllowedPrompt `json:"allowedPrompts,omitempty"`
}

type AllowedPrompt struct {
    Tool   string `json:"tool"`   // 工具名，如 "Bash"
    Prompt string `json:"prompt"` // 语义描述，如 "run tests"
}

// ExitPlanModeTool 读取计划文件，提交用户审批，退出 plan 模式。
type ExitPlanModeTool struct {
    modeManager *permission.ModeManager
    planDir     string
    promptUser  permission.UserPromptFunc
}

func NewExitPlanModeTool(mm *permission.ModeManager, planDir string, promptUser permission.UserPromptFunc) tools.Tool

func (t *ExitPlanModeTool) Handle(ctx context.Context, input string) (string, error) {
    if t.modeManager.Current() != permission.ModePlan {
        return "", fmt.Errorf("not in plan mode")
    }

    var params ExitPlanModeInput
    if err := json.Unmarshal([]byte(input), &params); err != nil {
        return "", fmt.Errorf("invalid input: %w", err)
    }

    planPath := t.getPlanFilePath(ctx)
    plan, err := os.ReadFile(planPath)
    if err != nil {
        return "", fmt.Errorf("计划文件不存在: %s，请先写入计划再调用 ExitPlanMode", planPath)
    }

    approved, err := t.promptUser(ctx, fmt.Sprintf("退出 plan 模式？\n\n计划:\n%s", string(plan)))
    if err != nil {
        return "", err
    }
    if !approved {
        return "计划被拒绝。请继续在 plan 模式中完善计划。", nil
    }

    restoredMode, err := t.modeManager.exitPlan()
    if err != nil {
        return "", err
    }

    // AllowedPrompts 转为 session 级 allow 规则
    for _, ap := range params.AllowedPrompts {
        t.addSessionRule(ctx, PermissionRule{
            Source:   SourceSession,
            Behavior: PermissionAllow,
            ToolName: ap.Tool,
            Pattern:  ap.Prompt, // 语义描述作为 pattern，由规则匹配器解释
        })
    }

    return fmt.Sprintf("计划已批准。模式恢复为 %s。开始实现。", restoredMode), nil
}

func (t *ExitPlanModeTool) IsReadOnly() bool { return false }
```

#### PlanModePromptSection

```go
// PlanModePromptSection 是 prompt.Builder 的动态 section。
// Plan 模式激活时注入只读指令和计划文件路径。
type PlanModePromptSection struct {
    modeManager *permission.ModeManager
    planDir     string
}

func (s *PlanModePromptSection) Name() string     { return "plan_mode" }
func (s *PlanModePromptSection) Priority() int     { return 50 }

func (s *PlanModePromptSection) Build(ctx context.Context) (string, error) {
    if s.modeManager.Current() != permission.ModePlan {
        return "", nil
    }
    planPath := filepath.Join(s.planDir, session.IDFromContext(ctx)+".md")
    return fmt.Sprintf(`=== PLAN MODE ===
当前处于只读计划模式。禁止执行任何写入操作。

工作流程：
1. 使用只读工具探索代码，理解现有模式
2. 设计实现方案，考虑多种方案的权衡
3. 将计划写入: %s
4. 调用 ExitPlanMode 提交审批
`, planPath), nil
}
```

#### FilterToolsForMode

```go
// FilterToolsForMode 根据当前模式过滤工具列表。
// Plan 模式下只保留只读工具 + plan 工具 + PlanModeTool 声明的工具。
// 其他模式返回完整工具列表。
func FilterToolsForMode(allTools []tools.Tool, mode permission.Mode) []tools.Tool {
    if mode != permission.ModePlan {
        return allTools
    }
    filtered := make([]tools.Tool, 0, len(allTools))
    for _, t := range allTools {
        if t.Name() == "EnterPlanMode" || t.Name() == "ExitPlanMode" || t.Name() == "WritePlan" {
            filtered = append(filtered, t)
            continue
        }
        if rt, ok := t.(tools.ReadOnlyTool); ok && rt.IsReadOnly() {
            filtered = append(filtered, t)
            continue
        }
        if pt, ok := t.(PlanModeTool); ok && pt.AvailableInPlanMode() {
            filtered = append(filtered, t)
            continue
        }
    }
    return filtered
}

// PlanModeTool 是可选接口，声明非只读工具在 plan 模式下仍可用。
// 例如 AskUserQuestion 需要用户交互但不修改状态。
type PlanModeTool interface {
    AvailableInPlanMode() bool
}

// WritePlanTool 是 plan 模式专用的写入工具。
// 只能写入 planDir 下的计划文件，不能写入其他路径。
// 这解决了 plan 模式过滤掉所有写入工具后模型无法写计划文件的矛盾。
type WritePlanTool struct {
    planDir string
}

func NewWritePlanTool(planDir string) tools.Tool

func (t *WritePlanTool) Handle(ctx context.Context, input string) (string, error) {
    var params struct {
        Content string `json:"content"`
    }
    if err := json.Unmarshal([]byte(input), &params); err != nil {
        return "", err
    }
    planPath := filepath.Join(t.planDir, session.IDFromContext(ctx)+".md")
    if err := os.WriteFile(planPath, []byte(params.Content), 0644); err != nil {
        return "", err
    }
    return fmt.Sprintf("计划已写入 %s", planPath), nil
}

func (t *WritePlanTool) IsReadOnly() bool { return false }
```

### 14.3 Auto Mode（纯接口设计）

Auto Mode 使用可插拔 AI 分类器自主决策工具调用的权限。框架只定义接口和控制器，不提供内置分类器实现。

#### Classifier 接口

```go
package permission

// Classifier 评估工具调用是否应被批准或拒绝，无需用户交互。
// 实现可以是 LLM 调用、规则引擎、本地模型等。
type Classifier interface {
    Classify(ctx context.Context, req *ClassifyRequest) (*ClassifyResult, error)
}

// ClassifyRequest 包含分类器决策所需的全部上下文。
type ClassifyRequest struct {
    ToolName     string           `json:"toolName"`
    ToolInput    string           `json:"toolInput"`
    Messages     []*model.Message `json:"messages"`
    SystemPrompt string           `json:"systemPrompt"`
    Rules        ClassifierRules  `json:"rules"`
}

// ClassifierRules 是用户可配置的规则，注入分类器的决策上下文。
type ClassifierRules struct {
    Allow       []string `json:"allow"`       // 应批准的动作描述
    SoftDeny    []string `json:"softDeny"`    // 应拒绝的动作描述
    Environment []string `json:"environment"` // 环境上下文提示
}

// ClassifyResult 是分类器的决策结果。
type ClassifyResult struct {
    ShouldBlock bool              `json:"shouldBlock"`
    Reason      string            `json:"reason"`
    Thinking    string            `json:"thinking,omitempty"`
    Unavailable bool              `json:"unavailable,omitempty"` // 分类器不可用（API 错误等）
    Usage       *model.TokenUsage `json:"usage,omitempty"`
}
```

#### AutoModeController

```go
// AutoModeController 管理 auto 模式的决策流程。
// 包含 acceptEdits 快速路径、熔断器和分类器调用。
type AutoModeController struct {
    classifier    Classifier
    modeManager   *ModeManager
    denialTracker *DenialTracker
    acceptEdits   *AcceptEditsChecker // 快速路径：acceptEdits 安全的工具跳过分类器
    rules         ClassifierRules     // 用户配置的分类器规则
}

func NewAutoModeController(classifier Classifier, mm *ModeManager, opts ...AutoModeOption) *AutoModeController

// Evaluate 运行 auto 模式决策管线。
// 返回 PermissionAllow/PermissionDeny/PermissionAsk（fallback 到用户提示）。
func (c *AutoModeController) Evaluate(
    ctx context.Context, toolName, input string, messages []*model.Message, systemPrompt string,
) (PermissionDecision, error) {
    // 1. 快速路径：acceptEdits 安全的工具直接批准，跳过分类器
    if c.acceptEdits != nil {
        if allowed, _ := c.acceptEdits.Check(ctx, toolName, input); allowed {
            return PermissionAllow, nil
        }
    }

    // 2. 熔断器检查：连续拒绝过多 → 降级到用户提示
    if c.denialTracker.ShouldFallback() {
        // 触发系统降级：auto → default
        _ = c.modeManager.Transition(ModeTransition{
            From: ModeAuto, To: ModeDefault, Source: TransitionSystem,
        })
        return PermissionAsk, nil
    }

    // 3. 运行分类器
    result, err := c.classifier.Classify(ctx, &ClassifyRequest{
        ToolName:     toolName,
        ToolInput:    input,
        Messages:     messages,
        SystemPrompt: systemPrompt,
        Rules:        c.rules,
    })
    if err != nil || result.Unavailable {
        return PermissionAsk, nil // 分类器不可用 → 提示用户，不是拒绝
    }

    // 4. 更新熔断器状态
    if result.ShouldBlock {
        c.denialTracker.RecordDenial()
        return PermissionDeny, nil
    }
    c.denialTracker.RecordSuccess()
    return PermissionAllow, nil
}
```

#### DenialTracker 熔断器

```go
// DenialTracker 跟踪连续和总拒绝次数。
// 超过阈值时触发降级，防止 Agent 陷入拒绝循环。
type DenialTracker struct {
    mu                 sync.Mutex
    consecutiveDenials int
    totalDenials       int
    maxConsecutive     int // 默认 3
    maxTotal           int // 默认 20
}

func NewDenialTracker(maxConsecutive, maxTotal int) *DenialTracker

func (d *DenialTracker) RecordDenial() {
    d.mu.Lock()
    defer d.mu.Unlock()
    d.consecutiveDenials++
    d.totalDenials++
}

func (d *DenialTracker) RecordSuccess() {
    d.mu.Lock()
    defer d.mu.Unlock()
    d.consecutiveDenials = 0
}

func (d *DenialTracker) ShouldFallback() bool {
    d.mu.Lock()
    defer d.mu.Unlock()
    return d.consecutiveDenials >= d.maxConsecutive || d.totalDenials >= d.maxTotal
}

// Reset 清除所有计数。用户手动批准操作后调用，表示继续使用 auto 模式。
func (d *DenialTracker) Reset() {
    d.mu.Lock()
    defer d.mu.Unlock()
    d.consecutiveDenials = 0
    d.totalDenials = 0
}
```

### 14.4 Accept Edits Mode

Accept Edits 模式自动批准工作目录内的文件写入工具，其他工具（Bash、MCP 等）仍需确认。

```go
// AcceptEditsChecker 检查工具调用是否在 acceptEdits 模式下可自动批准。
type AcceptEditsChecker struct {
    workingDir         string
    additionalDirs     []string
    fileWriteToolNames map[string]bool // 可自动批准的工具名，如 {"FileWrite": true, "FileEdit": true}
}

func NewAcceptEditsChecker(workingDir string, fileWriteTools []string, opts ...AcceptEditsOption) *AcceptEditsChecker

// Check 返回 true 表示该工具调用可在 acceptEdits 模式下自动批准。
func (c *AcceptEditsChecker) Check(ctx context.Context, toolName, input string) (bool, error) {
    if !c.fileWriteToolNames[toolName] {
        return false, nil
    }
    filePath, err := extractFilePath(toolName, input)
    if err != nil {
        return false, nil // 无法确定路径，不自动批准
    }
    absPath, err := filepath.Abs(filePath)
    if err != nil {
        return false, nil
    }
    // 解析符号链接，防止通过 symlink 逃逸工作目录
    // 例如：workdir/link -> /etc/passwd，filepath.Rel 会认为在目录内
    realPath, err := filepath.EvalSymlinks(absPath)
    if err != nil {
        return false, nil // 无法解析，不自动批准
    }
    return c.isInAllowedDirectory(realPath), nil
}

// isInAllowedDirectory 检查路径是否在允许的目录内。
// 使用 filepath.Rel 防止路径遍历攻击（../../../etc/passwd）。
func (c *AcceptEditsChecker) isInAllowedDirectory(absPath string) bool {
    dirs := append([]string{c.workingDir}, c.additionalDirs...)
    for _, dir := range dirs {
        rel, err := filepath.Rel(dir, absPath)
        if err != nil {
            continue
        }
        if !strings.HasPrefix(rel, "..") {
            return true
        }
    }
    return false
}

type AcceptEditsOption func(*AcceptEditsChecker)

func WithAdditionalDirectories(dirs ...string) AcceptEditsOption {
    return func(c *AcceptEditsChecker) {
        c.additionalDirs = append(c.additionalDirs, dirs...)
    }
}
```

### 14.5 安全不可绕过检查（Safety Invariants）

SafetyChecker 在权限决策链最前面执行，任何模式都无法绕过。

```go
// SafetyChecker 执行 bypass-immune 安全检查。
// 在权限链第 1 层运行，任何模式（包括未来的 bypass_permissions）都无法绕过。
type SafetyChecker interface {
    Check(ctx context.Context, toolName, input string) *SafetyViolation
}

// SafetyViolation 描述安全违规。
type SafetyViolation struct {
    Reason   string        `json:"reason"`
    Severity SafetySeverity `json:"severity"`
}

// SafetySeverity 区分安全违规的严重程度。
type SafetySeverity int
const (
    // SeverityBlock 绝对禁止，任何机制都无法覆盖。
    // 用于路径遍历、跨机器攻击等。
    SeverityBlock SafetySeverity = iota

    // SeverityConfirm 需要用户确认，但不绝对禁止。
    // 用于敏感文件写入（.git/hooks、.blades/settings.json 等），
    // 用户可能有合理理由需要写入这些文件。
    // 在 auto 模式下，分类器可以覆盖此级别。
    SeverityConfirm
)

// DefaultSafetyChecker 实现常见安全检查。
type DefaultSafetyChecker struct {
    workingDir     string
    sensitiveGlobs []string // 敏感文件模式，如 ".git/**", ".blades/**", "~/.ssh/**"
}

func NewDefaultSafetyChecker(workingDir string) *DefaultSafetyChecker

func (c *DefaultSafetyChecker) Check(ctx context.Context, toolName, input string) *SafetyViolation {
    // 检查 1：路径遍历检测（SeverityBlock）
    // 拒绝通过符号链接解析或编码路径组件逃逸工作目录的输入。
    if c.isPathTraversal(toolName, input) {
        return &SafetyViolation{Reason: "path traversal attempt detected", Severity: SeverityBlock}
    }

    // 检查 2：敏感文件保护（SeverityConfirm）
    // 标记对 .git/、.blades/、shell 配置等敏感路径的写入。
    // 使用 SeverityConfirm 而非 SeverityBlock，因为用户可能有合理理由
    // 写入这些文件（如配置 git hooks、更新 .blades/settings.json）。
    if c.isSensitiveFilePath(toolName, input) {
        return &SafetyViolation{Reason: "write to sensitive file path", Severity: SeverityConfirm}
    }

    return nil
}
```

### 14.6 模式与 Agent Loop 集成

交互模式系统通过以下方式与 Agent Loop（模块 1）集成：

**ContextBuilder 集成：**

```go
// ContextBuilder.Build 在构建 model.Request 时调用 FilterToolsForMode，
// 根据当前模式过滤发送给模型的工具列表。
func (b *ContextBuilder) Build(ctx context.Context, sess session.Session, allTools []tools.Tool) (*model.Request, error) {
    mode := b.modeManager.Current()
    tools := FilterToolsForMode(allTools, mode)
    // ... 其余构建逻辑
}
```

**prompt.Builder 集成：**

```go
// Agent 构造时注册 PlanModePromptSection 为动态 section。
// Plan 模式激活时自动注入只读指令。
func NewAgent(opts ...AgentOption) Agent {
    // ...
    a.promptBuilder.AddDynamic(planmode.NewPlanModePromptSection(a.modeManager, a.planDir))
    // ...
}
```

**AgentOption 注入：**

```go
func WithModeManager(mm *permission.ModeManager) AgentOption
func WithClassifier(c permission.Classifier) AgentOption
func WithAcceptEditsChecker(c *permission.AcceptEditsChecker) AgentOption
func WithSafetyChecker(c permission.SafetyChecker) AgentOption
```

**ModeManager.OnChange 事件：**

```go
// 模式变更时发射 Hook 事件，供可观测性和 UI 使用。
modeManager.OnChange(func(from, to permission.Mode) {
    hookRegistry.Emit(ctx, &hook.HookModeChange{From: from, To: to})
})
```

**数据流：**

```
用户切换模式（CLI/命令）
  │
  ├─→ ModeManager.Transition()
  │     ├─→ 验证转换规则
  │     ├─→ 更新 ModeState
  │     └─→ 触发 OnChange → HookModeChange
  │
  ├─→ 下一轮 ContextBuilder.Build()
  │     ├─→ FilterToolsForMode（plan 模式过滤写入工具）
  │     └─→ PlanModePromptSection（plan 模式注入指令）
  │
  └─→ 工具执行时 PermissionChain.Check()
        ├─→ 第 3 层：模式决策
        └─→ 第 7 层：后处理（auto → Classifier）
```

### 14.7 关键设计决策

1. **4 种核心模式而非 8 种** — 聚焦 default/accept_edits/plan/auto 四种最常用模式。bypass_permissions、deny_all、dont_ask、escalate 预留枚举值，后续按需实现。避免过早设计不确定的模式语义，同时保持扩展性。

2. **纯接口 Classifier** — 框架不内置 LLM 分类器实现，只提供 `Classifier` 接口和 `AutoModeController`（快速路径 + 熔断器）。原因：分类器质量高度依赖具体模型和 prompt 工程，框架不应承担这个责任。用户可以注入基于 Claude/GPT 的 LLM 分类器、基于规则的分类器、或本地小模型分类器。

3. **Plan 模式过滤工具列表 + WritePlanTool** — Plan 模式下从发送给模型的工具列表中移除写入工具，但保留专用的 `WritePlanTool`（只能写入 planDir）。这解决了「过滤写入工具」和「模型需要写计划文件」之间的矛盾。权限链仍作为双重保障。

4. **安全检查双级别** — SafetyViolation 区分 `SeverityBlock`（绝对禁止，如路径遍历）和 `SeverityConfirm`（需确认，如敏感文件写入）。敏感文件不是绝对禁止——用户可能需要配置 git hooks 或更新 .blades/settings.json。SeverityConfirm 跳过模式决策直接进入后处理层（auto 模式下分类器可覆盖）。

5. **模式转换状态机** — 显式转换规则表防止非法转换。例如 auto 模式只能通过 system 来源降级到 default（熔断器触发），不能通过 tool 来源直接跳到其他模式。Plan 模式的暂存/恢复机制确保退出 plan 后恢复用户之前的模式选择。

6. **模式切换时序** — 模式切换立即生效于 ModeState。权限链在每次工具执行时读取当前模式，因此当前轮次的后续工具调用立即受影响。工具列表过滤和 prompt 注入在下一轮 ContextBuilder.Build() 时体现。

7. **AllowedPrompts 闭环** — ExitPlanModeTool 的 AllowedPrompts 在用户审批后直接转为 session 级 PermissionRule，由 PermissionChain 的规则匹配层处理。语义描述作为 pattern 字段，规则匹配器负责解释。

---

## 实现计划

### 阶段 1：Event 系统 + Agent Loop（基础）

- [ ] 定义 `InputEvent` / `OutputEvent` 接口和所有 Event 类型
- [ ] 实现 `TurnState` 不可变状态管理
- [ ] 实现 Agent Loop 双循环状态机
- [ ] 实现 `InputMiddleware` / `OutputMiddleware`
- [ ] 迁移现有测试到新 Event 接口

### 阶段 2：Session 持久化（Agent Loop 依赖）

- [ ] 定义 `session.Entry` 联合类型
- [ ] 实现 `session.Tree`（分支/导航/路径）
- [ ] 实现 `session.fileStore`（JSONL 读写）
- [ ] 实现会话恢复流程
- [ ] 实现 Compaction 历史保留

### 阶段 3：消息与上下文

- [ ] 实现 `model/` 包（Message, Part 内置类型, Provider, Request, Response, TokenUsage）
- [ ] 实现 ContextBuilder（含 `filterForProvider` 消息过滤）
- [ ] 实现 `streamAndRecord` 私有方法
- [ ] 实现 `token.Counter` 接口和多实现
- [ ] 实现 `compact.Pipeline` 和 5 种内置策略（Summarizer 函数注入）
- [ ] 实现 `prompt.Builder`（静态/动态分段）
- [ ] 集成 Anthropic Provider 的 cache_control

### 阶段 4：工具系统增强

- [ ] 精简 `Tool` 核心接口 + 可选能力接口（ConcurrentTool、ReadOnlyTool 等）
- [ ] 实现 `partitionToolCalls` 自动分区
- [ ] 实现 `StreamingToolExecutor`
- [ ] 实现 `ToolResultBudget`

### 阶段 5：扩展与 Hook

- [ ] 定义 `HookEvent` 判别联合类型（Agent/Model/Tool 核心事件）
- [ ] 实现 `HookRegistry`（观察型 + 拦截型 Handler）
- [ ] 增强 Skill frontmatter（hooks、mcpServers、model）

### 阶段 6：权限与交互模式系统

- [ ] 定义权限类型（Decision、Mode、Rule）
- [ ] 实现 `ModeManager`（ModeState + 转换规则表 + OnChange）
- [ ] 实现 `SafetyChecker` 接口 + `DefaultSafetyChecker`（路径遍历 + 敏感文件）
- [ ] 实现 `AcceptEditsChecker`（工作目录边界 + 路径遍历防护）
- [ ] 实现 `PermissionChain` 7 层决策链
- [ ] 实现 `PermissionMiddleware` 集成
- [ ] 定义 `Classifier` 接口 + `ClassifyRequest/ClassifyResult`
- [ ] 实现 `AutoModeController`（快速路径 + 分类器调用）
- [ ] 实现 `DenialTracker` 熔断器
- [ ] 实现 `EnterPlanModeTool` + `ExitPlanModeTool` + `WritePlanTool`
- [ ] 实现 `PlanModePromptSection`（system prompt 动态注入）
- [ ] 实现 `FilterToolsForMode`（plan 模式工具过滤）
- [ ] 集成 ModeManager 到 Agent Loop（ContextBuilder + prompt.Builder）

### 阶段 7：子 Agent 系统

- [ ] 实现 `ForkAgent`（共享缓存前缀）
- [ ] 实现 `BackgroundAgent`（fire-and-forget + Drain）
- [ ] 实现 `CreateWorktreeAgent`（git worktree 隔离）
- [ ] 实现 `QuerySource` 行为区分
- [ ] 重构现有 `NewAgentTool` 使用 ForkAgent

### 阶段 8：Memory 系统

- [ ] 实现 `memory.Loader`（5 层发现 + @include 解析）
- [ ] 实现 Memory 文件处理管线
- [ ] 实现 `memory.Extractor`（后台 Fork Agent）
- [ ] 实现 `memory.Section`（条件注入 System Prompt）
- [ ] 迁移现有 `memory/` 包到新架构

### 阶段 9：错误处理与可观测性

- [ ] 实现 `retry.Policy` 和 `ErrorClassifier`
- [ ] 实现 OTel Hook 集成
- [ ] 迁移现有 `contrib/otel` 到 Hook 系统

### 阶段 10：迁移与集成

- [ ] 迁移 `flow/` 5 种组合 Agent 到新 Event 接口
- [ ] 实现 `flow/graph.go` 桥接层
- [ ] 迁移 `contrib/` Provider 实现（实现 model.Provider，内部处理格式转换）
- [ ] 迁移 `skills/` 到新 Tool 接口

---

## 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| InputEvent/OutputEvent 接口变更影响所有消费者 | 高 | 根包精简为 Agent + Event 类型，变更面可控 |
| 5 策略压缩管线复杂度 | 中 | 每个策略独立实现和测试，管线按需组合 |
| StreamingToolExecutor 并发安全 | 高 | 充分的并发测试 + race detector |
| JSONL 文件膨胀（append-only） | 中 | 定期 GC 清理废弃分支（后续工作） |
| 自动 Memory 提取质量 | 中 | 节流 + 互斥 + 人工审核机制 |
| Hook 系统交互路径多 | 中 | 类型化事件 + 编译时检查减少运行时错误 |
| Output channel 背压 | 中 | buffer 大小可配置（默认 16），context 取消时 goroutine 清理 |
| 现有代码迁移工作量 | 高 | 阶段 10 专门处理迁移，flow/contrib/skills 逐包迁移 |
| Auto Mode 分类器质量不可控 | 高 | 纯接口设计，框架不承担分类器质量；DenialTracker 熔断器降级到 default |
| Plan Mode 工具过滤遗漏 | 中 | ReadOnlyTool 默认 false（安全默认值），权限链第 3 层双重保障 |
| 模式转换竞态 | 中 | ModeManager 内部 sync.RWMutex 保护 |
| AcceptEdits 路径遍历 | 高 | filepath.Rel 检查 + 符号链接解析 |

---

## Streaming 背压与生命周期

### Channel Buffer 策略

`output := make(chan OutputEvent, 16)` 的 buffer 大小通过 `AgentOption` 可配置：

```go
func WithOutputBuffer(size int) AgentOption
```

默认 16 足以覆盖大多数场景（一次 streaming 的 text delta 通常不会积压超过 16 个）。
如果消费者处理慢导致 channel 满，Agent Loop 的 `output <-` 会阻塞，自然形成背压。

### Context 取消清理

Agent Loop 的 goroutine 必须在 context 取消时正确清理：

```go
func (a *agent) loop(ctx context.Context, input <-chan InputEvent, output chan<- OutputEvent) {
    defer close(output)
    // ... 所有 output <- 操作都需要检查 ctx.Done()：
    select {
    case output <- event:
    case <-ctx.Done():
        return
    }
}
```

### Pause/Resume 实现

`ControlEvent{Action: ActionPause/Resume}` 通过 Agent Loop 内部的 `paused` 标志实现：
- Pause：Agent Loop 停止从 Provider stream 读取，但不断开连接
- Resume：恢复读取
- 如果 Provider stream 有自己的超时，Pause 时间过长可能导致连接断开，此时自动重试

---

## 参考资料

- [Claude Code Agent 参考设计](reference-claude-code-agent.md) — 核心 Agent Loop、多策略压缩、权限系统、Hook 系统、Memory 系统
- [pi-agent Framework 参考设计](reference-pi-agent-framework.md) — 极简核心哲学、双循环模型、扩展系统、convertToLlm 边界
- [Blades 现有代码](https://github.com/go-kratos/blades) — 当前 Agent/Tool/Session/Flow/Graph 实现
- [Go iter.Seq2 规范](https://pkg.go.dev/iter) — Generator 流式原语
- [Anthropic Prompt Caching](https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching) — 缓存感知 System Prompt 设计依据
