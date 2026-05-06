---
name: code-review
description: Review pull requests for correctness, maintainability, tests, documentation, security, and performance.
---

# Code Review

Use this skill for GitHub PR review in any repository.

## Goal

Provide concise, actionable PR feedback. Prioritize bugs, regressions, security issues, data loss, race conditions, API compatibility problems, and missing tests for changed behavior.

## Review Workflow

1. Read the PR title, description, changed files, and diff.
2. Read relevant nearby code and tests before commenting.
3. Run targeted checks only when useful and allowed by the workflow.
4. Leave inline comments for specific changed lines. Use a top-level comment only for summary or cross-file concerns.
5. Do not modify files, commit, or push.

## Findings Standard

- Comment only on issues that are likely to matter before merge.
- Include file and line evidence.
- Explain the concrete failure mode.
- Prefer one precise fix direction over broad advice.
- Do not request tests that only duplicate existing coverage. Ask for tests when a distinct behavior, edge case, regression, public API contract, race, or failure path changed.
- If there are no actionable findings, say that no blocking issues were found and mention any important test gap.

## General Review Rules

- Code should follow the repository's established language idioms, formatting, naming, and local patterns.
- Public APIs should preserve compatibility unless the PR clearly documents a breaking change.
- Cancellable, async, or long-running operations should respect the repository's cancellation, timeout, and cleanup conventions.
- Concurrent, streaming, session, queue, cache, and background worker code must avoid data races, leaked tasks, blocked sends, unclosed streams, and duplicate result handling.
- Configuration, recipe, manifest, or plugin changes must preserve validation for duplicate names, invalid modes, missing dependencies, and bad parameter wiring.
- Integration or adapter code should keep provider-specific behavior isolated and should not weaken shared abstractions.
- CLI or file-writing changes should preserve workspace/home separation, avoid overwriting user files unexpectedly, and keep destructive commands explicit.
- Middleware and retry logic should not retry non-idempotent actions silently.
- Tests should follow repository conventions and isolate shared mutable state, global environment changes, filesystem writes, network calls, and timing-sensitive behavior.

## Security Review Focus

- Do not expose API keys, tokens, environment secrets, or private file contents in logs or errors.
- Watch for command injection, path traversal, unsafe workspace escapes, and accidental writes outside configured roots.
- Validate external inputs from configuration files, manifests, network responses, provider or plugin responses, and integration boundaries.
- Prefer least-privilege behavior for tools and GitHub automation.

## Performance Review Focus

- Flag unbounded memory growth in accumulated state, caches, histories, streams, queues, and logs.
- Flag O(n^2) behavior only when input size can plausibly grow enough to matter.
- Watch for repeated filesystem scans, repeated expensive initialization, unnecessary network calls, and avoidable blocking in streaming or request paths.

## Documentation Review Focus

- Public API changes should update README, API docs, examples, or comments when users would otherwise be misled.
- Examples should compile or be clearly marked as illustrative.
- Do not require docs for internal-only refactors unless behavior changed.

## Output

Post only the review comments that meet the findings standard. Keep wording direct and concise.
