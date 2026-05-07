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

`memory/` 为 AgentOS 提供跨 turn、跨 session 或跨应用的长期上下文能力。v2 在 v1（`Recall` + `Remember`）基础上引入显式 `Forget`，并把所有方法改为 variadic option 形态，避免后续扩字段引入破坏性变更。文件布局、向量检索、自动抽取、workspace 遍历和后台调度仍属于具体实现或应用层。

Memory 不进入 root Agent 配置。应用通过 `prompt.Memory(mem, query, ...)` 把 recall 结果作为 prompt section 注入模型请求。

> v2 不保留 v1 兼容形态。`design-prompt.md` 与 `design-agent-framework.md` 已同步至 v2 措辞。

## Memory 接口

```go
package memory

type Memory interface {
    Recall(ctx context.Context, query string, opts ...RecallOption) ([]content.Part, error)
    Remember(ctx context.Context, parts []content.Part, opts ...RememberOption) error
    Forget(ctx context.Context, opts ...ForgetOption) error
}
```

语义：

- `Recall` 根据自然语言或结构化字符串查询返回 `content.Part`，调用方不关心底层是关键词、向量、图还是远程服务。`RecallOption` 由实现决定具体字段；建议形态包括 `WithLimit(n int)`、`WithFilter(map[string]any)`，未来可扩展 `WithNamespace` 等。
- `Remember` 接收已经归一化的 part 列表，由实现决定如何抽取、切片、索引和持久化。`RememberOption` 建议形态包括 `WithMetadata(map[string]any)`。
- `Forget` 删除已写入的长期上下文。`ForgetOption` 建议形态包括 `WithIDs(ids ...string)`、`WithFilter(map[string]any)`。**调用 `Forget` 必须至少提供 IDs 或 Filter 之一**；实现应在没有任何选项时返回错误，禁止无参全清以降低事故面。

`content.Part` 是 Memory 与 prompt/event/model 共享的多模态叶子协议。Memory 不返回 `model.Message`，避免长期上下文直接绑定模型历史形态。

为何 v2 引入 `Forget`：v1 仅提供读写主路径，应用要清理过期记录、撤回错误抽取或处理用户主动撤销时只能下钻到 `Store`，破坏抽象。v2 把删除提升为一等方法，与 `Recall` / `Remember` 对称。

## Entry 与 Store 可选后端抽象

`Store` 是 Memory 实现的内部扩展点。core 可以提供建议形态，但 Agent Loop 只依赖 `Memory`。

```go
type Entry struct {
    ID        string
    Parts     []content.Part
    Metadata  map[string]any
    CreatedAt time.Time
    UpdatedAt time.Time
}

type Query struct {
    Text     string
    Limit    int
    Metadata map[string]any
}

type Store interface {
    Put(ctx context.Context, entry *Entry) error
    Search(ctx context.Context, query Query, opts ...SearchOption) ([]*Entry, error)
    Delete(ctx context.Context, opts ...DeleteOption) error
}
```

命名说明：v2 把 v1 的 `Item` 重命名为 `Entry`。原因：

- 接口名已固定为 `Memory`（红线 r21），同包内无法再定义同名结构体复用 `Memory`。
- 候选 `Record` / `Memo` / `Fragment` 各自带数据库或 RAG 偏向；`Entry`「一条长期上下文条目」语义最贴合，与 Go 生态命名习惯一致。

实现示例：

- in-memory：适合测试和短生命周期 demo。
- SQLite：适合本地持久化、metadata 过滤和全文索引。
- vector database：适合语义检索。
- remote service：适合团队级共享 memory。

## 应用层注入策略

Memory 通过 prompt section 进入请求，可以在 section 端直接传入召回选项：

```go
memSection := prompt.Memory(
    mem,
    func(ctx context.Context) (string, error) {
        if s, ok := session.FromContext(ctx); ok {
            return "session " + s.ID(), nil
        }
        return "current task", nil
    },
    memory.WithLimit(8),
)

builder := prompt.New(
    prompt.System("Follow project instructions."),
    memSection,
)
```

Agent 构造不需要 `WithMemory` 之类 root option。不同应用可以在不同 section、不同 query、不同选项下注入 memory，而不影响 core Agent API。

## 异步抽取与遗忘边界

v2 core 仍不内置后台抽取或清理调度。推荐应用层流程：

1. Agent Loop 结束一个 turn，输出事件或通知。
2. 应用层 job 读取会话消息、工具结果或用户反馈。
3. job 生成需要长期保存的 `content.Part`。
4. job 调用 `mem.Remember(ctx, parts, opts...)`。
5. 当出现以下场景时调用 `mem.Forget`：
   - 用户主动撤回某条记忆或会话。
   - 应用基于 metadata（例如 `expires_at`）执行 TTL 清理。
   - 评估或人工纠错确认某条抽取错误。
6. 如需提示用户，应用通过自己的 notification/event bridge 回流。

这样 Memory 实现专注存取、检索与删除主路径；调度、频率控制、TTL 策略、用户确认和产品 UI 都留在应用层。

## 设计决策

1. **三方法接口**：`Recall` / `Remember` / `Forget` 覆盖读、写、删主路径。删除升格为一等公民，避免应用下钻 `Store`。
2. **全部 variadic option**：避免后续扩字段（如 namespace、TTL、过滤器）引入破坏性变更。
3. **Forget 必须显式范围**：调用必须至少提供 IDs 或 Filter 之一，无参调用应返回错误，杜绝误清空。
4. **不进入 root Agent**：Memory 是 prompt 上下文来源之一，不应扩大根构造参数。
5. **返回 part 而非消息**：长期上下文可以是文本、图片、结构化 blob 或 thinking 片段，不绑定某一轮对话。
6. **后端可选**：检索质量和存储形态由实现负责，core 只要求稳定接口；`Store` 与 `Memory` 对称提供 `Put` / `Search` / `Delete`。
7. **Entry 命名**：数据载体改名 `Entry`，避免泛化的 `Item` 与接口名 `Memory` 冲突。

## 与红线对照

- r21：`Memory` 固定为 `Recall` / `Remember` / `Forget` 三方法，全部使用 variadic option。
- r21：可选 `Store` 作为后端抽象，对称提供 `Put` / `Search` / `Delete`。
- r21：Memory 不进入 root Agent 配置，通过 `prompt.Memory` section 注入 recall 结果。
