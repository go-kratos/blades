---
type: design
title: Event 系统与 Agent Loop 状态机
parent: design-agent-framework.md
date: 2026-05-01
status: draft
modules: [module-1]
---

# Event 系统与 Agent Loop 状态机

## 设计结论

Event 和 Message 保持分层，不合并。

- `event/` 是用户协议层，只描述用户输入和 Agent 输出，不导入 `model/`。
- `model/` 是模型上下文层，只描述 Provider、Session、Compression 需要的 Message/Part，不导入 `event/`。
- Agent Loop 是唯一转换边界：`Event -> Agent Loop conversion -> Provider(Message/Part)`。
- 输入和输出都必须支持多模态。输入使用 `InputPart`，输出使用 `OutputPart`，不能把输出简化为文本。
- 用户直接导入 `event/` 使用 Event 类型。根包 `blades` 不 re-export Event 类型，也不提供 Event 构造函数。

这个分层的核心收益是依赖图稳定：Event 层不会因为 Provider message schema 演进而变化，Provider 层也不会被用户交互事件污染。

## 架构位置

```
┌──────────────────────────────────────────────────────────┐
│  User Layer                                               │
│    blades.Agent                                           │
│    <-chan event.Input -> Agent -> <-chan event.Output      │
│    用户代码直接依赖 event/，根包不提供 Event 别名             │
├──────────────────────────────────────────────────────────┤
│  event/（叶子包，用户协议 DTO）                            │
│    Input, Output                                          │
│    InputPart, OutputPart                                  │
│    Prompt, Steer, TextDelta, PartDelta, ToolEnd             │
├──────────────────────────────────────────────────────────┤
│  Agent Loop（internal/loop，转换与状态机）                  │
│    event.InputPart  -> model.Part                         │
│    model.Response   -> event.Output                       │
│    tools.Result     -> event.OutputPart + model.ToolResultPart
├──────────────────────────────────────────────────────────┤
│  Internal Service Layer                                   │
│    ContextBuilder: Session -> compact -> model.Request     │
│    streamAndRecord: Provider stream -> Event + Session     │
├──────────────────────────────────────────────────────────┤
│  model/（模型协议 DTO + Provider 接口）                    │
│    model.Message, model.Part, model.Request, model.Response│
│    model.Provider.Stream(ctx, *model.Request)              │
├──────────────────────────────────────────────────────────┤
│  contrib/*                                                 │
│    Anthropic / OpenAI / Gemini 实现 model.Provider         │
└──────────────────────────────────────────────────────────┘
```

`event/` 和 `model/` 都是叶子协议包。它们可以有同名概念，例如 Text/File/Data/JSON，但不共享 Go 类型。这样可以避免未来在其中一层添加 provider-only 字段或 UI-only 字段时污染另一层。

## Agent 接口

```go
type Agent interface {
    Name() string
    Description() string
    Run(context.Context, <-chan event.Input) (<-chan event.Output, error)
}
```

`Run` 启动失败返回 error，运行时状态和错误通过 `event.Output` 传递。输入 channel 关闭表示用户不再发送新输入，Agent 在当前可完成工作结束后关闭输出 channel。

稳定运行信息通过 `context.Context` 传递，而不是塞进每个 event：

```go
package scope

type Scope struct {
    RunID       string
    AppID       string
    AgentID     string
    SessionID   string
    UserID      string
    ChannelID   string
    WorkspaceID string
}

func NewContext(ctx context.Context, scope Scope) context.Context
func FromContext(ctx context.Context) (Scope, bool)
```

`Scope` 在一次 `Run` 内保持稳定。Agent Loop 从 context 读取 `SessionID`、`WorkspaceID` 等运行信息；`event/` 不承载 `SessionID`、`UserID`、`ChannelID` 或 `TraceID`。Trace 使用 OpenTelemetry 的 context 传播。禁止把大对象、可变 map、消息历史或工具结果塞进 context。

## Event 类型

### 基础接口

```go
package event

type Input interface{ input() }
type Output interface{ output() }

type InputPart interface{ inputPart() }
type OutputPart interface{ outputPart() }
```

`Input` 和 `Output` 本身就是事件接口联合，channel 中直接传具体事件，不再使用 `Input{Event: ...}` 或 `Output{Event: ...}` 这类二次封装。marker method 使用非导出方法，确保 Event/Part 判别联合由框架控制。后续如需要开放扩展，应显式设计 `CustomPart` 或注册机制，而不是默认开放。

### 多模态输入

```go
type Prompt struct {
    Parts []InputPart `json:"parts"`
}

type Steer struct {
    Parts []InputPart `json:"parts"`
}

type Control struct {
    Action ControlAction `json:"action"`
}

type Notification struct {
    Source   string         `json:"source"`
    Kind     string         `json:"kind"`
    ID       string         `json:"id,omitempty"`
    Status   string         `json:"status,omitempty"`
    Parts    []InputPart    `json:"parts,omitempty"`
    Metadata map[string]any `json:"metadata,omitempty"`
    Usage    *Usage         `json:"usage,omitempty"`
    Err      error          `json:"-"`
}

type ControlAction string

const (
    ActionAbort  ControlAction = "abort"
    ActionPause  ControlAction = "pause"
    ActionResume ControlAction = "resume"
)

func Text(text string) TextInput
func NewPrompt(parts ...InputPart) Prompt
func PromptText(text string) Prompt
func NewSteer(parts ...InputPart) Steer
func SteerText(text string) Steer
```

`Prompt` 开始一轮用户输入。`Steer` 在 Agent 运行中途注入指令；它不会中断正在进行的模型 streaming，而是在当前轮次工具执行完成后、下一轮 `ContextBuilder.Build` 前按 FIFO 顺序转成 user message。

`Notification` 是框架内部的输入通知，供 host、channel、orchestrator、后台 job 或长任务把状态回流到某个 Agent 的 input channel。它仍然属于 `event/` 包，原因是 `Input` 使用非导出 marker method，外部包不能自行定义新的输入事件类型。通知内容只使用 `InputPart`、`Usage` 和普通 metadata，不导入 `model/`。

`Prompt` / `Steer` 的完整表达仍然是 `[]InputPart`，用于文本、文件、二进制数据和结构化 JSON 的组合输入。为了降低最常见文本路径的成本，`event/` 包提供少量构造 helper。helper 只存在于 `event/` 包，根包 `blades` 不提供第二套 Event API。

输入 Part：

```go
type TextInput struct {
    Text string `json:"text"`
}

type FileInput struct {
    URI      string `json:"uri"`
    MimeType string `json:"mimeType,omitempty"`
    Name     string `json:"name,omitempty"`
}

type DataInput struct {
    Data     []byte `json:"data"`
    MimeType string `json:"mimeType"`
    Name     string `json:"name,omitempty"`
}

type JSONInput struct {
    Value any `json:"value"`
}
```

选择 `TextInput` / `TextOutput` 这种方向性命名，而不是共用 `TextPart`，是为了让 Event API 的方向语义更清晰，并避免与 `model.TextPart` 产生包外歧义。`event.Text("hello")` 只是 `TextInput{Text: "hello"}` 的短构造函数，不引入新的 Part 类型。

### 多模态输出

输出 Part：

```go
type PartKind string

const (
    PartText     PartKind = "text"
    PartFile     PartKind = "file"
    PartData     PartKind = "data"
    PartJSON     PartKind = "json"
    PartThinking PartKind = "thinking"
)

type TextOutput struct {
    Text string `json:"text"`
}

type FileOutput struct {
    URI      string `json:"uri"`
    MimeType string `json:"mimeType,omitempty"`
    Name     string `json:"name,omitempty"`
}

type DataOutput struct {
    Data     []byte `json:"data"`
    MimeType string `json:"mimeType"`
    Name     string `json:"name,omitempty"`
}

type JSONOutput struct {
    Value any `json:"value"`
}

type ThinkingOutput struct {
    Text string `json:"text"`
}
```

输出 Event：

```go
type PartStart struct {
    Turn  int      `json:"turn"`
    ID    string   `json:"id"`
    Index int      `json:"index"`
    Kind  PartKind `json:"kind"`
}

type TextDelta struct {
    Turn  int    `json:"turn"`
    ID    string `json:"id"`
    Index int    `json:"index"`
    Text  string `json:"text"`
}

type ThinkingDelta struct {
    Turn  int    `json:"turn"`
    ID    string `json:"id"`
    Index int    `json:"index"`
    Text  string `json:"text"`
}

type PartDelta struct {
    Turn  int        `json:"turn"`
    ID    string     `json:"id"`
    Index int        `json:"index"`
    Delta OutputPart `json:"delta"`
}

type PartEnd struct {
    Turn  int        `json:"turn"`
    ID    string     `json:"id"`
    Index int        `json:"index"`
    Part  OutputPart `json:"part"`
}
```

`TextDelta` 是普通文本流式输出的主路径，`ThinkingDelta` 是 thinking 流式输出的主路径。这样最常见的文本消费只需要 switch 一层，不需要再从 `PartDelta.Delta` 中二次拆 `TextOutput`。

`PartDelta` 保留为高级多模态增量逃生口，用于非文本、非 thinking 的结构化 JSON、文件引用、二进制数据或未来扩展 part。Provider 不支持某些 delta 类型时，Agent Loop 可以只发 `PartEnd`。`PartEnd.Part` 和 `TurnEnd.Parts` 始终承载最终完整多模态结果，Agent Loop 负责把 `TextDelta` / `ThinkingDelta` 累积成 `TextOutput` / `ThinkingOutput`。

### 工具事件

```go
type ToolStart struct {
    Turn   int             `json:"turn"`
    CallID string          `json:"callId"`
    Name   string          `json:"name"`
    Args   json.RawMessage `json:"args"`
}

type ToolDelta struct {
    Turn   int        `json:"turn"`
    CallID string     `json:"callId"`
    Delta  OutputPart `json:"delta"`
}

type ToolEnd struct {
    Turn   int          `json:"turn"`
    CallID string       `json:"callId"`
    Name   string       `json:"name"`
    Result []OutputPart `json:"result,omitempty"`
    Err    error        `json:"-"`
}
```

工具结果也是多模态的。`tools/` 包不导入 `model/`，工具执行完成后由 Agent Loop 同时转换为：

- 给用户看的 `ToolDelta` / `ToolEnd{Result: []event.OutputPart}`
- 给下一轮 Provider 的 `model.ToolResultPart`

### 轮次和生命周期

```go
type Usage struct {
    InputTokens       int64 `json:"inputTokens,omitempty"`
    OutputTokens      int64 `json:"outputTokens,omitempty"`
    TotalTokens       int64 `json:"totalTokens,omitempty"`
    CachedInputTokens int64 `json:"cachedInputTokens,omitempty"`
}

type StopReason string

const (
    StopReasonStop          StopReason = "stop"
    StopReasonToolUse       StopReason = "tool_use"
    StopReasonMaxOutput     StopReason = "max_output"
    StopReasonContentFilter StopReason = "content_filter"
)

type TurnEnd struct {
    Turn       int          `json:"turn"`
    StopReason StopReason   `json:"stopReason"`
    Parts      []OutputPart `json:"parts,omitempty"`
    Usage      *Usage       `json:"usage,omitempty"`
}

type Error struct {
    Err     error         `json:"-"`
    Retry   bool          `json:"retry"`
    RetryIn time.Duration `json:"retryIn,omitempty"`
}

type Done struct {
    Reason TerminalReason `json:"reason"`
    Err    error          `json:"-"`
}

type TerminalReason string

const (
    ReasonInputClosed TerminalReason = "input_closed"
    ReasonMaxTurns    TerminalReason = "max_turns"
    ReasonAborted     TerminalReason = "aborted"
    ReasonError       TerminalReason = "error"
)
```

`Done` 严格表示 Agent 生命周期结束，不承载最终文本。最终内容属于 `PartEnd` / `TurnEnd.Parts`。这避免 `Done.Text` 与流式 part 重复或不一致。

## Event 构造方式

Event 类型可以使用普通 struct literal 构造；`event/` 包也提供少量文本和 variadic helper，服务最常见路径。根包不增加别名或便捷函数。

```go
event.PromptText("hello")

event.NewPrompt(
    event.Text("hello"),
    event.FileInput{URI: "file:///tmp/a.png", MimeType: "image/png"},
)

event.Control{Action: event.ActionAbort}
```

这样做让 Event 协议只有一个入口：类型定义、构造方式和 switch 处理都来自 `event/`。如果后续确实需要更多样板封装，也应优先加在 `event/` 包或调用侧业务 helper，而不是在根包提供第二套 Event API。

## 使用方式

### 单次调用

```go
input := make(chan event.Input, 1)
input <- event.PromptText("hello")
close(input)

output, err := agent.Run(ctx, input)
if err != nil {
    log.Fatal(err)
}

for ev := range output {
    switch e := ev.(type) {
    case event.TextDelta:
        fmt.Print(e.Text)
    case event.Error:
        log.Printf("error: %v", e.Err)
    case event.TurnEnd:
        fmt.Println()
    }
}
```

### Live steering

```go
input := make(chan event.Input, 4)
input <- event.PromptText("分析这段代码")

output, err := agent.Run(ctx, input)
if err != nil {
    log.Fatal(err)
}

for ev := range output {
    switch e := ev.(type) {
    case event.TextDelta:
        fmt.Print(e.Text)
    case event.ToolStart:
        input <- event.SteerText("同时检查测试覆盖率")
    case event.TurnEnd:
        if e.StopReason == event.StopReasonToolUse {
            continue
        }
        close(input)
    }
}
```

### 多模态输出消费

```go
for ev := range output {
    switch e := ev.(type) {
    case event.TextDelta:
        fmt.Print(e.Text)
    case event.ThinkingDelta:
        debugLog(e.Text)
    case event.PartDelta:
        switch p := e.Delta.(type) {
        case event.JSONOutput:
            renderJSON(p.Value)
        }
    case event.PartEnd:
        switch p := e.Part.(type) {
        case event.FileOutput:
            fmt.Printf("file: %s\n", p.URI)
        case event.DataOutput:
            saveBlob(p.Data, p.MimeType)
        }
    }
}
```

## Middleware

```go
type InputMiddleware func(<-chan event.Input) <-chan event.Input
type OutputMiddleware func(<-chan event.Output) <-chan event.Output
```

Middleware 只操作 Event 协议，不操作 `model.Message`。需要基于模型上下文做决策的逻辑应放在 Agent Loop 能力层中，例如 Hook、Policy、ContextBuilder 或 ToolOrchestrator。

```go
func LogOutput(in <-chan event.Output) <-chan event.Output {
    out := make(chan event.Output)
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

## Internal Service Layer

### ContextBuilder

```go
type ContextBuilder struct {
    compression *compact.Pipeline
    prompt      *blades.PromptBuilder
}

func (b *ContextBuilder) Build(
    ctx context.Context,
    session session.Session,
    input []event.InputPart,
    steer [][]event.InputPart,
    tools []tools.Tool,
) (*model.Request, error)
```

职责：

- 将 `event.InputPart` 转为 user `model.Message`。
- 将 session 历史、system prompt、压缩结果和工具声明组装成 `model.Request`。
- 将 `tools.Tool` 的 schema 转成 `model.ToolSpec`，避免 `model/` 依赖 `tools/`。
- 执行 provider 能力过滤，例如 thinking 支持、文件支持、cache breakpoint 支持。

不单独暴露 `MessageConverter` 接口。转换规则和 ContextBuilder 的上下文预算、provider 能力过滤强相关，作为私有实现更容易保持一致。

### streamAndRecord

```go
func (a *agent) streamAndRecord(
    ctx context.Context,
    stream iter.Seq2[*model.Response, error],
    session session.Session,
    output chan<- event.Output,
) (msg *model.Message, toolCalls []ToolCall, err error)
```

职责：

- 将 `model.Response` 增量转为 `PartStart` / `TextDelta` / `ThinkingDelta` / `PartDelta` / `PartEnd` / `ToolStart`。
- 累积完整 assistant `model.Message` 并写入 session。
- 提取完整 tool calls，交给 ToolOrchestrator 执行。
- 将 usage 从 `model.TokenUsage` 转为 `event.Usage`。

Provider 特定格式转换仍在 `contrib/*` 内部完成。Internal Service Layer 只处理 provider-neutral 的 `model.Response`。

## 数据流

```
User                                  Agent Loop
  │                                       │
  │ input <- event.PromptText("...")      │
  │ ───────────────────────────────────→  │
  │                                       ├─→ event.InputPart -> model.Part
  │                                       ├─→ ContextBuilder.Build(session)
  │                                       │     -> *model.Request
  │                                       ├─→ provider.Stream(ctx, request)
  │                                       ├─→ streamAndRecord(...)
  │  output: PartStart                   │
  │ ←───────────────────────────────────  │
  │  output: TextDelta                    │
  │ ←───────────────────────────────────  │
  │  output: ToolStart                    │
  │ ←───────────────────────────────────  │
  │                                       ├─→ tool.Handle(ctx, args)
  │ input <- event.SteerText("检查测试")  │
  │ ───────────────────────────────────→  │  Steer 排队
  │  output: ToolEnd{[]OutputPart}        │
  │ ←───────────────────────────────────  │
  │  output: TurnEnd{tool_use}            │
  │ ←───────────────────────────────────  │
  │                                       │  下一轮注入 Steer
  │  output: TextDelta                    │
  │ ←───────────────────────────────────  │
  │  output: TurnEnd{stop}                │
  │ ←───────────────────────────────────  │
  │ close(input)                          │
  │ ───────────────────────────────────→  │
  │  output: Done{input_closed}           │
  │ ←───────────────────────────────────  │
```

## 与现有代码的关系

| 现有类型 | 新角色 | 说明 |
|---------|--------|------|
| `*Message` | `model.Message` | 只用于 Session、Compression、Provider，不作为用户 I/O |
| `iter.Seq2[*Message, error]` | 被替代 | Agent 返回 `(<-chan event.Output, error)` |
| `*Invocation` | 去掉 | Session 和配置由 Agent 构造时确定或通过 context 注入 |
| `ModelProvider` | `model.Provider` | `Stream(ctx, *Request)` 替代旧 streaming 命名 |
| `Session` | `session.Session` | 存储 `model.Message`，不存储 Event |
| `Middleware` | 拆分 | `InputMiddleware` / `OutputMiddleware` |
| 旧任务通知类型 | `event.Notification` | 作为输入通知回流，不作为 `event.Output` |

## Agent Loop 状态机

```go
type AgentState int

const (
    StateIdle AgentState = iota
    StatePreparing
    StateStreaming
    StateActing
    StateStopHooks
    StateDone
)
```

状态转换：

```
Idle      --[Prompt]-----------> Preparing
Idle      --[Control:Abort]----> Done

Preparing --[request ready]----> Streaming
Preparing --[compact needed]---> Preparing

Streaming --[text delta]-------> Streaming  (yield TextDelta)
Streaming --[part delta]-------> Streaming  (yield PartDelta)
Streaming --[tool call]--------> Acting     (yield ToolStart)
Streaming --[stop]-------------> StopHooks
Streaming --[error]------------> Done       (yield Done{error})

Acting    --[tool delta]-------> Acting     (yield ToolDelta)
Acting    --[tool done]--------> Acting     (yield ToolEnd)
Acting    --[all tools done]---> Preparing  (yield TurnEnd{tool_use})
Acting    --[max turns]--------> Done       (yield Done{max_turns})

StopHooks --[continue]---------> Preparing
StopHooks --[no continue]------> Idle       (yield TurnEnd{stop})
StopHooks --[max turns]--------> Done       (yield Done{max_turns})

Any       --[Steer]------------> queue
Any       --[Control:Abort]----> Done
```

`TurnEnd.StopReason` 取代 `HasText`。有工具调用时使用 `tool_use`，正常停止时使用 `stop`，输出被截断时使用 `max_output`。

### TurnState

```go
type TurnState struct {
    Messages               []*model.Message
    Turn                   int
    TokenCount             int64
    TokenBudget            int64
    TokenBudgetRemaining   int64
    AutoCompactStats       compact.AutoCompactStats
    MaxOutputRecovery      int
    MaxOutputRecoveryLimit int
}
```

每轮创建新的 `TurnState`，压缩策略接收旧状态并返回新状态，不原地修改共享消息切片。

### 双循环伪代码

```go
func (a *agent) Run(ctx context.Context, input <-chan event.Input) (<-chan event.Output, error) {
    if a.provider == nil {
        return nil, ErrProviderRequired
    }
    output := make(chan event.Output, a.outputBuffer)
    go a.loop(ctx, input, output)
    return output, nil
}

func (a *agent) handlePrompt(
    ctx context.Context,
    prompt event.Prompt,
    input <-chan event.Input,
    output chan<- event.Output,
    state *TurnState,
) {
    currentInput := prompt.Parts
    var steerQueue [][]event.InputPart

    for state.Turn < a.maxTurns {
        state = a.rebuildTurnState(state)

        req, err := a.contextBuilder.Build(ctx, a.session, currentInput, steerQueue, a.tools)
        if err != nil {
            output <- event.Done{Reason: event.ReasonError, Err: err}
            return
        }
        currentInput = nil
        steerQueue = steerQueue[:0]

        stream := a.provider.Stream(ctx, req)
        msg, toolCalls, err := a.streamAndRecord(ctx, stream, a.session, output)
        if err != nil {
            output <- event.Done{Reason: event.ReasonError, Err: err}
            return
        }
        _ = msg

        if len(toolCalls) > 0 {
            results := a.tools.Execute(ctx, toolCalls, output)
            a.recordToolResults(ctx, results)
            output <- event.TurnEnd{Turn: state.Turn, StopReason: event.StopReasonToolUse}
            steerQueue = drainSteer(input)
            state.Turn++
            continue
        }

        hooks := a.runStopHooks(ctx, state)
        if hooks.ContinueLoop {
            currentInput = hooks.FollowUpInputParts
            state.Turn++
            continue
        }

        output <- event.TurnEnd{Turn: state.Turn, StopReason: event.StopReasonStop}
        return
    }

    output <- event.Done{Reason: event.ReasonMaxTurns}
}
```

## 关键设计决策

1. **Event 和 Message 分层而非合并**：Event 面向用户交互，Message 面向 Provider 上下文。合并会让 UI 控制事件、Provider tool protocol、session 压缩状态混在一个类型系统里，导致包依赖和语义都变重。

2. **Event 不依赖 model**：Event 层不能出现 `model.TokenUsage`、`model.Part` 或 `model.Message`。需要相似数据时定义独立 DTO，例如 `event.Usage` 和 `event.OutputPart`。

3. **文本一等公民，输出仍多模态**：模型最常见输出是文本，因此用 `TextDelta` 降低消费成本；thinking 用 `ThinkingDelta` 保持同样的单层消费体验。完整输出仍通过 `PartEnd` / `TurnEnd.Parts` 承载 `OutputPart`，非文本高级增量使用 `PartDelta`。

4. **工具结果多模态**：工具返回 `[]OutputPart`，Agent Loop 再转换成 `model.ToolResultPart`。这样工具系统不依赖模型层，同时保留向用户展示丰富结果的能力。

5. **状态机显式化**：状态转换表是实现和测试依据。`StopHooks` 独立建模，因为它可能触发 follow-up，从而继续进入下一轮。

6. **Steer FIFO 且不打断 streaming**：中途输入通过队列进入下一轮，避免在 provider streaming 和工具执行中引入复杂抢占语义。
