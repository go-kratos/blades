---
type: design
title: Memory 长期上下文
date: 2026-05-05
status: draft
parent: design-agent-framework.md
related: [design-agent-framework.md]
tags: [agentos, memory, prompt, context]
---

# Memory 长期上下文

## 概述

`memory/` 为 AgentOS 提供跨 turn、跨 session 或跨应用的长期上下文能力。v1 core 只定义 `Recall` 与 `Remember` 两个方法；文件布局、向量检索、自动抽取、workspace 遍历和后台调度都属于具体实现或应用层。

Memory 不进入 root Agent 配置。应用通过 `prompt.Memory(mem, query)` 把 recall 结果作为 prompt section 注入模型请求。

## Memory 接口

```go
package memory

type Memory interface {
    Recall(ctx context.Context, query string) ([]content.Part, error)
    Remember(ctx context.Context, parts []content.Part) error
}
```

语义：

- `Recall` 根据自然语言或结构化字符串查询返回 `content.Part`，调用方不关心底层是关键词、向量、图还是远程服务。
- `Remember` 接收已经归一化的 part 列表，由实现决定如何抽取、切片、索引和持久化。

`content.Part` 是 Memory 与 prompt/event/model 共享的多模态叶子协议。Memory 不返回 `model.Message`，避免长期上下文直接绑定模型历史形态。

## Store 可选后端抽象

`Store` 是 Memory 实现的内部扩展点。core 可以提供建议形态，但 Agent Loop 只依赖 `Memory`。

```go
type Item struct {
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
    Put(ctx context.Context, item *Item) error
    Search(ctx context.Context, query Query) ([]*Item, error)
    Delete(ctx context.Context, id string) error
}
```

实现示例：

- in-memory：适合测试和短生命周期 demo。
- SQLite：适合本地持久化、metadata 过滤和全文索引。
- vector database：适合语义检索。
- remote service：适合团队级共享 memory。

## 应用层注入策略

Memory 通过 prompt section 进入请求：

```go
memSection := prompt.Memory(mem, func(ctx context.Context) (string, error) {
    if s, ok := session.FromContext(ctx); ok {
        return "session " + s.ID(), nil
    }
    return "current task", nil
})

builder := prompt.New(
    prompt.System("Follow project instructions."),
    memSection,
)
```

Agent 构造不需要 `WithMemory` 之类 root option。不同应用可以在不同 section、不同 query 和不同运行阶段注入 memory，而不影响 core Agent API。

## 异步抽取边界

v1 core 不内置后台抽取调度。推荐应用层流程：

1. Agent Loop 结束一个 turn，输出事件或通知。
2. 应用层 job 读取会话消息、工具结果或用户反馈。
3. job 生成需要长期保存的 `content.Part`。
4. job 调用 `mem.Remember(ctx, parts)`。
5. 如需提示用户，应用通过自己的 notification/event bridge 回流。

这样 Memory 实现专注存取和检索；调度、频率控制、用户确认和产品 UI 都留在应用层。

## 设计决策

1. **双方法接口**：`Recall` 和 `Remember` 覆盖读取与写入主路径，避免把抽取流程拆成多个 core 抽象。
2. **不进入 root Agent**：Memory 是 prompt 上下文来源之一，不应扩大根构造参数。
3. **返回 part 而非消息**：长期上下文可以是文本、图片、结构化 blob 或 thinking 片段，不绑定某一轮对话。
4. **后端可选**：检索质量和存储形态由实现负责，core 只要求稳定接口。

## 与红线对照

- r21：`Memory` 固定为 `Recall(ctx, query)` 与 `Remember(ctx, parts)` 两方法。
- r21：可选 `Store` 只作为后端抽象。
- r21：Memory 不进入 root Agent 配置，通过 `prompt.Memory` section 注入 recall 结果。
