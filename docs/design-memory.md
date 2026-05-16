---
type: design
title: Memory 长期上下文 (v2)
date: 2026-05-07
status: draft
parent: design-agent-framework.md
related: [design-agent-framework.md, design-prompt.md]
tags: [agentos, memory, prompt, context]
---

# Memory 长期上下文 (v2)

## 概述

`memory/` 为 AgentOS 提供跨 turn、跨 session 或跨应用的长期上下文能力。v2 在 v1（`Recall` + `Remember`）基础上引入显式 `Forget`，并把核心协议收敛到 `Entry` / `Query` 两个结构体。文件布局、向量检索、自动抽取、workspace 遍历和后台调度仍属于具体实现或应用层。

Memory 不进入 root Agent 配置。应用通过 `prompt.Memory(mem, query)` 把 recall 结果作为 prompt section 注入模型请求。

> v2 不保留 v1 兼容形态。`design-prompt.md` 与 `design-agent-framework.md` 已同步至 v2 措辞。

## Memory 接口

```go
package memory

type Memory interface {
    Recall(ctx context.Context, query Query) ([]Entry, error)
    Remember(ctx context.Context, entry Entry) error
    Forget(ctx context.Context, entry Entry) error
}
```

语义：

- `Recall` 根据 `Query{Text, Limit, Filter}` 返回匹配的 `Entry`，调用方不关心底层是关键词、向量、图还是远程服务。
- `Remember` 写入一条已经归一化的 `Entry`。它不负责从原始对话中自动抽取多条记忆；抽取、总结和分片属于应用层或更高层组件。
- `Forget` 删除一条已知 `Entry`。core 语义固定为按 `Entry.ID` 删除；空 ID 返回错误，不存在的 ID 视为成功以保持幂等。

`content.Part` 是 Memory 与 prompt/event/model 共享的多模态叶子协议。Memory 返回 `Entry` 而不是 `model.Message`，避免长期上下文直接绑定模型历史形态，同时保留 ID、metadata 与时间戳边界。

为何 v2 引入 `Forget`：v1 仅提供读写主路径，应用要清理过期记录、撤回错误抽取或处理用户主动撤销时缺少统一入口。v2 把删除提升为一等方法，与 `Recall` / `Remember` 对称。

## Entry 与 Query

```go
type Entry struct {
    ID        string
    Parts     []content.Part
    Metadata  map[string]any
    CreatedAt time.Time
    UpdatedAt time.Time
}

type Query struct {
    Text   string
    Limit  int
    Filter map[string]any
}
```

命名说明：v2 把 v1 的 `Item` 重命名为 `Entry`。原因：

- 接口名已固定为 `Memory`（红线 r21），同包内无法再定义同名结构体复用 `Memory`。
- 候选 `Record` / `Memo` / `Fragment` 各自带数据库或 RAG 偏向；`Entry`「一条长期上下文条目」语义最贴合，与 Go 生态命名习惯一致。

实现示例：

- `memory.NewInMemory()`：适合测试和短生命周期 demo。
- SQLite：适合本地持久化、metadata 过滤和全文索引。
- vector database：适合语义检索。
- remote service：适合团队级共享 memory。

## 应用层注入策略

Memory 通过 prompt section 进入请求，section 端提供 recall query：

```go
memSection := prompt.Memory(
    mem,
    func(ctx context.Context) (memory.Query, error) {
        if s, ok := session.FromContext(ctx); ok {
            return memory.Query{
                Text:  "session " + s.ID(),
                Limit: 8,
            }, nil
        }
        return memory.Query{Text: "current task", Limit: 8}, nil
    },
)

builder := prompt.New(
    prompt.Text("Follow project instructions."),
    memSection,
)
```

Agent 构造不需要 `WithMemory` 之类 root option。不同应用可以在不同 section 中注入不同 query，而不影响 core Agent API。

## 与 Compact 的边界

Memory 与 Compact（[design-compact.md](design-compact.md)）在 Agent Loop 中**架构上完全解耦、互不感知**，不要把它们当作同一类压缩/裁剪操作：

| 维度 | Memory | Compact |
|------|--------|---------|
| 作用对象 | `*model.Request.System`（prompt 的 system 段） | `*model.Request.Messages`（消息历史段） |
| 调用入口 | `prompt.Memory(...)` section → `prompt.Builder.Build` | Agent Loop 请求构造阶段调用 `compactor.Compact(ctx, compact.Request{Messages: snapshot, TokenCounter: counter})` |
| 数据源 | 跨 turn / 跨 session 的长期上下文存储 | 当前 Session 的 `Messages()` 快照 |
| 是否进入 Session | 不进 | 不进（仅 rolling state 进 `State()`） |
| 是否每 step 重新计算 | 是（每个 model step 重新 `Recall`，除非 section 内部缓存） | 是（带状态 Compactor 通过 `__compact_*__` state 增量复用） |
| 体量控制责任方 | Memory 实现 + 应用层（`Query.Limit`、section 内 token 估算） | Compactor（`MaxTokens`、`KeepRecent`、迭代折叠） |

由此推出的应用层使用约束：

- **不要依赖 Compact 兜底 memory**：Compactor 的输入只看 messages，永远不会再次裁剪 system 段中的 memory 召回结果。memory 段超限会直接堆到 system 上，一旦 provider 报 context-too-long，Loop 的 `HintShrink` 也只会让 Compactor 进一步压缩 messages，无法替你削减 memory。
- **预算分摊在应用层完成**：建议在应用初始化处把 provider 上下文上限分为三段——`SystemBudget`（含 memory 召回 + 静态系统指令）/ `MessagesBudget`（compact 的 `MaxTokens`）/ `ResponseReserve`（输出预留），并分别注入 prompt 与 compact 的配置。
- **每 step 重新 Recall 是默认行为**：让 query 随当前任务自适应；如果 memory 后端调用昂贵，应用层应自行在 prompt section 中实现 per-turn 缓存，不要要求 core 层做。
- **Forget 与 Compact 的 rolling summary 无关**：`mem.Forget(ctx, entry)` 删除的是长期上下文条目，不会影响任何 Session 的 `__compact_summary_*__` 状态；反之 Compactor 的 offset 推进也不会触发 memory 的 Forget。两者生命周期独立。
- **Memory 召回不进入 compact 增量历史**：召回结果只存在于本次请求的 system 段，下次 step 时由 prompt builder 重新生成；不会被 Compactor 当作"未压缩消息"持续累积。

如需观测 system 段（含 memory）与 messages 段（含 compact view）的 token 占比，应用层通过 hook 或 observability 自行采样，core 不在 Memory 或 Compactor 接口上加协同字段。

## 异步抽取与遗忘边界

v2 core 仍不内置后台抽取或清理调度。推荐应用层流程：

1. Agent Loop 结束一个 turn，输出事件或通知。
2. 应用层 job 读取会话消息、工具结果或用户反馈。
3. job 生成需要长期保存的 `content.Part`。
4. job 调用 `mem.Remember(ctx, memory.Entry{ID, Parts, Metadata})`。
5. 当出现以下场景时调用 `mem.Forget`：
   - 用户主动撤回某条记忆或会话。
   - 应用基于 metadata（例如 `expires_at`）执行 TTL 清理。
   - 评估或人工纠错确认某条抽取错误。
6. 如需提示用户，应用通过自己的 notification/event bridge 回流。

这样 Memory 实现专注存取、检索与删除主路径；调度、频率控制、TTL 策略、用户确认和产品 UI 都留在应用层。

## 设计决策

1. **三方法接口**：`Recall` / `Remember` / `Forget` 覆盖读、写、删主路径。删除升格为一等公民。
2. **Entry 为核心**：`Remember` 写入一条 `Entry`，`Recall` 返回 `Entry`，`Forget` 接收 `Entry`，调用链保持一致。
3. **Forget 按 ID**：`Forget` 只按 `Entry.ID` 删除，忽略 Parts / Metadata / 时间戳；空 ID 返回错误，不存在 ID 视为成功。
4. **不进入 root Agent**：Memory 是 prompt 上下文来源之一，不应扩大根构造参数。
5. **返回 Entry 而非消息**：长期上下文可以是文本、图片、结构化 blob 或 thinking 片段，不绑定某一轮对话。
6. **后端自定义**：检索质量和存储形态由实现负责，core 只要求稳定 `Memory` 接口。
7. **Entry 命名**：数据载体命名为 `Entry`，避免泛化的 `Item` 与接口名 `Memory` 冲突。

## 与红线对照

- r21：`Memory` 固定为 `Recall` / `Remember` / `Forget` 三方法。
- r21：Memory 不进入 root Agent 配置，通过 `prompt.Memory` section 注入 recall 结果。
