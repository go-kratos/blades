---
type: design
title: Compact 上下文压缩
date: 2026-05-05
status: draft
parent: design-agent-framework.md
related: [design-agent-framework.md]
tags: [agentos, compact, context, token-budget]
---

# Compact 上下文压缩

## 概述

`compact/` 负责把历史 `[]*model.Message` 转换为仍满足 provider 不变量的较小消息列表。v1 只有一个核心接口：无状态纯函数 `Compactor`。

Compact 不依赖 root Agent、Provider、tools 或 memory。触发时机由 Agent Loop 根据 token budget、provider 错误或应用配置决定；Loop 只需要持有一个外部传入的 `Compactor`。

## Compactor 接口

```go
package compact

type Compactor interface {
    Compact(ctx context.Context, msgs []*model.Message) ([]*model.Message, error)
}
```

约束：

- 不原地修改调用方传入的消息和 part。
- 返回值仍是合法的 `model.Message` 序列。
- 保留 `Role` 语义：User、Assistant、Tool。
- 不向 `model.Message` 添加额外生命周期字段。
- 遵守 `content.Part` 的 sealed 边界。

## 内置实现

```go
func Window(n int) Compactor

func ToolResultBudget(budget int) Compactor

type SummarizeFunc func(ctx context.Context, msgs []*model.Message) (string, error)

func Summarize(summarizer SummarizeFunc) Compactor

func Chain(cs ...Compactor) Compactor
```

### Window

`Window(n)` 保留最近 `n` 条消息，并在内部调整边界，避免破坏工具调用与工具结果的相邻关系。`n <= 0` 应返回空消息列表或错误，具体由实现文档声明。

### ToolResultBudget

`ToolResultBudget(budget)` 限制工具结果 part 的大小。它只处理已经进入 `model.Message` 的内容，不导入 `tools/`，也不决定完整结果存放在哪里。完整结果可由应用层 artifact、session store 或工具运行时保存。

### Summarize

`Summarize` 用注入函数生成摘要，再用 provider 可接受的消息形式替代较旧历史。

```go
summarizer := func(ctx context.Context, msgs []*model.Message) (string, error) {
    return "summary text", nil
}

c := compact.Summarize(summarizer)
```

`compact/` 不持有模型客户端；是否调用 LLM、用哪个 provider、如何计费都由注入函数外部决定。

### Chain

`Chain` 顺序执行多个 `Compactor`，前一个输出作为后一个输入。

```go
c := compact.Chain(
    compact.ToolResultBudget(64 * 1024),
    compact.Window(80),
    compact.Summarize(summarizer),
)

msgs, err := c.Compact(ctx, original)
```

## provider invariant 保护

所有实现都必须在返回前验证 provider 需要的消息不变量。共享 helper 位于 `internal/convert`：

```go
func ValidateMessages(msgs []*model.Message) error
```

典型检查：

- 工具调用和工具结果的顺序合法。
- assistant 输出中的 thinking、text、tool 调用包装保持同一消息内的完整性。
- 裁剪后的首尾角色组合能被目标 provider 接受。
- system 内容不混入普通历史；系统上下文由 `model.Request.System` 承载。

不同 provider 的额外约束由 adapter 在转换时再次检查；compact 的目标是维护 core 级通用不变量。

## 触发时机

Agent Loop 决定何时调用 Compactor：

1. 构建请求前估算 token 超过软阈值。
2. provider 返回上下文过长错误后重试。
3. 工具结果过大，需要先预算化再进入下一轮。
4. 应用显式要求整理会话。

Compact 执行后，Loop 可以把结果传给 `session.Replace(ctx, msgs)` 持久化；是否覆盖原历史由应用配置决定。

## 设计决策

1. **单一接口**：所有压缩方式都表现为 `Compactor`，组合方式统一。
2. **纯函数边界**：输入消息到输出消息，便于测试、重试和审计。
3. **模型调用外置**：摘要函数注入，避免 compact 与 provider 形成依赖环。
4. **触发由 Loop 控制**：压缩实现不感知运行循环状态，只处理消息列表。

## 与红线对照

- r20：`Compactor` 单一接口；内置 `Window`、`ToolResultBudget`、`Summarize`、`Chain` 均返回 `Compactor`。
- r20：`SummarizeFunc func(ctx, msgs) (string, error)` 注入摘要能力。
- r20：provider invariant 由实现内部负责，并复用 `internal/convert.ValidateMessages`。
