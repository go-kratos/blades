---
type: design
title: Session 与持久化
date: 2026-05-05
status: draft
parent: design-agent-framework.md
related: [design-agent-framework.md]
tags: [agentos, session, persistence, checkpoint]
---

# Session 与持久化

## 概述

`session/` 定义 AgentOS core 的会话能力。v1 Session 是线性的 `model.Message` 列表，只负责消息读写、截断和替换；复杂分叉、对比实验、回放与版本管理由应用层用多个 Session 或 `Replace` 组合实现。

线性设计对齐 Anthropic、OpenAI 等主流 SDK 的上下文形态，也让 compact、prompt、policy 与 provider 转换边界更容易验证。

协议约束：

- `model.Message{Role, Parts []content.Part}` 不携带额外生命周期字段。
- `Role` 仅使用 User、Assistant、Tool。
- `content.Part` 是 sealed marker，公共叶子为 Text、Blob、Thinking。

## Session 接口

```go
package session

type Session interface {
    ID() string
    Append(ctx context.Context, msgs ...*model.Message) error
    Messages(ctx context.Context) ([]*model.Message, error)
    Truncate(ctx context.Context, n int) error
    Replace(ctx context.Context, msgs []*model.Message) error
}
```

方法语义：

- `ID` 返回会话稳定标识。
- `Append` 以可变参追加消息；同一次调用必须作为一个原子写入单元，避免 assistant/tool 成组消息被部分持久化。
- `Messages` 返回当前消息快照；调用方不得依赖返回 slice 可被原地修改。
- `Truncate` 保留前 `n` 条消息；`n` 小于 0 应返回错误，`n` 大于当前长度等价于无修改。
- `Replace` 用完整消息列表替换当前会话；用于 compact 后落盘、回放恢复、应用层版本切换等场景。

Session 不负责 token 预算、模型请求组装、工具执行或长期记忆召回。这些能力分别属于 `compact/`、Agent Loop、`tools/` 与 `memory/`。

## CheckpointSession 可选接口

检查点是可选能力，供持久化实现保存可恢复快照。

```go
type Checkpoint struct {
    ID        string
    CreatedAt time.Time
    Messages  []*model.Message
    Metadata  map[string]any
}

type CheckpointSession interface {
    Session
    SaveCheckpoint(ctx context.Context, name string) (*Checkpoint, error)
    LoadCheckpoint(ctx context.Context, id string) ([]*model.Message, error)
    ListCheckpoints(ctx context.Context) ([]*Checkpoint, error)
}
```

`LoadCheckpoint` 只读取快照；是否调用 `Replace` 恢复由应用层决定。这样检查点能力不会改变基础 Session 的线性语义。

## Store 可选后端抽象

`Store` 是实现层扩展点，不是 Agent Loop 的直接依赖。它用于把 Session 状态映射到 JSONL、SQLite、Redis 或远程存储。

```go
type Store interface {
    Open(ctx context.Context, id string) (Session, error)
    Delete(ctx context.Context, id string) error
    List(ctx context.Context) ([]string, error)
}
```

示例实现：

```go
jsonStore := jsonl.NewStore(".blades/sessions")
s, err := jsonStore.Open(ctx, "sess_123")
if err != nil {
    return err
}

err = s.Append(ctx,
    &model.Message{Role: model.RoleUser, Parts: []content.Part{content.Text{Text: "hi"}}},
    &model.Message{Role: model.RoleAssistant, Parts: []content.Part{content.Text{Text: "hello"}}},
)
```

- JSONL 适合本地调试和顺序追加。
- SQLite 适合索引、检索和多会话列表。
- Redis 适合短生命周期在线会话。

无论后端如何，`Append` 的批量原子性和 `Messages` 的快照语义必须一致。

## Context helper

Session 作为 capability 注入运行上下文，命名遵循 stdlib 风格。

```go
ctx = session.NewContext(ctx, s)

if s, ok := session.FromContext(ctx); ok {
    msgs, err := s.Messages(ctx)
    _ = msgs
    _ = err
}
```

该 helper 只承载当前 Session 引用，不负责创建、查找或恢复会话。

## 应用层 fork、A/B 与回放

v1 core 不提供非线性会话结构。常见需求由应用层组合完成：

- fork：读取源会话 `Messages`，创建新 Session，并 `Replace` 为同一快照。
- A/B：为每个实验分配独立 Session ID，比较最终输出或中间事件。
- 回放：从日志或检查点读取消息，再通过 `Replace` 恢复到目标 Session。
- 撤销：应用保存检查点 ID，必要时 `LoadCheckpoint` 后显式 `Replace`。

这种方式让 core 保持小接口，同时不限制产品层构建更复杂的历史视图。

## 设计决策

1. **线性消息列表**：模型 API 接收的就是有序消息，core 不引入额外导航模型。
2. **批量追加原子性**：一次模型轮次可能包含多条紧密相关消息，`Append(ctx, msgs...)` 保证不会只写入其中一部分。
3. **完整替换而非隐式变更**：compact、恢复和回放都通过 `Replace` 显式落地，便于审计和测试。
4. **后端可选**：内存、文件、数据库和远程服务都能实现同一 Session 语义。

## 与红线对照

- r18：Session 固定为 `ID`、`Append`、`Messages`、`Truncate`、`Replace` 五方法。
- r19：Context helper 使用 `session.NewContext` / `session.FromContext`。
- r29：会话是线性 `model.Message` 列表；fork、A/B、回放在应用层用多 Session 或 `Replace` 实现。
