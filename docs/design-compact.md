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

`compact/` 把历史 `[]*model.Message` 转换为仍然合法、但更短的消息列表，以适配 provider 的 token 预算。v1 的核心抽象是一个无副作用的纯函数式接口 `Compactor`，外加一个独立的 `TokenCounter` 用于估算消息体积。

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
    Compact(ctx context.Context, msgs []*model.Message) ([]*model.Message, error)
}

// TokenCounter estimates token usage of one or more messages.
// Implementations may use model-specific tokenizers or heuristic approximations.
type TokenCounter interface {
    Count(msgs ...*model.Message) int64
}
```

约束（Compactor 必须遵守）：

- **不修改入参**：不原地修改调用方传入的 `*Message` 或 `Part`。
- **保持 Role 语义**：`User`、`Assistant`、`Tool` 三种角色的语义不被改写。
- **保持 tool_call / tool_result 配对**：含 `content.ToolUse` 的 assistant 消息与对应 `content.ToolResult` 结果消息要么同时保留、要么同时丢弃；中间不得插入摘要使配对失配。
- **保持 assistant 消息内完整性**：单条 assistant 消息中的 thinking / text / tool 调用 part 不得被拆分到多条消息。
- **System 不混入历史**：系统级指令通过 `model.Request.System` 承载，不出现在 compact 的输入或输出消息序列中。
- **首尾角色对齐**：返回的序列首条不应是悬挂的 tool 角色结果（无对应 assistant tool_call）；末条不应是仅含 tool_call 而紧接被截断的 assistant 消息。
- **幂等等价性**：在没有外部状态变化的前提下，二次调用应当与一次调用产出语义等价的结果。

## 内置实现

```go
func NewWindow(opts ...WindowOption) Compactor

func NewSummarize(provider model.Provider, opts ...SummarizeOption) Compactor

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
- 头部裁剪遇到 tool use / tool result 配对边界时按"配对原子"对齐：当被丢弃边界落在一条 assistant tool_call 与其 tool 结果之间时，向前移动边界，把整对一起丢弃。
- 当裁剪导致首条变成悬挂 tool 结果时，再额外丢弃这条 tool 结果。
- 空输入直接返回空输出，无错误。

### Summarize

`NewSummarize` 将"过早的历史"折叠为一段滚动摘要，最近 N 条消息保持原样。摘要由一个外部 `model.Provider` 生成：

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

- 仅截断 `content.ToolResult.Parts` 中的超长文本结果；不丢弃配对、不改变 `ID`/`Name`。
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

## 增量压缩契约

Session 是 append-only（参见 [design-session.md](design-session.md) §4）：一旦消息被 `Append` 写入，其在 `Messages()` 返回切片中的下标就保持稳定，不会因为后续追加而前移；既存消息也不会被原地修改或删除。这一不变量是增量压缩的前提——Compactor 只需要维护一个**单调递增**的 `offset`，即可在每次调用中精确区分"已压缩区"与"未压缩区"，无需对消息做内容指纹或 ID 追踪。

带状态 Compactor（典型为 `Summarize`）的标准增量流程：

1. 从 `Session.State()` 读取私有键 `__compact_summary_offset__` / `__compact_summary_content__`，得到 `(offset, summaryContent)`。Session 不存在或键缺失时取零值 `(0, "")`，等价于一次纯函数调用。
2. 构造工作视图 `view = [summaryMsg(summaryContent)?] + msgs[offset:]`。已压缩部分不再进入摘要 LLM 的输入。
3. 仅当 `view` 仍超出预算时，从 `msgs[offset:]` 取下一批进入摘要 LLM；既有 `summaryContent` 与新批次合并产出新摘要；推进 `offset`。
4. 把更新后的 `(offset, summaryContent)` 写回 `Session.State()`；下次调用直接复用，避免对同一段历史重复调用 LLM。

`offset` 的稳定性规则（实现必须遵守）：

- **单调递增**：除"外部 reset"场景外，`offset` 不允许回退。
- **不可越界**：`offset ≤ len(msgs)` 永远成立；若读到 `offset > len(msgs)`（外部 reset / 清空 / fork 出更短的会话），视为状态失效，回零并从头建摘要。
- **配对原子性优先**：推进 `offset` 时遵守 [核心接口](#核心接口) 中的 tool_call / tool_result 配对约束——`offset` 不得停在 assistant tool_call 与其对应 tool_result 之间，必须一次跨过整对。
- **`KeepRecent` 是硬上界**：`offset` 不得越过 `len(msgs) - KeepRecent`。即使预算仍超，最近 N 条原文也保持不被折叠（保护近期上下文的语义保真度）。
- **无 Session 退化**：若运行时未注入 Session，Compactor 退化为无状态调用——每次都从 `offset=0` 全量计算。这是合法行为，但失去增量复用的性能收益，应在文档与日志中提示应用层。

无状态策略（`Window` / `ToolResultBudget`）不需要 `offset`：

- `Window` 每次都是 O(n) 切片 + 配对回退，纯函数式；它依赖的"近期"语义本身就是位置无关的滑动窗口。
- `ToolResultBudget` 只对单条消息内的 `content.ToolResult.Parts` 做长度截断，不跨消息累计状态。
- 两者**不写** `Session.State()`，避免污染状态命名空间。

`Chain` 不聚合状态：每个 stage 自带（或不带）`offset`，Chain 仅串接调用顺序，不为子 Compactor 提供共享 state 容器。

## 迭代压缩契约

实际运行中，单次 `Compact` 调用折叠一个批次后，视图可能仍然超过预算（例如最近一批工具结果集中爆量、或摘要 LLM 自身受 token 上限约束只能逐批吃）。"上下文不足时可以一直进行压缩控制上下文大小"是 Compactor 的内部责任，不应让上层多次调用 `Compact` 来手动驱动。

带状态 Compactor 的内部循环规约：

```
for {
    view := assemble(summaryContent, msgs[offset:])
    if tokens(view) <= budget                       { return view, nil }   // ① 预算达成
    if offset >= len(msgs) - keepRecent             { return view, nil }   // ② 无可压区
    if iterations >= maxFoldIterations              { return view, nil }   // ③ 安全阀
    summaryContent, offset = foldNextBatch(...)
    iterations++
}
```

终止条件三选一：

1. **预算达成**：视图 token ≤ `MaxTokens`，正常返回。
2. **无可压缩区间**：`offset` 已抵达 `len(msgs) - KeepRecent`，最近 N 条不允许折叠；返回当前最佳视图，由 Loop 决定后续动作。
3. **安全上限**：单次 `Compact` 调用内的折叠批次数达到内部上限（防御性，避免摘要 LLM 异常时陷入死循环）；建议默认 `maxFoldIterations = 8`，可由实现暴露选项调优。

返回视图后两层兜底机制（与 [design-event-agent-loop.md](design-event-agent-loop.md) §HintShrink / §9 对齐）：

- **Step 内（Compactor 自身）**：上述循环；属于"step 内迭代折叠"。
- **Step 间（Loop 透传 hint）**：若 provider 实际调用仍返回 context-too-long 错误，Loop 通过 `compact.WithHint(ctx, HintShrink)` 透传 hint 进入**同一 step 的第二次**请求构造。Compactor 在 hint 模式下应采取更激进的策略（例如降低 `KeepRecent`、启用更紧的 `ToolResultBudget`），并必须返回**严格单调下降**的视图（token 数严格小于上一次返回的视图）。最大重试次数默认 1 次；若返回视图未严格下降或仍超预算，Loop **fail-fast** 抛出 `event.Error` 并终止 turn。

两层机制的职责边界：

| 层级 | 触发主体 | 触发条件 | 期望效果 |
|------|----------|----------|----------|
| Step 内迭代 | Compactor 自身 | 当前视图超 `MaxTokens` 估算 | 在不调 provider 的前提下尽量逼近预算 |
| Step 间 hint | Loop | provider 实际报 context-too-long | 在 Compactor 估算与 provider 真实账本之间补差 |

实现要点：

- 迭代循环必须在 `Compact` 一次调用内完成；不要把"再来一次"的责任甩给 Loop——Loop 的重试只针对 provider 真实硬错误，不替代估算误差。
- 配对原子性、`KeepRecent` 硬上界等约束在每次循环内都要重新校验，不允许在中间临时态违反。
- 每次循环推进必须做出**实际进展**（offset 增加 ≥ 1 条配对原子，或 `summaryContent` 变化）；零进展时立即跳出，防止死循环。

## 触发时机

Compactor 的调用点位于 Agent Loop 的请求构造阶段，遵循**"Loop 无条件调用、Compactor 自适应"**契约：

1. **Loop 无条件调用**：每个 model step 构建 `*model.Request` 之前，Loop 都调用一次 `compactor.Compact(ctx, snapshot)`（hint 通过 `compact.WithHint(ctx, ...)` 注入 ctx）。Loop 不再判断"是否到了该压缩的时机"。
2. **Compactor 自适应短路**：
   - `Window` / `ToolResultBudget` 等纯函数策略在已经低于预算时直接 `return msgs, nil` 零成本透传。
   - `Summarize` 等带状态策略读取 `Session.State()` 中的 rolling summary 键（`__compact_summary_offset__` / `__compact_summary_content__`），仅当未摘要部分增长跨过阈值时才调用 LLM；其余调用直接拼接已有摘要 + 增量原文返回。
3. **provider 硬错误重试**：当 provider 返回 context-too-long 类错误后，Loop 用 hint 重新构造一次请求，并通过 `compact.WithHint(ctx, HintShrink)` 透传给 `compactor.Compact`，Compactor 必须返回**严格单调下降**的视图。最大重试次数默认 1 次；若仍未下降，Loop fail-fast 抛出 `event.Error` 并终止 turn。
4. **应用层显式整理**：`/compact` 命令、TTL 触发等由应用层主动调用 `Compactor` 或 `Memory` API 完成，与 Loop 路径解耦。Compactor 自身只关心"按预算压"。
5. **工具结果裁剪**：`compact.NewToolResultBudget(maxBytes)` 作为 Chain 中的一个阶段，统一受上述流程驱动，不需要在 Loop 中做特化分支。

落地规则：

- compact 输出仅用于本次 `*model.Request.Messages`；
- **不写回 Session**：Session 严格 view-only（参见 [design-session.md](design-session.md) §4）；Compactor 的 rolling state 通过 `Session.State()` 私有 key 持久化，与协议历史正交；
- 下次 step 重新调用 compact，仍由 Compactor 自身决定是短路透传还是重新计算（Summarize 借助 rolling summary 复用，避免重复 LLM 调用）。

## 与 Memory 的关系

Memory（`memory.Memory`，参见 [design-memory.md](design-memory.md)）和 Compact 在 Agent Loop 中**完全解耦、互不感知**：

- **作用域不同**：
  - Memory 召回结果通过 `prompt.Memory` section 进入 `*model.Request.System`（system 段是 prompt builder 的产物）。
  - Compactor 的输入与输出都只是 `[]*model.Message`，对应 `*model.Request.Messages`。
- **调用顺序（在请求构造阶段）**：
  ```
  snapshot := session.Messages(ctx)              // 1. 全量原始消息
  view     := compactor.Compact(ctx, snapshot)   // 2. 仅作用于 Messages 段
  system   := prompt.Builder.Build(ctx)          // 3. memory.Recall 在此处发生
  request  := &model.Request{
      System:   systemTextFrom(system),
      Messages: view + turnLocalPending,
      Tools, Options,
  }
  ```
  Compactor 不读 `system`，prompt builder 也不读 `view`；二者在 Agent Loop 的请求构造阶段汇合，但不互相依赖。
- **Compactor 不会再次裁剪 Memory**：召回结果在 system 段中保留原样，不进入 Compact 输入；意味着 memory section 自身的体量控制（`memory.Query.Limit`、应用层在 section 内做 token 估算）必须由 memory / 应用层负责，不会因 compact 兜底而被掩盖。
- **预算分摊建议**：Provider 的上下文上限 = `SystemBudget` + `MessagesBudget` + `ResponseReserve`。
  - `SystemBudget` 涵盖系统指令 + memory 召回 + 其他静态 prompt 段；超限由 prompt 层自己处理。
  - `MessagesBudget` 是 Compactor 的 `MaxTokens` 实际配额，仅计 `view` 内的消息体量。
  - `ResponseReserve` 给模型输出留位，应用层根据任务调整。
  - core 不强制具体分配比例；建议在应用初始化处一次性算好后分别注入 prompt 和 compact 的配置中。
- **不写 Session 的对称性**：Compactor 不修改 Session.Messages（仅写自己的 state 键）；Memory 不修改 Session.Messages（不出现在 `Append` 写入路径）。两者都只是"读取 + 派生 view"，确保 Session 仍是审计真相唯一来源。

应用层若需要 memory 与 compact 协同观测（例如统计 system 段与 messages 段的 token 占比），通过 hook 或 observability 自行采样，不要在 Compactor 或 Memory 接口上加耦合。

## 不变量与 provider 兼容

不变量校验分两层：

- **compact 层**：保证通用消息序列合法（见核心接口约束）。每个内置实现在返回前自校验；建议引入共享 helper（如 `internal/compactutil.Validate`，planned）以减少重复。
- **provider adapter 层**：在 `model.Request → provider 原生请求` 转换时再做 provider 特定校验（OpenAI 的 `tool_call_id` 配对、Anthropic 的 `tool_use`/`tool_result` block 顺序、Gemini 的 part 类型等）。

任何 provider 特定约束都不应反向依赖 compact 包。

## 错误处理与可观测性

错误回退策略（由 Loop 实现）：

- compact 返回 error 时，Loop **fail-fast**：以 `event.Error` 输出错误并结束当前 turn，不静默把原历史送给 provider，也不静默吞掉错误。
- 上层（应用、hook、observability）可在收到 `event.Error` 后选择重试、降级到只读 `Window` 策略，或提示用户。

推荐 metric / log：

- `compact_triggered_total{reason=step|retry|explicit|tool}`
- `compact_token_before` / `compact_token_after`（直方图）
- `compact_summarize_latency_seconds`、`compact_summarize_tokens`
- `compact_invariant_violations_total`（启用校验时）

## 设计决策

1. **单一接口 + 组合优先**：所有压缩方式都表现为 `Compactor`，通过 `Chain` 组合；不同策略不需要新接口。
2. **TokenCounter 与 Compactor 解耦**：估算与压缩各司其职，便于在 Loop / 监控 / 不同策略中复用同一计数器。
3. **摘要器以 `model.Provider` 注入**：复用 provider 的 system prompt、流式、超时、重试与工具豁免能力；不引入新的 `SummarizeFunc` 抽象。
4. **状态可选**：无 Session = 等价无状态调用；有 Session = 借助 `State()` 做增量摘要。compact 不暴露独立的状态接口。
5. **Loop 无条件调用、Compactor 自适应**：compact 实现不感知"是否到该压"，由策略自身决定短路或工作；Loop 不写"判断阈值再触发"分支，避免重复实现与漂移。
6. **配对完整性是 compact 自身责任**：不外推到 provider adapter，避免重复实现。
7. **增量基于 Session append-only**：消息下标稳定 ⇒ 单 `offset` 即可表达"已压缩边界"；不需要消息 ID 或内容指纹。无 Session 时退化为无状态全量计算。
8. **Step 内迭代 + Step 间 hint 两层兜底**：单次 `Compact` 调用内部循环折叠到预算/无可压区/安全阀；provider 仍报 context-too-long 由 Loop 触发 `HintShrink` 重试 1 次；仍不下降 fail-fast。两层职责正交，不互相替代。
9. **Memory 与 Compact 解耦**：Memory 走 system 段、Compact 走 messages 段；两者在请求构造阶段汇合但互不感知；预算由应用层三段分摊，不互相兜底。

## 与相邻设计的接口面

- [`design-event-agent-loop.md`](design-event-agent-loop.md)：触发条件、请求构造 pipeline、HintShrink 重试与 Compactor 内部迭代的两层关系。
- [`design-session.md`](design-session.md)：append-only 与 offset 增量的关系、Summarize 的滚动状态键命名空间、回写历史的可选性。
- [`design-model-provider.md`](design-model-provider.md)：Summarize 注入的 `model.Provider`、context-too-long 错误的归一化。
- [`design-tool-system.md`](design-tool-system.md)：`ToolResultBudget` 与 `content.ToolResult`/工具运行时 artifact 存储的边界。
- [`design-memory.md`](design-memory.md)：Memory 与 Compact 的解耦边界、system 段与 messages 段的预算分摊。

## 与红线对照

- r20：`Compactor` 单一接口；内置 `NewWindow`、`NewSummarize`、`NewChain` 在 v1 提供，`NewToolResultBudget` 列为 planned。
- r20：`TokenCounter` 与 `Compactor` 同层，互不依赖。
- r20：摘要器以 `model.Provider` 注入，避免 compact 与 provider 形成依赖环。
- r20：provider invariant 由实现内部负责，并建议复用 helper（planned）。

## 已知 gap

本文采用目标命名 `compact.Compactor` / `compact.TokenCounter`，与当前代码现状存在以下差异，将在后续任务中收敛，不属于本设计文档的修订范围：

1. 代码现状包名为 `context/{window,summary}`，接口名为 `blades.ContextCompressor`；rename 到 `compact/Compactor` 待迁移。
2. 触发点目前在 `Session.History()` 内，本文按设计目标描述为 Agent Loop 触发；落地后需把触发点迁移到 Loop。
3. `Window` 当前实现未保护 tool_call / tool_result 配对，本文已将其写为契约；实现需补齐边界对齐逻辑。
4. `Summarize` 已实现 rolling summary + offset 持久化（state key `__summary_offset__` / `__summary_content__`），rename 到 `compact` 包后建议同步把 key 改为 `__compact_*__` 前缀。
5. `ToolResultBudget`、`Chain`、共享不变量校验 helper 尚未实现，文中以 planned 标注。
6. 当前 `Summarize` 实现单次 `Compact` 调用只折叠一个批次，不在 step 内迭代到预算；新增的[迭代压缩契约](#迭代压缩契约) 要求循环到预算/无可压区/安全阀任一终止条件，落地需把外层循环下沉到 Compactor 内部。
7. 当前实现未对 `offset > len(msgs)` 做防御回零；新增契约要求外部 reset / fork 后自动失效重建，落地需补一个一致性检查。
