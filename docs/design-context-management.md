---
type: design
title: Context Management
date: 2026-05-16
status: draft
parent: design-agent-framework.md
related: [design-event-agent-loop.md, design-compact.md, design-memory.md, design-prompt.md]
tags: [agentos, context, compact, memory, budget]
---

# Context Management

## Overview

Context management is part of the root `blades` default Agent runtime. It is the request-view assembly layer between `session`, `compact`, `prompt`, `memory`, `tools`, and `model.Request`. It does not own long-term state and does not replace those packages. Its job is to build one model-call view, expose budget intent while building it, and report segmented token stats after it is assembled.

The default Agent Loop uses a private root `contextBuilder` for each model step. Applications configure budget coordination through:

```go
blades.WithContextBudget(blades.ContextBudget{
    InputTokens:           148_000,
    SystemTokens:          12_000,
    MessagesTokens:        120_000,
    ToolTokens:            8_000,
    ResponseReserveTokens: 8_000,
})
blades.WithTokenCounter(counter)
```

If no counter is provided explicitly, the default Agent falls back to `model.ApproxTokenCounter`. The selected counter is passed explicitly to compactors through `compact.Request.TokenCounter`; it is not discovered from `model.Provider` and is not stored in `context.Context`. `ResponseReserveTokens` is advisory and does not require separate enforcement.

## API

Root API:

```go
type ContextPurpose string

const (
    ContextPurposeMain ContextPurpose = "main"
)

type ContextBudget struct {
    InputTokens           int64
    SystemTokens          int64
    MessagesTokens        int64
    ToolTokens            int64
    ResponseReserveTokens int64
}

type ContextInfo struct {
    Purpose ContextPurpose
    Budget  ContextBudget
}

type ContextStats struct {
    Purpose        ContextPurpose
    Budget         ContextBudget
    Count          model.TokenCount
    MessagesBefore int
    MessagesAfter  int
}
```

`blades.ContextInfoFromContext` exposes `ContextInfo` to prompt builders while a request is being assembled. `blades.ContextStatsFromContext` exposes the final `ContextStats` to model hooks and provider calls.

Request token counting belongs to `model/`:

```go
type TokenCounter interface {
    CountTokens(ctx context.Context, req *Request) (TokenCount, error)
}

type TokenCount struct {
    InputTokens    int64
    SystemTokens   int64
    MessagesTokens int64
    ToolTokens     int64
    HasBreakdown   bool
}
```

The counter is deliberately request-view oriented rather than message-only, because system prompt, memory recall, and tool declarations live outside `[]*model.Message`. It is model-owned because it describes provider-facing request accounting, not a compact-only message view. When compact only needs message accounting, it calls the same counter with `model.Request{Messages: msgs}`.

## Pipeline

The private root context builder assembles requests in a fixed order:

1. Read `Session.Messages(ctx)` as the full append-only transcript.
2. Call `Compactor.Compact(ctx, compact.Request{Messages: snapshot, TokenCounter: counter})` on the messages segment only.
3. Build prompt sections into `model.Request.System`; `prompt.Memory` recall happens here.
4. Render tool specs into `model.Request.Tools`.
5. Count tokens and enforce configured segment budgets.
6. Return `*model.Request` and pre-hook `ContextStats`.

The default loop attaches pre-hook `ContextStats` to the context used for `BeforeModel`. Since `BeforeModel` hooks may mutate `*model.Request`, the loop recomputes stats and enforces budget after all `BeforeModel` hooks finish; the provider call and `AfterModel` receive the final stats.

`InputTokens` can be enforced whenever the counter returns total input usage. Segment budgets (`SystemTokens`, `MessagesTokens`, `ToolTokens`) require `TokenCount.HasBreakdown`; otherwise enforcement fails with `ContextBudgetError{Unavailable: true}` instead of silently accepting an unchecked budget.

## Memory And Compact

Memory and compact remain decoupled:

- Memory recall enters `Request.System` through prompt sections.
- Compact transforms only `Request.Messages`.
- Root context management coordinates budget visibility and stats, but it does not let compact inspect or trim memory.

Applications that need adaptive memory recall should read `blades.ContextInfoFromContext` in the memory query function and choose `memory.Query.Limit` or backend-specific filters accordingly. If system memory exceeds `ContextBudget.SystemTokens`, the root context builder returns `ContextBudgetError`; it does not silently drop memory entries.

## Summary Requests

Summary generation is treated as an internal model request, not an Agent fork. `compact.NewModelSummarizer(provider, ...)` calls `model.Provider.Generate` directly, with no tools, no Agent loop, no session writes, and no compact recursion.

This replaces the older root-level `ForkSummarizer` shape. Use:

```go
compact.NewBlockSummarize(
    compact.WithSummarizer(compact.NewModelSummarizer(summaryProvider)),
)
```

## Error Semantics

Budget enforcement is fail-fast and runs after `BeforeModel` mutations so the checked request matches the provider request. `ContextBudgetError` reports the segment, limit, actual estimate when available, and whether segment breakdown was unavailable. The Agent Loop surfaces that error through the normal `event.TurnEnd.Err` / `event.Error` path.

`ResponseReserveTokens` is advisory context for builders and counters. It is not enforced by the root context builder because provider-specific total windows and output reservation policies belong to the application or provider integration.
