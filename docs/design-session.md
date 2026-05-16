---
type: design
title: Session 与持久化
date: 2026-05-07
status: draft
parent: design-agent-framework.md
related: [design-agent-framework.md, design-event-agent-loop.md, design-compact.md, design-hook-extension.md]
tags: [agentos, session, persistence, message]
---

# Session 与持久化

## 1. 概述

Session 是 AgentOS 的**会话数据层**，承载两个用途：

1. **UI 历史展示**：聊天/对话型应用直接读取 Session 渲染历史。
2. **下一次模型调用的上下文**：Agent Loop 从 Session 取消息组装 `model.Request`。

两者要的都是已聚合的 final `*model.Message`（含 `Role`、`Parts`），不是流式增量也不是控制信令。Session 只做存取与原子性保证，**不内置压缩、过滤、重写**——这些策略归 Agent Loop。

线性消息列表的形态对齐 OpenAI、Anthropic 等主流 SDK 的上下文协议，也让 compact、prompt、policy 与 provider 转换的边界容易验证。

协议约束（与 r29 一致）：

- 会话存储单元是 `*model.Message`。
- `Role` 使用 `User`、`Assistant`、`Tool`；系统内容走 `model.Request.System`。
- 流式增量、工具中间态、控制信令通过 `event.*` 通道与 hook 承载，**不进 Session**。

## 2. Session 接口

```go
package session

type Session interface {
    ID() string
    Metadata() map[string]any
    State() map[string]any
    SetState(key string, value any)

    Append(ctx context.Context, msgs ...*model.Message) error
    Messages(ctx context.Context) ([]*model.Message, error)
}
```

Session 是**纯追加**的：一旦创建，外部只能通过 `Append` 增长消息列表，无法删除、截断或替换。所有"非追加"的初始化或重建都通过 `NewSession` 的构造选项完成。

```go
func NewSession(opts ...SessionOption) Session

func WithSessionID(id string) SessionOption
func WithMessages(msgs ...*model.Message) SessionOption
func WithMetadata(metadata map[string]any) SessionOption
func WithState(state map[string]any) SessionOption
```

方法语义：

- `ID` 返回稳定标识。默认 `uuid.NewString()` 全局唯一；构造时可注入自定义 ID（`WithSessionID`）。
- `Metadata` 返回会话级元数据快照，供应用标记用户、渠道、标签等业务索引。
- `State` / `SetState` 提供会话级 k/v 存取，供 agent 之间共享数据，以及 compactor 等策略层存放滚动状态（如增量摘要的 `offset`、`summaryContent`，参见 [design-compact.md](design-compact.md)）。
- `Append` 以变参追加消息；**同一次调用必须作为一个原子写入单元**，避免一次模型轮次中 assistant + tool 等成组消息被部分持久化。
- `Messages` 返回**当前消息的完整原始快照**，无任何压缩、过滤、截断；调用方不得依赖返回 slice 可被原地修改。

构造选项语义：

- `WithMessages(msgs...)`：会话起始消息（恢复、回放、fork 等场景的入口）。一次性写入，等价于在空 Session 上做一次 `Append`。
- `WithMetadata`：初始化会话级业务元数据。
- `WithState`：初始化会话级 k/v 状态。

Session **不**负责：token 预算、模型请求组装、工具执行、长期记忆召回、历史压缩、消息删除/截断/替换。这些能力分别属于 Agent Loop、`tools/`、`memory/`、`compact/`，以及"新建一个 Session"的应用层组合。

## 3. 历史载荷为什么是 `*model.Message` 而非 `event.Event`

Session 与事件协议的关系来自 [design-event-agent-loop.md](design-event-agent-loop.md) §1 与 r29 的边界声明：**event 与 message 不合并，转换边界集中在 `internal/convert/`**。本文进一步明确 Session 站在 message 这一边。

| | Session（数据层） | Event 通道（协议/流） |
|---|---|---|
| 载荷 | 已聚合的 `*model.Message` | `event.Input` / `event.Output` 增量与控制 |
| 典型条目 | user 提问、当前 turn 消费的 steer、assistant final、tool result | `TextDelta`、`ThinkingDelta`、`ToolStart`、`ToolEnd`、`Pause`、`Resume`、`Abort` |
| 服务对象 | UI 渲染、provider 调用、compactor | 流式渲染、hook、observability |
| 生命周期 | turn 结束后稳定快照 | turn 内瞬时事件流 |
| 持久化必要性 | 必要 | 可选（需要时由独立 `eventlog` / observability 落盘） |

不把 events 放进 Session 的具体理由：

1. 流式增量（`TextDelta`、`ThinkingDelta`、`ToolDelta`）数量级远大于 final message，混入 Session 会把"流缓存"和"持久状态"两件事搅在一起。
2. 控制信令（`Pause`、`Resume`、`Abort`）不是历史，进 Session 会污染 provider 上下文。`Steer` 兼具控制语义和消息语义：只有被当前 turn 消费或在 idle 状态开启新 turn 时，才转换成 user message 写入 Session；作为排队/调度信号本身不单独持久化。
3. compactor 对 Message 列表的语义清晰（按 token、角色、重要性截断重写）；对 event 增量做压缩没有意义。
4. UI 渲染需要的是 final message，不是中间流；用 events 还得在每次读取时再 fold 一次。
5. Session 与 Loop 协议解耦：Loop 协议演进时 Session 不需要跟着改。

如果应用需要事件级回放或审计，应该新增独立的 `eventlog`（或复用 hook + observability 落盘），不要把两类语义合到 Session。

## 4. Compaction 边界

Session 与 compaction **完全解耦**：

- Session 接口、构造选项、字段中**不出现**任何 compressor 概念。
- `Messages()` 永远返回完整原始快照（Session 是审计真相）。
- Compaction 是 **Agent Loop request construction** 的策略层职责，且**不写回 Session**。Loop 每次构造模型请求时按以下流程（Compactor 自适应契约见 [design-compact.md](design-compact.md) §触发时机）：
  1. `msgs, _ := session.Messages(ctx)`
  2. `view, err := compactor.Compact(ctx, compact.Request{Messages: msgs, TokenCounter: counter})`  // 纯变换，不副作用；Compactor 自身决定是否短路
  3. 用 `view` 组装 `model.Request` 调用 provider
  4. provider 返回的 final assistant 消息与本 step 的全部 tool 结果作为一个语义组通过一次 `Append` 写回 Session（仍然是完整原始消息，不是压缩后的视图）
- compactor 的滚动状态（如 summarize 的 `offset` / `summaryContent`）通过 `Session.State()` 持久化，避免每轮重算。参见 [design-compact.md](design-compact.md) §3。

### 为什么 append-only 是增量压缩的前提

Session 一旦 `Append`，已存在消息在 `Messages()` 返回切片中的下标就保持稳定，不会因后续追加前移、也不会被原地修改或删除。这一不变量直接支撑了 Compactor 的**增量摘要**实现：

- Compactor（典型为 `Summarize`）只需要在 `Session.State()` 中维护一个**单调递增**的 `offset`，即可在每次调用中精确区分"已压缩区 `msgs[:offset]`"与"未压缩区 `msgs[offset:]`"，无需对消息做内容指纹或显式 ID 追踪。
- 每次 `Compact` 调用只对 `msgs[offset:]` 中**新增**的部分做工作（必要时调 LLM 折叠），把 `msgs[:offset]` 已折叠到的 `summaryContent` 直接复用——避免对同一段历史重复调用摘要 LLM。
- 如果 Session 接口允许截断 / 覆盖 / 删除（即非 append-only），Compactor 就必须为每条消息分配稳定 ID 并维护映射，状态结构与失效语义会显著复杂化。append-only 把这部分复杂度直接消除。
- 反过来，"撤销 / fork / A/B" 等"看似改写历史"的操作通过新建 Session（参见 §8）承载，新 Session 的 compactor state 也是独立的，不会污染原 Session 的 offset。

详细的 offset 单调性、配对原子性、`KeepRecent` 硬上界、外部 reset 后回零等规则参见 [design-compact.md](design-compact.md) §增量压缩契约。

### State 键命名空间

`Session.State()` 是 agent 间共享 k/v 与策略层滚动状态的混合存储。为避免冲突，core **保留 `__` 双下划线前缀**作为内部命名空间：

| 前缀 | 归属 | 用途 |
|------|------|------|
| `__compact_*__` | `compact/` 包私有 | Compactor 的滚动状态，例如 `__compact_summary_offset__` / `__compact_summary_content__` |
| `__<corepkg>_*__` | core 其他子包预留 | 未来若需在 State 中存放运行时元数据（如 hint 缓存等） |

应用层在 `SetState` 时**应避免使用 `__` 前缀**，以免与 core 私有键冲突或被 core 实现升级时覆盖。Session 实现自身不强校验前缀（保持接口最小），但 Compactor 等内部组件读写自己的键时必须遵守此约定。

Compactor 的 state 键写入是 view-only 模型（参见上一段）下唯一允许的 Session 副作用，且与 message append 是不同 key 命名空间，互不污染——`Messages()` 返回的快照不会包含 state 内容，`State()` 不暴露消息历史。

这种"view-only"模型的好处：

- **审计与回放**：Session 永远保留完整真相，任何时点都能拿到原始消息列表。
- **策略可替换**：换一个 compactor 实现，下一轮立刻生效，不需要"还原"压缩痕迹。
- **A/B 与调试**：不同 compactor 可对同一 Session 并行计算视图，结果差异不污染状态。
- **接口最小**：Session 不需要 `Replace` 这种"完整覆盖"操作，简化并发与持久化语义。

代价：每轮模型调用都会重新跑一次 compactor。这对 `Window`、`ToolResultBudget` 等纯函数策略代价可忽略；`Summarize` 等带状态策略通过 `Session.State()` 缓存增量摘要避免重复 LLM 调用。

## 5. Context helper

Session 通过 context 注入运行时。最终态命名按 AgentOS 蓝图统一为 `session/` 子包导出：

```go
ctx = session.NewContext(ctx, sess)

if sess, ok := session.FromContext(ctx); ok {
    msgs, err := sess.Messages(ctx)
    _ = msgs
    _ = err
}

sess := session.Ensure(ctx) // 不存在则新建内存实例
```

helper 只承载当前 Session 引用，不负责创建、查找或恢复会话。

## 6. 多会话管理

Session core 不提供 Manager 抽象。常见做法由应用层组合：

- **单会话注入**：`session.NewContext` 把当前 Session 放进 ctx，整条调用链共用。
- **多会话索引**：应用持有 `map[string]Session` 或自带数据库映射（用户 ID、频道 ID → Session）。
- **跨会话发现**（列出/搜索/删除）：是后端实现细节，由具体存储后端（JSONL、SQLite、Redis 等）暴露自身 API；core 不为此设抽象接口。

这样保持 core 接口面最小，应用可按需组合。

## 7. 跨进程并发语义

- **单实例内**：`Append` / `Messages` 串行可见；并发安全由实现自身保证（`sessionInMemory` 使用并发容器；远程后端可用 mutex 或追加日志）。
- **跨进程**：core 不规定一致性级别。具体后端在自身文档中声明（last-write-wins 不适用，因为没有覆盖语义；纯追加场景下后端通常实现"顺序追加 + 单调读"）。
- **推荐使用模式**：以 step 为原子单元写入——turn 起始 `Append(ctx, userMsg)`；每个 model step + tool wave 完成后 `Append(ctx, assistantMsg, toolMsg)` 一次性写入语义组（参见 [design-event-agent-loop.md](design-event-agent-loop.md) §9）；compaction 不写回 Session（参见 §4）；不要把这些操作拆成长事务。
- 应用层可基于 `State()` 或后端自身字段实现版本号、租约、乐观锁，core 不内置。


## 8. 应用层 fork、A/B 与回放

core 不在 Session 接口上提供 `Fork` / `Replace` 这类方法。所有"从已有消息构造新会话"的需求统一通过 `NewSession + WithMessages` 完成。常见的 **fork** 场景由 core 提供一个 helper 函数，避免每个调用方重复"读快照 + 构造"模板代码：

```go
// Fork 从 src 拷贝消息构造一个新 ID 的 Session；额外 opts 可覆盖默认值。
func Fork(ctx context.Context, src Session, opts ...SessionOption) (Session, error) {
    msgs, err := src.Messages(ctx)
    if err != nil {
        return nil, err
    }
    base := []SessionOption{
        WithMessages(msgs...),
    }
    return NewSession(append(base, opts...)...), nil
}
```

`Fork` 只是 `NewSession + WithMessages` 的薄包装，不是 Session 接口的一部分。这样接口保持 6 方法，常用便利可由应用层 helper 暴露，未来增加更多组合（如 `ForkAt(ctx, src, n)`、`ForkUntil(ctx, src, predicate)`）也无须扩展接口。

典型用法：

```go
// fork：拷贝当前会话到新 ID
forked, _ := Fork(ctx, src)

// 回放：从外部日志/快照拿消息列表，构造新会话
replay := session.NewSession(
    session.WithSessionID("sess_replay_1"),
    session.WithMessages(loaded...),
)

// A/B：每个实验分配独立 ID（实验组由应用层外部索引关联），各自独立追加
expA, _ := Fork(ctx, base, session.WithSessionID("sess_exp_a"))
expB, _ := Fork(ctx, base, session.WithSessionID("sess_exp_b"))
```

**撤销不支持原地回退**：Session 是纯追加的，撤销由应用层用"快照 + 新建 Session"实现——在关键节点保存 Message 列表（或借助外部存储），需要回退时构造新 Session 并切换引用。

这个设计的核心是：**Session 标识一段单调演进的对话历史**。任何"看起来要修改历史"的操作（fork、A/B、回放、撤销、压缩落盘）都是另一段历史，应该是另一个 Session。core 用最小接口承载这个不变量，应用层（必要时配合 helper）在其上构建任意复杂的历史视图（树形、版本化、分支管理）。

## 9. 参考实现路径（附录）

> 主轴是上文目标态。本节仅说明从早期实现演进到目标态的参考顺序，不作为接口规范，也不暗含任何"过渡 alias 永久保留"。

参考路径：

1. 引入 `Append(ctx, msgs ...*model.Message)` 变参原子写入。
2. 引入 `Messages(ctx)` 返回完整原始快照。
3. 引入 `WithMessages` / `WithSessionID` / `WithMetadata` / `WithState` 构造选项。
4. 把任何残留在 Session 内的 compactor 调用迁出到 Agent Loop request construction（具体落点由 [design-compact.md](design-compact.md) 与 [design-event-agent-loop.md](design-event-agent-loop.md) 描述），Session 严格 view-only。
5. 完成后接口最终形态与本文 §2 完全一致：6 方法 append-only。常用 fork 可由应用层 helper 包装。

## 10. 设计决策

1. **载荷只是 `*model.Message`**：UI 与 provider 共用同一份稳定快照；events 走独立通道。
2. **纯追加（append-only）**：Session 一旦创建只能增长，不能截断、替换、删除。所有非追加的初始化通过 `NewSession + WithMessages` 完成。
3. **批量追加原子性**：一次模型轮次的成组消息要么全写入要么全失败。
4. **fork/replay/撤销 = 新建 Session**：用 `NewSession(WithMessages(...))` 统一入口；常用 fork 场景由应用层 helper 组合，不在 Session 接口上暴露 `Fork`/`Replace`/`Truncate`。
5. **Session 与 compaction 完全解耦**：compactor 是 view 层纯变换，不写回 Session。
6. **保留 State k/v**：服务于 agent 间共享数据与 compactor 滚动状态（避免每轮重算摘要）。
7. **业务索引不进 Session 接口**：用户、标签、渠道、实验组等查询维度由应用层或具体存储后端维护，避免把通用会话接口扩成索引模型。
8. **后端可选**：内存、JSONL、SQLite、Redis、远程服务都能实现同一 Session 语义；core 不为后端定义统一 `Store` 接口（避免过早抽象），由具体后端暴露各自 API。

## 11. 与红线对照

- **r18**：Session 接口固定为 `{ID, Metadata, State, SetState, Append, Messages}` 六方法（在 `session/` 包），纯追加；非追加初始化走 `NewSession + WithMessages` / `WithSessionID` / `WithMetadata` / `WithState`；常用 fork 由应用层 helper 组合。
- **r19**：Context helper 使用 `session.NewContext` / `session.FromContext` / `session.Ensure`（最终态在 `session/` 子包暴露）。
- **r29**：会话是线性 `*model.Message` 列表；fork、A/B、回放、撤销在应用层用"新建 Session + WithMessages"实现；events 不并入 Session。
