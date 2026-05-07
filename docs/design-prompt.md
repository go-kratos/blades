---
type: design
title: Prompt 构建
date: 2026-05-05
status: draft
parent: design-agent-framework.md
related: [design-agent-framework.md]
tags: [agentos, prompt, context, system]
---

# Prompt 构建

## 概述

`prompt/` 负责把静态说明、动态上下文、系统文本和 memory recall 结果构造成有序 `content.Part`。v1 只保留两个核心抽象：`Builder` 与函数类型 `Section`。

Prompt 包不直接表示产品模式、工具排序或 provider 细节。Agent Loop 负责把 Builder 输出拆分到 `model.Request{System, Messages, Tools}`，并保证 `model.Request` 不包含 stream 开关。

## Builder 接口

```go
package prompt

type Builder interface {
    Build(ctx context.Context) ([]content.Part, error)
}
```

`Build` 返回可直接进入上下文构建流程的 part 序列。调用方应传入带取消、deadline、trace 和 typed capability 的 `context.Context`。

Builder 不缓存全局状态；缓存策略由 Section 实现、应用层或 provider 转换层决定。

## Section 函数类型

```go
type Section func(ctx context.Context) ([]content.Part, error)
```

`Section` 是函数类型，不是结构体，也不是接口。它足以表达静态文本、动态环境、memory recall、工具提示和应用层 mode 提示等输入，同时保持组合成本很低。

Section 必须返回 `[]content.Part`。`content.Part` 是 sealed marker，仅由 content 包定义公共叶子，如 Text、Blob、Thinking。

## 内置工厂

```go
func Static(parts ...content.Part) Section

func Dynamic(fn func(context.Context) ([]content.Part, error)) Section

func System(text string) Section

func Memory(mem memory.Memory, query func(context.Context) (string, error)) Section
```

语义：

- `Static` 返回固定 part 序列。
- `Dynamic` 在每次构建时执行函数，适合环境、时间、session 摘要和应用层状态。
- `System` 标记系统说明文本；Builder 输出时仍表现为 part，Agent Loop 在请求组装阶段提取到 `model.Request.System`。
- `Memory` 调用 `memory.Memory.Recall(ctx, query)`，把召回结果作为 Section 输出。

示例：

```go
b := prompt.New(
    prompt.System("You are a concise coding assistant."),
    prompt.Static(content.Text("Project: blades")),
    prompt.Dynamic(func(ctx context.Context) ([]content.Part, error) {
        s, ok := session.FromContext(ctx)
        if !ok {
            return nil, nil
        }
        return []content.Part{content.Text("Session: " + s.ID())}, nil
    }),
    prompt.Memory(mem, func(ctx context.Context) (string, error) {
        return "current task", nil
    }),
)

parts, err := b.Build(ctx)
```

## prompt.New 顺序拼接

默认 Builder 按传入顺序执行 Section，并按顺序拼接返回的 part。

```go
func New(sections ...Section) Builder
```

规则：

1. 前一个 Section 的输出先进入结果。
2. 任一 Section 返回错误，`Build` 立即返回该错误。
3. nil 或空输出被跳过。
4. Builder 不重排、不去重、不解释业务优先级。

需要优先级、条件开关或 profile 的应用可以在创建 `prompt.New` 前自行组织 Section 列表。

## System section 到 model.Request.System

Agent Loop 是 prompt 输出进入模型请求的边界。推荐流程：

```go
parts, err := builder.Build(ctx)
if err != nil {
    return err
}

req := &model.Request{
    System:   systemTextFrom(parts),
    Messages: messages,
    Tools:    toolSpecs,
}
```

`model.Request.System` 是单段 `string`，承载 provider-neutral 系统上下文。普通 part 进入消息或其他上下文位置的规则由 Loop 统一处理，避免 Section 直接依赖 provider adapter。

## cache control

Cache control 不进入 `model.Request` 顶层字段；走 `model.Request.Options` 中的 `CacheHint{Scope, TTL}` 表达，由 provider 转换层选择性解释（不支持的 provider 安全忽略）。

这样做的边界更清晰：

- `prompt/` 只生产 part。
- `model/` 表达 provider-neutral 请求结构；`Options` 承载可选 hint。
- contrib provider 把字段转换为 Anthropic、OpenAI、Gemini 等具体参数。

## 设计决策

1. **函数型 Section**：减少样板代码，便于应用层按需组合。
2. **系统文本作为工厂**：系统说明不需要独立接口，`System(text)` 足以表达。
3. **memory 通过 Section 注入**：Memory 不进入 root Agent 配置，prompt 是长期上下文进入请求的自然位置。
4. **缓存字段贴近协议对象**：cache control 属于 part 或 system block 的元信息，不需要额外抽象层。

## 与红线对照

- r24：`Builder.Build(ctx) ([]content.Part, error)` 与 `Section func(ctx) ([]content.Part, error)`。
- r21：Memory 通过 `prompt.Memory(mem, query)` 注入 recall 结果，不进入 root Agent 配置。
- r1-r3：遵守 `content.Part` sealed marker、`model.Message` 与 `model.Request` v1 协议形态。
