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
