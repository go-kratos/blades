---
type: design
title: Event 系统与 Agent Loop 状态机
parent: design-agent-framework.md
date: 2026-05-01
status: draft
modules: [module-1]
---

# Event 系统与 Agent Loop 状态机

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
│    States: Idle → Preparing → Streaming → Acting → StopHooks│
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
| `Session` | `session.Session` | 移到 session/ 包，接口扩展为 7 方法（+Leaf/Branch），压缩移出到 compact.Pipeline |
| `Middleware` | 拆分 | 从 `func(Handler) Handler` 变为 `InputMiddleware` + `OutputMiddleware` |

---

## Agent Loop 状态机

Agent Loop 是 Agent.Run 内部启动的 goroutine。它从 input channel 读取 Event，驱动状态转换，向 output channel 写入 Event。

### 状态定义

```go
type AgentState int
const (
    StateIdle      AgentState = iota // 等待输入
    StatePreparing                    // 构建上下文（压缩、组装 model.Request）
    StateStreaming                    // 模型正在生成
    StateActing                       // 执行工具调用
    StateStopHooks                    // 运行 stop hooks（memory 提取、auto-dream 等）
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
Streaming ──[model stop]───────→ StopHooks      (运行 stop hooks)
Streaming ──[model error]──────→ Done           (yield DoneEvent{Reason: Error})

Acting    ──[tool done, more]──→ Acting         (yield ToolEndEvent, 继续下一个工具)
Acting    ──[all tools done]───→ Preparing      (yield TurnEndEvent{HasText: false}, 下一轮)
Acting    ──[exit signal]──────→ Idle           (yield TurnEndEvent{HasText: true})
Acting    ──[all tools done, model stop]──→ StopHooks (运行 stop hooks)
Acting    ──[max turns]────────→ Done           (yield DoneEvent{Reason: MaxTurns})

StopHooks ──[all hooks done, no continue]─→ Idle       (yield TurnEndEvent{HasText: true})
StopHooks ──[hook requests continue]──────→ Preparing  (注入 follow-up，继续循环)
StopHooks ──[max turns reached]───────────→ Done       (yield DoneEvent{Reason: MaxTurns})

Any       ──[ControlEvent:Abort]→ Done          (yield DoneEvent{Reason: Aborted})
Any       ──[SteerEvent]────────→ (queue)       (排队，当前轮工具完成后下一轮生效)
```

注意：`model stop`（模型正常结束，无工具调用）转换到 `StopHooks` 运行 stop hooks，再根据 hook 结果决定转到 `Idle`（发送 `TurnEndEvent`）或 `Preparing`（继续循环）。
`DoneEvent` 严格表示 Agent 生命周期终止，只在 `max turns`、`abort`、`error` 时发送。

### TurnState（不可变每轮状态）

```go
// TurnState 是每轮的不可变状态快照。
// 每次迭代重建，不原地修改，便于调试和回溯。
type TurnState struct {
    Messages               []*Message
    Turn                   int
    TokenCount             int64
    TokenBudget            int64
    TokenBudgetRemaining   int64            // 跨压缩边界追踪的剩余 token 预算
    AutoCompactStats       AutoCompactStats
    MaxOutputRecovery      int
    MaxOutputRecoveryLimit int              // 默认 3
}

type AutoCompactStats struct {
    CompactionCount int
    LastCompactTurn int
    TotalSaved      int64
}
```

#### max_output_tokens 恢复路径

当模型因 max_output_tokens 截断时，Agent Loop 自动恢复：

1. 首次截断：将 max_output_tokens 从默认值（8K）升级到 64K
2. 重试当前轮次（不消耗 Turn 计数）
3. 最多重试 MaxOutputRecoveryLimit 次（默认 3）
4. 超过限制后正常终止当前轮次

恢复路径在 Streaming 状态内部处理，不产生额外的状态转换。

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

        // 模型正常结束（无工具调用）——进入 StopHooks 状态
        hookResults := a.runStopHooks(ctx, state)
        if hookResults.ContinueLoop {
            // stop hook 请求继续循环（如 memory 提取后需要 follow-up）
            state.Messages = append(state.Messages, hookResults.FollowUpMessages...)
            state.Turn++
            continue
        }
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

5. **StopHooks 作为独立状态** — StopHooks 没有合并到 Idle→Preparing 转换中，而是作为独立状态存在。原因：stop hooks 可能触发后台工作（memory 提取、auto-dream 等），也可能注入 follow-up 消息要求再进行一轮循环迭代。将其建模为显式状态，使这些行为在状态机图中可见、可追踪、可测试，而不是隐藏在转换边的副作用中。

6. **max_output_tokens 恢复路径** — 模型因 max_output_tokens 截断时自动升级输出限制并重试，而非直接终止。恢复在 Streaming 状态内部完成，不引入额外状态转换，保持状态机简洁。重试次数通过 `MaxOutputRecoveryLimit` 限制，防止无限循环。
