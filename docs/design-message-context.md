---
type: design
title: 消息与上下文系统
parent: design-agent-framework.md
date: 2026-05-01
status: draft
modules: [module-2]
---

# 消息与上下文系统

### 现状对比

| 维度 | 当前 Blades | 新设计 |
|------|------------|--------|
| 消息类型 | `Part` 密封接口（4 种类型） | 内置 7 种 Part 类型，暂不开放注册 |
| 消息过滤 | 无（直接发给 Provider） | ContextBuilder 内部 `filterForProvider` 私有方法 |
| 上下文压缩 | 单一 `ContextCompressor` | 6 策略 `compact.Pipeline` |
| System Prompt | 简单字符串 | 缓存感知 `blades.PromptBuilder` |

### 2.1 内置消息类型

Part 保持判别联合风格，所有类型内置在 `model/` 包中。`model.Part` 是 Provider、Session、Compression 使用的模型上下文协议，不是用户 Event API。用户输入输出使用 `event.InputPart` / `event.OutputPart`，由 Agent Loop 内部转换为 `model.Part`。两层不共享 Go 类型。

后续如有第三方扩展需求，再考虑开放注册机制。

```go
type Part interface{ part() }

// 基础类型
type TextPart struct { Text string `json:"text"` }
type FilePart struct { URI string `json:"uri"`; MimeType string `json:"mimeType"` }
type DataPart struct { Data any `json:"data"` }

// 工具调用
type ToolUsePart struct { CallID string `json:"callId"`; Name string `json:"name"`; Args string `json:"args"` }
type ToolResultPart struct { CallID string `json:"callId"`; Parts []Part `json:"parts"`; Err error `json:"-"` }

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

ContextBuilder 在进入 session/context 前先把当前轮 `event.InputPart` 转为 user `model.Message`。session 只保存 `model.Message`，不保存 Event；Event 是运行时 I/O 协议，不承担上下文压缩、Provider 重放或持久化 schema 的职责。

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
    Messages       []*model.Message
    SystemPrompt   string
    TokenCount     int64
    TokenBudget    int64
    TurnCount      int
    CompactionHist []CompactionRecord
    ReadFileState  map[string]string // 最近读取的文件路径→内容，用于压缩后恢复
}

type CompactionRecord struct {
    Turn         int
    Strategy     string
    TokensBefore int64
    TokensAfter  int64
    Timestamp    int64
}

// Pipeline 按顺序应用策略，token 降到预算内即短路。
type Pipeline struct {
    strategies []CompressionStrategy
    counter    model.Counter
}

func (p *Pipeline) Compress(
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

#### 6 种内置策略

| 策略 | 触发条件 | 作用范围 | 说明 |
|------|---------|---------|------|
| `ToolResultBudget` | 每轮开始 | 单个工具结果 | 超大结果持久化到磁盘，向模型发送截断预览 + 磁盘路径 |
| `Snip` | 每轮开始（95% 阈值） | 最旧消息 | 硬限制：当 token 超过 95% 上下文窗口时丢弃最旧消息 |
| `MicroCompact` | 每轮开始 | 小窗口旧消息（窗口 3） | 对小窗口内的旧消息做内联摘要替换，不调用 LLM |
| `SessionMemoryCompact` | token 阈值 + session memory 存在 | 全部对话 | 跳过 LLM 调用，直接使用已有 session memory 作为摘要 |
| `AutoCompact` | token 阈值（85%） | 全部/部分对话 | 通过 Fork Agent 调用 LLM 生成完整摘要 |
| `ReactiveCompact` | API 413 错误 | 全部对话 | 紧急恢复：强制全量压缩，每次重试截断 20% 最旧消息组 |

策略按成本从低到高排列，token 降到预算内即短路。`PostCompactRestorer` 在全量压缩（SessionMemoryCompact/AutoCompact/ReactiveCompact）后运行，恢复上下文状态。

```go
// 关键常量
const (
    DefaultToolResultBudgetChars     = 30_000
    DefaultSnipThresholdRatio        = 0.95  // 95% 上下文窗口触发 snip
    DefaultMicroCompactWindowSize    = 3
    DefaultAutoCompactThresholdRatio = 0.85  // 85% 上下文窗口触发 autocompact
    DefaultAutoCompactBufferTokens   = 13_000
    DefaultMaxFilesToRestore         = 5
    DefaultFileBudgetTokens          = 50_000
    DefaultFileTokenLimit            = 5_000
    MaxConsecutiveAutocompactFailures = 3    // 熔断器：连续失败 3 次后禁用
)

// ToolResultBudgetStrategy 处理超大工具结果。
type ToolResultBudgetStrategy struct {
    MaxResultChars int    // 每个工具结果的字符上限，默认 30000
    PersistDir     string // 完整结果持久化目录
}

// SnipStrategy 硬限制丢弃最旧消息。
// 当 token 超过 SnipThresholdRatio * contextWindow 时触发。
type SnipStrategy struct {
    ThresholdRatio float64 // 默认 0.95
}

// MicroCompactStrategy 对小窗口旧消息做内联摘要。
type MicroCompactStrategy struct {
    WindowSize int // 每次处理的消息窗口大小，默认 3
}

// SessionMemoryCompactStrategy 跳过 LLM 摘要调用，
// 直接使用已有的 session memory 作为压缩摘要。
// 当 session memory 已激活时，这是最经济的压缩路径。
type SessionMemoryCompactStrategy struct {
    SessionMemory   SessionMemoryProvider           // 由 memory 包提供
    MinTokensToKeep int64                           // 压缩后保留的最小 token 数，默认 10_000
}

// SessionMemoryProvider 是 memory.SessionMemory 的接口抽象，
// 避免 compact 包直接依赖 memory 包。
type SessionMemoryProvider interface {
    Load() (string, error)
    IsActive() bool
}

// AutoCompactStrategy 通过 LLM 生成摘要。
// 注意：不直接持有 Agent 引用，避免 compact 包与根包循环依赖。
// 改为接受 Summarizer 函数，由 Agent Loop 在构造时注入具体实现
//（可以是 ForkAgent，也可以是直接的 LLM 调用）。
type AutoCompactStrategy struct {
    ThresholdRatio float64                                                   // 触发阈值比例，默认 0.85
    BufferTokens   int64                                                     // 预留 buffer，默认 13000
    Summarize      func(ctx context.Context, messages []*model.Message) (string, error) // 由 Agent Loop 注入
    Stats          *AutoCompactStats                                         // 熔断器状态
}

// AutoCompactStats 追踪自动压缩的统计信息和熔断器状态。
type AutoCompactStats struct {
    CompactionCount     int
    LastCompactTurn     int
    TotalSaved          int64
    ConsecutiveFailures int  // 连续失败计数
    Disabled            bool // 熔断器触发后为 true，本会话不再尝试 autocompact
}

// ReactiveCompactStrategy 紧急恢复压缩。
// 在 API 返回 prompt_too_long 错误时触发。
// 每次重试截断 20% 最旧消息组（truncateHeadForPTLRetry）。
type ReactiveCompactStrategy struct {
    Summarize        func(ctx context.Context, messages []*model.Message) (string, error) // 由 Agent Loop 注入
    TruncateRatio    float64 // 每次重试截断比例，默认 0.20
}
```

#### 熔断器逻辑

AutoCompact 内置熔断器，防止无限重试循环：

```go
func (s *AutoCompactStrategy) Apply(ctx context.Context, state *CompressionState) (*CompressionState, error) {
    if s.Stats.Disabled {
        return state, nil // 熔断器已触发，跳过
    }
    result, err := s.Summarize(ctx, state.Messages)
    if err != nil {
        s.Stats.ConsecutiveFailures++
        if s.Stats.ConsecutiveFailures >= MaxConsecutiveAutocompactFailures {
            s.Stats.Disabled = true // 熔断：本会话不再尝试
        }
        return state, err
    }
    s.Stats.ConsecutiveFailures = 0 // 成功则重置
    // ... 构建压缩后状态
}
```

#### API 不变量保护

压缩切割消息时，必须保护 `ToolUsePart` / `ToolResultPart` 配对完整性。任何移除消息头部的策略（Snip、AutoCompact、SessionMemoryCompact）在确定切割边界后，调用 `AdjustKeepBoundary` 修正：

```go
// AdjustKeepBoundary 确保切割边界不会拆散 tool_use/tool_result 对
// 或 thinking 流。从 proposedIndex 向前搜索，找到安全的切割点。
//
// 规则：
//   - 如果 proposedIndex 处的消息包含 ToolResultPart，
//     向前移动直到找到对应的 ToolUsePart 所在消息之前
//   - 如果 proposedIndex 处的消息与前一条消息共享同一 message ID
//     （thinking 流），向前移动到该组的起始位置
func AdjustKeepBoundary(messages []*model.Message, proposedIndex int) int
```

#### 压缩后状态恢复

全量压缩（SessionMemoryCompact/AutoCompact/ReactiveCompact）后，上下文丢失了最近读取的文件、活跃的 plan/skill 状态和延迟的工具声明。`PostCompactRestorer` 负责恢复这些状态：

```go
// PostCompactRestorer 在全量压缩后恢复上下文状态。
type PostCompactRestorer struct {
    MaxFilesToRestore int   // 恢复的最近文件数，默认 5
    FileBudgetTokens  int64 // 文件恢复总 token 预算，默认 50_000
    FileTokenLimit    int64 // 单文件 token 上限，默认 5_000
}

// Restore 在压缩后的消息列表中追加恢复内容：
//   1. 最近读取的文件（从 CompressionState.ReadFileState 获取）
//   2. 活跃的 plan 状态（如果存在）
//   3. 延迟的工具声明（MCP 工具等）
func (r *PostCompactRestorer) Restore(
    ctx context.Context, state *CompressionState,
) (*CompressionState, error)
```

`PostCompactRestorer` 不是 `CompressionStrategy`（它不减少 token），而是 `compact.Pipeline` 的后处理步骤。Pipeline 在任何全量压缩策略成功后自动调用 Restore。
```

### 2.3 缓存感知 System Prompt

```go
package blades

// PromptBuilder 将 system prompt 分为静态可缓存前缀和动态后缀。
// 静态部分跨会话缓存（如工具描述、行为指南），动态部分每会话变化（如 Memory、环境信息）。
type PromptBuilder struct {
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
func (b *PromptBuilder) Build(ctx context.Context) (*SystemPrompt, error)

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

3. **管线式压缩而非单一压缩器** — 当前 `ContextCompressor` 是全有或全无的单一接口。新设计将压缩分解为 6 个独立策略（含 SessionMemoryCompact）和一个压缩后恢复步骤，按成本从低到高排列，token 降到预算内即短路。轻量策略（Snip、MicroCompact）每轮都运行，SessionMemoryCompact 在 session memory 存在时跳过 LLM 调用，重量策略（AutoCompact）仅在阈值触发时运行。压缩策略通过 `Summarizer` 函数注入 LLM 能力，避免与根包循环依赖。AutoCompact 内置熔断器（连续 3 次失败后禁用），防止无限重试循环。

4. **缓存感知 System Prompt** — 当前 system prompt 是简单字符串，每次调用都完整发送。新设计将 prompt 分为静态前缀（跨会话不变）和动态后缀（每会话变化），配合 Provider 的 prompt cache 机制（如 Anthropic 的 cache_control），显著降低重复 token 消耗。动态 section 默认可缓存，需要显式标记不可缓存的 section（如 MCP 指令）。

5. **API 不变量保护** — 压缩切割消息时，`AdjustKeepBoundary` 确保不会拆散 `ToolUsePart`/`ToolResultPart` 配对或 thinking 流。这是 API 调用的硬约束——拆散的配对会导致 Provider 返回错误。

6. **压缩后状态恢复** — 全量压缩后，`PostCompactRestorer` 恢复最近读取的文件（最多 5 个，50K token 预算）、活跃的 plan/skill 状态和延迟的工具声明。这确保压缩后 Agent 不会"失忆"正在进行的工作。
