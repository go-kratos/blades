---
type: design
title: Context Management
date: 2026-05-16
status: current
parent: design-agent-framework.md
related: [design-event-agent-loop.md, design-compact.md, design-memory.md, design-prompt.md]
tags: [agentos, context, compact, memory, budget]
---

# Context Management

## Overview

Context management is part of the root `blades` default Agent runtime. It is the request-view assembly layer between `session`, `compact`, `prompt`, `memory`, `tools`, and `model.Request`. It does not own long-term state and does not replace those packages. Its job is to build one model-call view, expose budget intent while building it, and report segmented token stats after it is assembled.

The default Agent Loop uses a private root `contextBuilder` for each model step. Applications configure budget coordination through:

```go
blades.WithContextBudget(model.TokenCount{
    Input:    148_000,
    System:   12_000,
    Messages: 120_000,
    Tools:    8_000,
})
blades.WithTokenCounter(counter)
```

If no counter is provided explicitly, the default Agent falls back to `model.ApproxTokenCounter`. The selected counter is passed explicitly to compactors through `compact.Request.TokenCounter`; it is not discovered from `model.Provider` and is not stored in `context.Context`.

## API

Root API — single unified context value:

```go
type ContextWindow struct {
    Budget         model.TokenCount  // 上限（零值 = 不限制）
    Usage          model.TokenCount  // 实际计数
    MessagesBefore int               // compaction 前消息数
    MessagesAfter  int               // compaction 后消息数
}

// Enforce checks that Usage does not exceed any non-zero Budget limit.
func (w ContextWindow) Enforce() error

type BudgetError struct {
    Segment     string
    Limit       int64
    Actual      int64
    Unavailable bool
}

// ContextWindowFrom retrieves the context window from ctx.
func ContextWindowFrom(ctx context.Context) (ContextWindow, bool)
```

Request token counting belongs to `model/`:

```go
type TokenCounter interface {
    CountTokens(ctx context.Context, req *Request) (TokenCount, error)
}

type TokenCount struct {
    Input    int64
    System   int64
    Messages int64
    Tools    int64
}

// Total returns Input if set, otherwise the sum of sub-segments.
func (c TokenCount) Total() int64

// HasSegments reports whether per-segment breakdown is available.
func (c TokenCount) HasSegments() bool
```

The counter is deliberately request-view oriented rather than message-only, because system prompt, memory recall, and tool declarations live outside `[]*model.Message`. It is model-owned because it describes provider-facing request accounting, not a compact-only message view. When compact only needs message accounting, it calls the same counter with `model.Request{Messages: msgs}`.

## Pipeline

The private root context builder assembles requests in a fixed order:

1. Create `ContextWindow{Budget: agent.contextBudget}` and inject into context.
2. Read `Session.Messages(ctx)` as the full append-only transcript.
3. Call `Compactor.Compact(ctx, compact.Request{Messages: snapshot, TokenCounter: counter})` on the messages segment only.
4. Build prompt sections into `model.Request.System`; `prompt.Memory` recall happens here.
5. Render tool specs into `model.Request.Tools`.
6. Count tokens via `TokenCounter.CountTokens(ctx, req)` and populate `ContextWindow.Usage`.
7. Call `ContextWindow.Enforce()` to check budget limits.
8. Return `*model.Request` and `ContextWindow`.

The default loop attaches `ContextWindow` to the context used for `BeforeModel`. Since `BeforeModel` hooks may mutate `*model.Request`, the loop recomputes stats and enforces budget after all `BeforeModel` hooks finish; the provider call and `AfterModel` receive the final stats.

`Input` can be enforced whenever the counter returns total input usage. Segment budgets (`System`, `Messages`, `Tools`) require `TokenCount.HasSegments()`; otherwise enforcement fails with `BudgetError{Unavailable: true}` instead of silently accepting an unchecked budget.

## Design Principles

### One Concept, One Type

`ContextWindow` unifies budget configuration and runtime observation:
- **Budget** is the upper limit (zero = no limit)
- **Usage** is the actual measured count
- Both use the same `model.TokenCount` shape

This eliminates the redundancy between the old `ContextBudget` and `model.TokenCount` structs.

### Single Context Value

Instead of separate `ContextInfo` (pre-assembly) and `ContextStats` (post-assembly) context values, there is one `ContextWindow` that progressively enriches:
- Build starts: `Budget` is set, `Usage` is zero
- After counting: `Usage` is populated
- Hooks and provider see the complete `ContextWindow`

### Extensible Enforcement

The `Enforce()` method iterates over all segments uniformly. Adding a new segment requires only:
1. Add a field to `model.TokenCount`
2. Add a check case in `Enforce()`

No manual field-by-field comparison code.

## Memory And Compact

Memory and compact remain decoupled:

- Memory recall enters `Request.System` through prompt sections.
- Compact transforms only `Request.Messages`.
- Root context management coordinates budget visibility and stats, but it does not let compact inspect or trim memory.

Applications that need adaptive memory recall should read `blades.ContextWindowFrom(ctx)` in the memory query function and choose `memory.Query.Limit` or backend-specific filters accordingly. If system memory exceeds `ContextWindow.Budget.System`, the root context builder returns `BudgetError`; it does not silently drop memory entries.

## Summary Requests

Summary generation is treated as an internal model request, not an Agent fork. `compact.NewModelSummarizer(provider, ...)` calls `model.Provider.Generate` directly, with no tools, no Agent loop, no session writes, and no compact recursion.

This replaces the older root-level `ForkSummarizer` shape. Use:

```go
compact.NewBlockSummarize(
    compact.WithSummarizer(compact.NewModelSummarizer(summaryProvider)),
)
```

## Error Semantics

Budget enforcement is fail-fast and runs after `BeforeModel` mutations so the checked request matches the provider request. `BudgetError` reports the segment, limit, actual estimate when available, and whether segment breakdown was unavailable. The Agent Loop surfaces that error through the normal `event.TurnEnd.Err` / `event.Error` path.

