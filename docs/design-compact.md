---
type: design
title: Compact 系统
parent: design-agent-framework.md
date: 2026-05-01
status: draft
modules: [module-2]
---

# Compact 系统

`compact/` 负责把 `[]*model.Message` 控制在 token budget 内。它只依赖 `model/`，不依赖 Provider、`blades.Agent`、`event/`、`tools/` 或 `memory/`。

## Pipeline

```go
package compact

type Pipeline struct {
    strategies []Strategy
    counter    model.Counter
}

type Strategy interface {
    Name() string
    ShouldApply(ctx context.Context, state *State) bool
    Apply(ctx context.Context, state *State) (*State, error)
}

type State struct {
    Messages     []*model.Message
    TokenCount   int64
    TokenBudget  int64
    Turn         int
    Records      []Record
}
```

Pipeline 按成本从低到高执行策略，token 降到预算内即短路。策略返回新的 `State`，不原地修改共享 message slice。

## 内置策略

| 策略 | 触发 | 作用 |
|------|------|------|
| `ToolResultBudget` | 每轮开始 | 超大工具结果替换为预览和外部引用 |
| `Snip` | 接近硬窗口 | 丢弃最旧的安全消息组 |
| `MicroCompact` | 旧小窗口 | 本地摘要短窗口，不调用 LLM |
| `SessionSummary` | 已有 session summary | 使用既有摘要替代旧历史 |
| `AutoCompact` | 软阈值 | 通过注入的 summarizer 生成摘要 |
| `ReactiveCompact` | prompt too long | 紧急压缩并重试 |

LLM 摘要通过函数注入：

```go
type Summarizer func(ctx context.Context, messages []*model.Message) (string, error)
```

这样 `compact/` 不持有 Provider 或 Agent，也不会形成循环依赖。

## 不变量保护

压缩不能破坏 Provider message invariant。任何裁剪头部消息的策略都必须先修正保留边界：

```go
func AdjustKeepBoundary(messages []*model.Message, proposed int) int
```

必须保护：

- `ToolUsePart` 与对应 `ToolResultPart` 成对保留或成对移除。
- 同一 assistant message 的 thinking/text/tool_use 增量不能被拆散。
- System prompt 和 compaction summary 的位置保持 provider 可接受。

## 工具结果预算

`ToolResultBudget` 不导入 `tools/`。Agent Loop 把工具结果转换为 `model.ToolResultPart` 后，compact 策略只处理 message part。完整结果持久化位置由应用或 session store 决定，compact 只保存 provider 可读的引用文本或 JSON。

## 设计决策

1. **策略管线而非单一 compressor**：不同成本和风险的压缩方式可以按顺序组合。
2. **函数注入 summarizer**：避免 `compact/` 依赖 Provider 或 root Agent。
3. **不变量先于节省 token**：宁可少压缩，也不能生成 Provider 拒绝的消息序列。
