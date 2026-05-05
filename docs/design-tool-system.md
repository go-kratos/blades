---
type: design
title: 工具系统
parent: design-agent-framework.md
date: 2026-05-01
status: draft
modules: [module-3]
---

# 工具系统

### 现状对比

| 维度 | 当前 Blades | 新设计 |
|------|------------|--------|
| 并发控制 | 全部并发（errgroup） | 自声明 ConcurrencyMode + 自动分区 |
| 流式执行 | 等模型完成才执行 | StreamingToolExecutor 重叠执行 |
| 生命周期 | 无 Hook | Agent Loop 在工具调用边界触发 `hook.PreToolUse` / `hook.PostToolUse` |
| 结果管理 | 无限制 | Agent Loop/compact 执行工具结果预算、截断和持久化引用 |
| 安全声明 | 无 | IsReadOnly / IsDestructive |

### 3.1 Tool 接口（精简核心 + 可选能力）

核心 `Tool` 接口保持精简（4 个方法），扩展能力通过可选接口（interface assertion）实现。
这是 Go 惯用的可选接口模式（类似 `io.WriterTo`、`io.ReaderFrom`）。

`tools/` 是能力叶子包，不导入 `model/`、`event/`、`hook/` 或 `policy/`。工具 schema、工具 metadata 和工具结果在 Agent Loop 中转换：

- `tools.Tool` / JSON Schema -> `model.ToolSpec`
- `tools.Result` -> `event.ToolEnd{Result: []event.OutputPart}`
- `tools.Result` -> `model.ToolResultPart{Parts: []model.Part}`

```go
// Tool 核心接口，所有工具必须实现。
type Tool interface {
    Name() string
    Description() string
    InputSchema() *jsonschema.Schema
    Handle(ctx context.Context, input json.RawMessage) (*Result, error)
}

type Result struct {
    Parts []ResultPart `json:"parts,omitempty"`
}

type ResultPart interface{ resultPart() }

type TextResult struct { Text string `json:"text"` }
type FileResult struct { URI string `json:"uri"`; MimeType string `json:"mimeType,omitempty"`; Name string `json:"name,omitempty"` }
type DataResult struct { Data []byte `json:"data"`; MimeType string `json:"mimeType"`; Name string `json:"name,omitempty"` }
type JSONResult struct { Value any `json:"value"` }

// --- 可选能力接口（通过 type assertion 检查）---

// ConcurrentTool 声明此工具是否可并发执行。
// 未实现此接口的工具默认 Sequential（安全默认值）。
type ConcurrentTool interface {
    ConcurrencyMode() ConcurrencyMode
}

// ReadOnlyTool 声明此工具是否只读。
// Agent Loop 或应用 bridge 可读取该 metadata，再映射为 policy request。
type ReadOnlyTool interface {
    IsReadOnly() bool
}

// DestructiveTool 声明此工具对给定输入是否有破坏性。
// Agent Loop 或应用 bridge 可读取该 metadata，再映射为 policy request。
type DestructiveTool interface {
    IsDestructive(input json.RawMessage) bool
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

// EnabledTool 声明工具是否在当前上下文中可用。
// 未实现此接口的工具默认始终可用。
type EnabledTool interface {
    IsEnabled(ctx context.Context) bool
}

// ValidatedTool 提供独立于 JSON schema 的语义校验。
// JSON schema 校验结构正确性，ValidateInput 校验业务语义
//（如路径是否存在、参数组合是否合法）。
type ValidatedTool interface {
    ValidateInput(ctx context.Context, input json.RawMessage) error
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
    destructive     func(json.RawMessage) bool
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

#### 工具调用状态机

每个工具调用在执行器内部经历以下状态流转：

```
Queued ──→ Executing ──→ Completed ──→ Yielded
  │            │
  └────────────┴──→ Aborted（被 sibling abort 取消）
```

```go
type ToolCallState int
const (
    ToolCallQueued    ToolCallState = iota // 等待执行槽位
    ToolCallExecuting                       // 正在执行
    ToolCallCompleted                       // 完成（成功或错误）
    ToolCallYielded                         // 结果已交付调用方
    ToolCallAborted                         // 被 sibling abort 取消
)

type streamingToolEntry struct {
    Call   ToolCall
    State  ToolCallState
    Result *Result
    Err    error
    Cancel context.CancelFunc // 每个调用独立的取消函数
}
```

状态流转规则：
- `Queued → Executing`：获得执行槽位且并发模式允许时触发。
- `Executing → Completed`：`Handle()` 返回后触发，无论成功或错误。
- `Completed → Yielded`：结果按原始顺序交付给调用方后触发。
- `Queued → Aborted` / `Executing → Aborted`：兄弟工具触发 `SiblingAbort` 时，所有处于 `Queued` 或 `Executing` 状态的调用被取消。

#### 执行器定义

```go
// StreamingToolExecutor 是 internal/loop 的私有服务，不属于 tools/ 包。
// 它在模型仍在流式输出时就开始执行工具。
// 并发安全的工具在 tool call 参数完整后立即启动，
// 串行工具排队等待。执行与模型生成重叠，降低端到端延迟。
type StreamingToolExecutor struct {
    tools        map[string]Tool
    resultBudget *compact.ToolResultBudget
    maxConc      int // 最大并发数，默认 10
}

// ExecuteStreaming 接收模型流式输出中逐步到达的 tool call。
// 返回按原始顺序排列的结果流。
func (e *StreamingToolExecutor) ExecuteStreaming(
    ctx context.Context,
    toolCalls <-chan ToolCall,
) iter.Seq2[*Result, error]
```

#### Sibling Abort 机制

当关键工具（如 Bash）执行失败时，继续执行同一轮中其余的兄弟工具调用通常没有意义——它们的前置条件可能已不成立。`SiblingAbort` 在这种场景下取消所有处于 `Queued` 或 `Executing` 状态的兄弟调用，并为每个被取消的调用生成合成错误结果，确保调用方仍能收到完整的结果序列。

```go
// SiblingAbort 在关键工具（如 Bash）失败时取消所有排队/执行中的兄弟工具调用。
// 为被取消的工具生成合成错误结果。不中止父 query。
// 实现：创建子 context.CancelFunc，Bash 错误时调用所有兄弟的 Cancel。
func (e *StreamingToolExecutor) SiblingAbort(failedCallID string)
```

`SiblingAbort` 不会中止父级 query 循环——模型仍会收到所有结果（包括合成错误），并自行决定下一步行动。

#### 执行流程

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
    executor func(context.Context, ToolCall) (*Result, error),
) ([]*Result, error)
```

### 3.4 工具生命周期 Hook

工具生命周期 Hook 由 Agent Loop 通过 `hook.Registry` 触发，不定义在 `tools/` 包内，避免 `tools/` 反向依赖 `hook/`、`event/` 或 `model/`。

- 执行前：`hook.PreToolUse` / `hook.PreToolUseHandler`，可阻止执行或修改 `json.RawMessage` 输入。
- 执行后：`hook.PostToolUse` / `hook.PostToolUseHandler`，可修改 `*tools.Result`。
- 执行失败：`hook.PostToolUseFailure`，用于审计、统计和恢复策略。

### 3.5 工具执行完整生命周期

```
1. IsEnabled 检查      ← 若工具实现 EnabledTool 且返回 false，跳过执行并返回不可用提示
2. 参数校验            ← JSON Schema 校验结构正确性
3. ValidateInput       ← 若工具实现 ValidatedTool，执行业务语义校验
4. hook.PreToolUse     ← 可阻止执行或修改参数
5. 权限检查            ← PolicyChain.Check()
6. tool.Handle()       ← 实际执行，支持流式进度
7. hook.PostToolUse    ← 可修改结果
8. Result budgeting   ← 超大结果截断 + 持久化引用
9. 发射事件            ← ToolDelta / ToolEnd
```

### 关键设计决策

1. **默认 Sequential + 可选接口** — 当前 Blades 所有工具默认并发执行，这是不安全的默认值（如两个 bash 命令并发可能冲突）。新设计默认 Sequential，工具通过实现 `ConcurrentTool` 可选接口显式声明并发安全。核心 `Tool` 接口保持 4 个方法，扩展能力通过 type assertion 检查，这是 Go 惯用的可选接口模式（类似 `io.WriterTo`）。

2. **流式工具执行** — 当前必须等模型完整输出后才开始执行工具。新设计在模型流式输出过程中，一旦某个 tool call 的参数完整就立即启动执行（如果是并发安全的），模型生成和工具执行时间重叠，显著降低端到端延迟。

3. **多模态工具结果** — 工具返回 `[]tools.ResultPart`，可以是文本、文件引用、二进制数据或结构化 JSON。Agent Loop 负责把它转换为面向用户的 `event.OutputPart` 和面向 Provider 的 `model.Part`，从而保持 `tools/` 与 `event/`、`model/` 解耦。

4. **工具结果预算** — 当前工具结果无大小限制，大文件读取可能撑爆上下文。新设计在 Agent Loop/compact 边界为工具结果设置大小上限，超出时完整结果持久化到磁盘，向模型发送截断预览 + 磁盘路径引用。

5. **Sibling Abort 使用独立子 context 而非共享 abort controller** — 每个工具调用持有独立的 `context.CancelFunc`（通过 `context.WithCancel` 从父 context 派生）。相比共享的 abort controller 模式，独立子 context 有三个优势：（1）天然与 Go 的 context 传播机制集成，工具内部调用的所有下游操作（网络请求、子进程等）自动响应取消；（2）取消粒度精确到单个调用，不会误伤已完成的工具；（3）无需额外的同步原语——`CancelFunc` 本身是并发安全的，多次调用幂等。共享 abort controller 需要手动管理订阅/取消订阅，且在 Go 中没有标准实现，引入不必要的复杂度。

6. **EnabledTool / ValidatedTool 作为可选接口** — `IsEnabled` 和 `ValidateInput` 没有放入核心 `Tool` 接口，因为大多数工具始终可用且不需要超出 JSON Schema 的额外校验。将它们作为可选接口保持核心接口精简，同时允许需要动态启用/禁用（如根据项目类型隐藏不相关工具）或复杂输入校验（如检查文件路径是否存在）的工具按需实现。`ValidateInput` 在 JSON Schema 校验之后执行，确保输入结构已正确，语义校验只需关注业务逻辑。

---
