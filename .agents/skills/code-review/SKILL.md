---
name: code-review
description: Review Blades pull requests for correctness, Go quality, tests, documentation, security, and performance.
---

# Code Review

Use this skill for GitHub Actions PR review in the Blades repository.

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

## Blades-Specific Review Rules

- Go code must be idiomatic and `gofmt` clean.
- Public APIs should preserve compatibility unless the PR clearly documents a breaking change.
- Context-aware operations should respect `context.Context` cancellation and deadlines.
- Streaming, runner, graph, flow, session, memory, and tool execution code must avoid data races, leaked goroutines, blocked sends, unclosed streams, and duplicate tool/result handling.
- Skills must keep `SKILL.md` frontmatter valid: lowercase kebab-case name, non-empty description, and resource files under `references/`, `assets/`, or `scripts/`.
- Recipe changes must preserve validation errors for duplicate names, invalid execution modes, missing tools, and bad parameter wiring.
- Provider integrations under `contrib/` should keep provider-specific behavior isolated and should not weaken core abstractions.
- CLI changes under `cmd/blades` should preserve workspace/home separation, never overwrite user files during init, and keep destructive commands explicit.
- Middleware and retry logic should not retry non-idempotent actions silently.
- Tests should use table-driven cases and `t.Run` where it improves clarity. Use `t.Parallel()` only when the test has no shared mutable state or global environment coupling.

## Security Review Focus

- Do not expose API keys, tokens, environment secrets, or private file contents in logs or errors.
- Watch for command injection, path traversal, unsafe workspace escapes, and accidental writes outside configured roots.
- Validate external inputs from config, YAML recipes, model/provider responses, MCP servers, and channel integrations.
- Prefer least-privilege behavior for tools and GitHub automation.

## Performance Review Focus

- Flag unbounded memory growth in conversation history, summaries, streams, and logs.
- Flag O(n^2) behavior only when input size can plausibly grow enough to matter.
- Watch for repeated filesystem scans, repeated model/tool registry construction, and avoidable blocking in streaming paths.

## Documentation Review Focus

- Public API changes should update README, package docs, examples, or comments when users would otherwise be misled.
- Examples should compile or be clearly marked as illustrative.
- Do not require docs for internal-only refactors unless behavior changed.

## Output

Post only the review comments that meet the findings standard. Keep wording direct and concise.
