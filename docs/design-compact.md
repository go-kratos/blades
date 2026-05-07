---
type: design
title: Compact 上下文压缩
date: 2026-05-07
status: draft
parent: design-agent-framework.md
related:
  - design-agent-framework.md
  - design-event-agent-loop.md
  - design-session.md
  - design-model-provider.md
  - design-tool-system.md
tags: [agentos, compact, context, token-budget]
---

# Compact 上下文压缩

## 概述

`compact/` 把历史 `[]*blades.Message` 转换为仍然合法、但更短的消息列表，以适配 provider 的 token 预算。v1 的核心抽象是一个无副作用的纯函数式接口 `Compactor`，外加一个独立的 `TokenCounter` 用于估算消息体积。

设计边界：

- compact 只负责消息序列层面的整理，不持有 model client 全局态、不感知 session 持久化策略、不替代 provider adapter 的转换。
- compact 不依赖 root Agent、Tools 或 Memory。
- 触发时机由 Agent Loop 决定（详见 [触发时机](#触发时机)）；compact 实现本身不感知运行循环状态。
- 实现可以**有状态地**借助 Session 做增量摘要，但缺失 Session 时必须能优雅退化为无状态行为。

## 核心接口

```go
package compact

// Compactor compacts a message list into a shorter, still-valid message list.
type Compactor interface {
    Compact(ctx context.Context, msgs []*blades.Message) ([]*blades.Message, error)
}

// TokenCounter estimates token usage of one or more messages.
// Implementations may use model-specific tokenizers or heuristic approximations.
type TokenCounter interface {
    Count(msgs ...*blades.Message) int64
}
```

> 兼容性说明：当前代码中等价类型分别为根包的 `blades.ContextCompressor` 与 `blades.TokenCounter`；本文采用 `compact.Compactor` 作为最终目标命名，rename 在另行任务中推进，详见文末 [已知 gap](#已知-gap)。

约束（Compactor 必须遵守）：

- **不修改入参**：不原地修改调用方传入的 `*Message` 或 `Part`。
- **保持 Role 语义**：`User`、`Assistant`、`Tool` 三种角色的语义不被改写。
- **保持 tool_call / tool_result 配对**：含 `ToolPart` 的 assistant 消息与对应 tool 角色结果消息要么同时保留、要么同时丢弃；中间不得插入摘要使配对失配。
- **保持 assistant 消息内完整性**：单条 assistant 消息中的 thinking / text / tool 调用 part 不得被拆分到多条消息。
- **System 不混入历史**：系统级指令通过 `blades.ModelRequest.Instruction` 承载，不出现在 compact 的输入或输出消息序列中。
- **首尾角色对齐**：返回的序列首条不应是悬挂的 tool 角色结果（无对应 assistant tool_call）；末条不应是仅含 tool_call 而紧接被截断的 assistant 消息。
- **幂等等价性**：在没有外部状态变化的前提下，二次调用应当与一次调用产出语义等价的结果。

## 内置实现

```go
func NewWindow(opts ...WindowOption) Compactor

func NewSummarize(model blades.ModelProvider, opts ...SummarizeOption) Compactor

// planned, not in v1 minimum:
func NewToolResultBudget(budget int64) Compactor

func NewChain(cs ...Compactor) Compactor
```

### Window

`NewWindow` 保留最近若干消息，超出限制时从历史头部丢弃：

```go
c := compact.NewWindow(
    compact.WithMaxMessages(100),
    compact.WithMaxTokens(32_000),
    compact.WithTokenCounter(tokens),
)
```

行为细则：

- `MaxMessages <= 0` 表示不限制条数；`MaxTokens <= 0` 表示不限制 token。两者可同时启用，先按条数裁剪、再按 token 裁剪。
- 头部裁剪遇到 ToolPart 配对边界时按"配对原子"对齐：当被丢弃边界落在一条 assistant tool_call 与其 tool 结果之间时，向前移动边界，把整对一起丢弃。
- 当裁剪导致首条变成悬挂 tool 结果时，再额外丢弃这条 tool 结果。
- 空输入直接返回空输出，无错误。

### Summarize

`NewSummarize` 将"过早的历史"折叠为一段滚动摘要，最近 N 条消息保持原样。摘要由一个外部 `ModelProvider` 生成：

```go
c := compact.NewSummarize(
    summaryModel,
    compact.WithSummaryMaxTokens(20_000),
    compact.WithSummaryKeepRecent(10),
    compact.WithSummaryBatchSize(20),
    compact.WithSummaryInstruction("Summarize the following transcript ..."),
    compact.WithTokenCounter(tokens),
)
```

工作流：

1. 读取 Session 中持久化的 `(offset, summaryContent)`（key 由 compact 包私有，建议形如 `__compact_summary_offset__` / `__compact_summary_content__`）。Session 不存在时使用零值，等价于一次性纯函数。
2. 构造工作视图 `[summaryMsg?] + msgs[offset:]`。
3. 当 token 估算超过 `SummaryMaxTokens` 时，从 `offset` 起取下一批 `BatchSize` 条（不越过 `len(msgs) - KeepRecent` 边界），调用 `model.Generate` 与既有 `summaryContent` 合并产生新摘要；推进 `offset`。
4. 重复直到工作视图在预算内或没有可压缩区间。
5. 把更新后的 `(offset, summaryContent)` 写回 Session。
6. 若 Session 在两次调用之间被外部 reset 致 `offset > len(msgs)`，重置为零值。

错误处理：摘要 LLM 调用失败时返回错误，由 Loop 决定回退（见 [错误处理](#错误处理与可观测性)）。

### ToolResultBudget（planned）

为大型工具结果（典型如读文件、网页抓取、长 stdout）做"结果体积预算"。语义：

- 仅截断 `ToolPart.Response` 字符串；不丢弃配对、不改变 `ID`/`Name`/`Request`。
- 保留首尾片段并写入截断标记；完整结果由调用方（应用层 artifact、session store、工具运行时）另存。
- 超长 binary 结果（`DataPart` 体积）应由 `Window`/应用层先剥离，不在 `ToolResultBudget` 范围内。

### Chain

`NewChain` 顺序执行多个 `Compactor`，前一个的输出作为后一个的输入：

```go
c := compact.NewChain(
    compact.NewToolResultBudget(64*1024),
    compact.NewWindow(compact.WithMaxMessages(80)),
    compact.NewSummarize(summaryModel, compact.WithSummaryMaxTokens(20_000)),
)

msgs, err := c.Compact(ctx, original)
```

契约：

- 任一 stage 返回 error 即短路返回。
- 每个 stage 自身必须保证 [核心接口约束](#核心接口) 中的不变量；`NewChain` 不做额外校验。
- 顺序敏感：通常推荐 `预算→窗口→摘要` 的顺序，先压缩单条再压缩总量、最后做语义级折叠。

## 触发时机

Compactor 的调用点位于 Agent Loop。Loop 在以下条件触发：

1. **软阈值**：构建 `ModelRequest` 前用 `TokenCounter` 估算消息总量超过配置阈值。
2. **provider 硬错误重试**：provider 返回 context-too-long 类错误后单次重试。为避免死循环：
   - 设置最大重试次数（建议 1–2 次）。
   - 每次重试要求消息长度严格单调下降；不下降则停止并向上抛错。
3. **显式 API**：应用层主动要求整理会话（如 `/compact` 命令）。
4. **工具结果落入历史前**：可在工具执行返回后立即对单条做 `ToolResultBudget`，避免下一轮再次触发完整 compact。

落地结果：

- compact 输出可作为本轮 `ModelRequest.Messages` 直接使用；
- Loop 决定是否将压缩结果回写到 Session（覆盖原历史 / 仅作为本轮视图）。无回写时，下次调用 compact 仍会重做（除非 Summarize 通过 Session 复用了 rolling summary）。

## 不变量与 provider 兼容

不变量校验分两层：

- **compact 层**：保证通用消息序列合法（见核心接口约束）。每个内置实现在返回前自校验；建议引入共享 helper（如 `internal/compactutil.Validate`，planned）以减少重复。
- **provider adapter 层**：在 `ModelRequest → provider 原生请求` 转换时再做 provider 特定校验（OpenAI 的 `tool_call_id` 配对、Anthropic 的 `tool_use`/`tool_result` block 顺序、Gemini 的 part 类型等）。

任何 provider 特定约束都不应反向依赖 compact 包。

## 错误处理与可观测性

错误回退策略（推荐由 Loop 实现）：

- compact 返回 error 时，Loop 不应静默把原历史送给 provider；应当：
  - 记录指标与 error；
  - 若是 Summarize 的 LLM 错误：降级为一次性 `Window`-only 压缩或返回给上层；
  - 若是不变量违反：直接 fail-fast，避免污染 Session。

推荐 metric / log：

- `compact_triggered_total{reason=soft|retry|explicit|tool}`
- `compact_token_before` / `compact_token_after`（直方图）
- `compact_summarize_latency_seconds`、`compact_summarize_tokens`
- `compact_invariant_violations_total`（启用校验时）

## 设计决策

1. **单一接口 + 组合优先**：所有压缩方式都表现为 `Compactor`，通过 `Chain` 组合；不同策略不需要新接口。
2. **TokenCounter 与 Compactor 解耦**：估算与压缩各司其职，便于在 Loop / 监控 / 不同策略中复用同一计数器。
3. **摘要器以 `ModelProvider` 注入**：复用 provider 的 instruction、流式、超时、重试与工具豁免能力；不引入新的 `SummarizeFunc` 抽象。
4. **状态可选**：无 Session = 等价无状态调用；有 Session = 借助 `State()` 做增量摘要。compact 不暴露独立的状态接口。
5. **触发由 Loop 控制**：compact 实现不感知运行循环状态，便于单测与替换。
6. **配对完整性是 compact 自身责任**：不外推到 provider adapter，避免重复实现。

## 与相邻设计的接口面

- [`design-event-agent-loop.md`](design-event-agent-loop.md)：触发条件、重试与回写策略。
- [`design-session.md`](design-session.md)：Summarize 的滚动状态键命名空间、回写历史的可选性。
- [`design-model-provider.md`](design-model-provider.md)：Summarize 注入的 `ModelProvider`、context-too-long 错误的归一化。
- [`design-tool-system.md`](design-tool-system.md)：`ToolResultBudget` 与 `ToolPart`/工具运行时 artifact 存储的边界。

## 与红线对照

- r20：`Compactor` 单一接口；内置 `NewWindow`、`NewSummarize`、`NewChain` 在 v1 提供，`NewToolResultBudget` 列为 planned。
- r20：`TokenCounter` 与 `Compactor` 同层，互不依赖。
- r20：摘要器以 `ModelProvider` 注入，避免 compact 与 provider 形成依赖环。
- r20：provider invariant 由实现内部负责，并建议复用 helper（planned）。

## 已知 gap

本文采用目标命名 `compact.Compactor` / `compact.TokenCounter`，与当前代码现状存在以下差异，将在后续任务中收敛，不属于本设计文档的修订范围：

1. 代码现状包名为 `context/{window,summary}`，接口名为 `blades.ContextCompressor`；rename 到 `compact/Compactor` 待迁移。
2. 触发点目前在 `Session.History()` 内，本文按设计目标描述为 Agent Loop 触发；落地后需把触发点迁移到 Loop。
3. `Window` 当前实现未保护 tool_call / tool_result 配对，本文已将其写为契约；实现需补齐边界对齐逻辑。
4. `Summarize` 已实现 rolling summary + offset 持久化（state key `__summary_offset__` / `__summary_content__`），rename 到 `compact` 包后建议同步把 key 改为 `__compact_*__` 前缀。
5. `ToolResultBudget`、`Chain`、共享不变量校验 helper 尚未实现，文中以 planned 标注。
