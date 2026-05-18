---
type: design
title: Context Management
date: 2026-05-18
status: current
parent: design-agent-framework.md
related: [design-event-agent-loop.md, design-compact.md, design-memory.md, design-prompt.md]
tags: [agentos, context, compact, memory, budget]
---

# Context Management

## Overview

Context management is part of the root `blades` default Agent runtime. It is the request-view assembly layer between `session`, `compact`, `prompt`, `memory`, `tools`, and `model.Request`. It does not own long-term state and does not replace those packages. Its job is to build one model-call view and enforce budget limits.

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

Budget enforcement is internal to `contextBuilder`. The public API is:

```go
// BudgetError reports that an assembled request segment exceeds its budget.
type BudgetError struct {
    Segment     string
    Limit       int64
    Actual      int64
    Unavailable bool
}

func (e *BudgetError) Error() string
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

1. Read `Session.Messages(ctx)` as the full append-only transcript.
2. Call `Compactor.Compact(ctx, compact.Request{Messages: snapshot, TokenCounter: counter})` on the messages segment only.
3. Build prompt sections into `model.Request.System`; `prompt.Memory` recall happens here.
4. Render tool specs into `model.Request.Tools`.
5. Assemble `model.Request` and check budget limits.
6. Return `*model.Request` or `BudgetError`.

The default loop calls `contextBuilder.Build()` to get the request. Since `BeforeModel` hooks may mutate `*model.Request`, the loop re-checks budget after all `BeforeModel` hooks finish; the provider call and `AfterModel` receive the final request.

`Input` can be enforced whenever the counter returns total input usage. Segment budgets (`System`, `Messages`, `Tools`) require `TokenCount.HasSegments()`; otherwise enforcement fails with `BudgetError{Unavailable: true}` instead of silently accepting an unchecked budget.

## Design Principles

### Budget Enforcement is Internal

`contextBuilder` is a private type. Budget enforcement happens during `Build()` and again after `BeforeModel` hooks. The result is either a valid `*model.Request` or a `BudgetError`. Hooks and the provider never see an invalid request.

### Compact and Agent Budget are Independent

- **Compact budget** (`compact.WithMessagesBudget(n)`) is a soft trigger: "start summarizing when messages exceed N tokens"
- **Agent budget** (`blades.WithContextBudget(tc)`) is a hard limit: "reject any request that exceeds these limits"

These serve different purposes and are configured separately. Compact has its own internal budget logic; Agent budget is enforced at the request assembly boundary.

### Session is the Single Message Store

- `contextBuilder` reads from `session.Messages(ctx)`
- `Compactor` transforms messages but does not write to session
- `agentLoop.commitStep()` writes the final turn to session
- This keeps session as the append-only source of truth

### No Context Value Injection

Budget enforcement does not inject `ContextWindow` into context. Hooks receive `*model.Request` directly, which contains all the information they need. This simplifies the API and avoids unnecessary context pollution.

## Error Semantics

Budget enforcement is fail-fast and runs after `BeforeModel` mutations so the checked request matches the provider request. `BudgetError` reports the segment, limit, actual estimate when available, and whether segment breakdown was unavailable. The Agent Loop surfaces that error through the normal `event.TurnEnd.Err` / `event.Error` path.

## Summary Requests

Summary generation is treated as an internal model request, not an Agent fork. `compact.NewModelSummarizer(provider, ...)` calls `model.Provider.Generate` directly, with no tools, no Agent loop, no session writes, and no compact recursion.

This replaces the older root-level `ForkSummarizer` shape. Use:

```go
compact.NewBlockSummarize(
    compact.WithSummarizer(compact.NewModelSummarizer(summaryProvider)),
)
```

