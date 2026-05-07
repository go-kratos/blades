---
type: design
title: 流式协议最终态参考
date: 2026-05-05
status: draft
parent: design-agent-framework.md
related: [design-agent-framework.md]
tags: [agentos, streaming, model, event, performance]
---

# 流式协议最终态参考

## 1. 当前协议态

AgentOS v1 的流式协议以 Go generator 与 context 取消为核心：

```go
type Provider interface {
    Name() string
    Stream(ctx context.Context, req *model.Request) iter.Seq2[*model.Response, error]
    Count(ctx context.Context, req *model.Request) (int, error)
}
```

Provider 逐帧 yield `*model.Response`，错误通过序列第二返回值 yield。`model/` 直接使用 `iter.Seq2`，不依赖根包 `blades.Generator`，避免协议叶子反向依赖 runtime。

最终态约束：

- 没有单独的流式配置对象。
- 没有 provider 关闭方法。
- 没有独立 buffer 层。
- 资源回收唯一入口是 `ctx` 取消、deadline 或调用栈自然退出。
- 同步生成由 `Provider.Generate` 直接返回；`model.Collect` 仅作为 stream-only adapter 的兜底 helper。
- token 计数由独立的 `model.TokenCounter` 接口承担（按能力探测），不在 `Provider` 内强制。

调用方如果提前停止消费，必须取消 context：

```go
ctx, cancel := context.WithCancel(parent)
defer cancel()

for resp, err := range provider.Stream(ctx, req) {
    if err != nil {
        return err
    }
    if stopEarly(resp) {
        cancel()
        break
    }
}
```

Loop 将 provider 响应转换为用户协议输出：

```go
for resp, err := range provider.Stream(ctx, req) {
    if err != nil {
        yield(event.Error{Err: err}, nil)
        continue
    }
    for _, part := range resp.Delta {
        emitPart(part)
    }
}
```

fatal 错误通过根包 `Agent.Run` 返回的 generator yield error；运行期错误通过 `event.Error{Err error}` 进入输出流。

## 2. 背压与资源释放

1. `iter.Seq2` 已把生产与消费耦合为拉取式序列，天然提供消费侧节奏控制。
2. `context.Context` 覆盖取消、deadline 和资源释放信号。
3. provider adapter 持有底层 HTTP / SSE / WebSocket / SDK 句柄，使用 `defer` 在 `Stream` 范围内释放。
4. 协议层不暴露独立缓冲或控制对象；批量、节流、合并、UI 刷新频率控制由应用层或 middleware 包装 generator 完成。

## 3. 性能注意点

### 3.1 Event hot path

文本和思考是最高频流式输出，使用紧凑值类型：

```go
type TextDelta struct {
    Text string
}

type ThinkingDelta struct {
    Text      string
    Signature []byte
}
```

Loop 在识别到文本或思考增量时直接产出这些事件，避免额外 part 包装和接口装箱。

### 3.2 Event cold path

多模态、二进制、文件引用和其他低频内容走 part 生命周期事件：

```go
type PartStart struct {
    Index int
    Part  content.Part
}

type PartDelta struct {
    Index int
    Part  content.Part
}

type PartEnd struct {
    Index int
    Part  content.Part
}
```

cold path 允许携带 `content.Blob`、`content.Thinking` 或完整 part 快照。它与 hot path 不重叠，由 Loop 按模态选择输出路径。

### 3.3 Provider adapter 职责

Provider adapter 应：

- 尽快把底层流转换为 `model.Response`。
- 在 `ctx.Done()` 后停止读取并释放连接。
- 避免在协议层引入全局队列或长期 goroutine。
- 将 provider 原生停止原因映射到 `model.StopReason`。
- 将 token 用量写入 `model.Usage`。

### 3.4 应用层包装

应用可以在不改变核心协议的前提下包装流：

- 合并小文本片段以降低 UI 刷新频率。
- 按时间窗口刷新终端或 WebSocket。
- 记录审计日志或指标。
- 对慢消费者做应用层丢弃或降采样。

这些包装不得改变 `Provider.Stream`、`Agent.Run` 和 `event.Output` 的协议形状。

## 与红线对照

本文覆盖 r10、r12、r13、r25。
