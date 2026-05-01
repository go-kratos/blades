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
