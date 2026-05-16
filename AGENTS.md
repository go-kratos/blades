# Repository Guidelines

## Project Structure & Module Organization

Blades is an event-driven multimodal Agent framework. The root `blades` package is the small public entrypoint: `Agent`, `NewAgent`, agent options, `Runner`, root errors, and the Agent-to-tool adapter. The default LLM runtime is implemented privately in the root package (`agent_loop.go`, `agent_turn.go`, etc.); do not add a public loop package unless the architecture docs are updated and reviewed.

Protocol packages are split by boundary:

- `content/`: the shared multimodal `Part` union used by events, model messages, and tool results.
- `event/`: user-facing `Input` / `Output` events, including prompts, steering, aborts, streaming deltas, tool lifecycle events, turn endings, runtime errors, and `Done`.
- `model/`: provider-facing requests, messages, responses, chunks, usage, options, and provider interfaces.
- `internal/convert/`: the private Event <-> Message conversion boundary.

Capability and runtime extension packages are focused by concern:

- `tools/`: tool specs, tool execution interface, resolver/filter helpers, and tool context.
- `policy/`: tool invocation policy decisions.
- `hook/`: lifecycle hooks around turns, model calls, and tool calls.
- `prompt/`: system prompt builders and sections.
- `compact/`: context compaction strategies.
- `session/`: append-only model message history and context helpers.
- `memory/`: recall/remember/forget abstractions.
- `flow/`: Agent composition primitives such as sequential, parallel, loop, routing, and deep flows.

Provider and integration code belongs under `contrib/<name>/`. Current integrations include `openai`, `anthropic`, `gemini`, `mcp`, and `otel`, each with its own `go.mod`. Keep provider-specific dependencies inside the matching contrib module; the root module should remain provider-agnostic.

`docs/` contains design, reference, and development process documents. `cmd/docs/` contains documentation tooling. `tests/` contains integration-style tests and fixtures such as `dummyprovider` and `testtools`. `internal/` is non-public implementation detail and must not be imported by external modules.

## Build, Test, and Development Commands

Use the root `Makefile` for full-repo checks. It discovers every `go.mod` and runs commands per module.

- `make tidy`: run `go mod tidy` in all modules.
- `make build`: run `go build ./...` in all modules.
- `make test`: run `go test -race ./...` in all modules.
- `make all`: run tidy, build, and test.
- `make examples`: run curated examples when an `examples/` directory is present; otherwise it exits cleanly.

For targeted development, run package tests directly:

```sh
go test -race ./event -run TestTurnEndText
go test -race ./tests/agent -run TestLLMAgent
(cd contrib/openai && go test -race ./...)
```

## Architecture Rules

Keep package boundaries explicit. `content.Part` is the only shared part union. `event` owns user protocol events. `model` owns provider protocol types. `tools.ToolSpec` is the single tool schema definition used by model requests. Event/message conversion stays in `internal/convert/`; do not duplicate conversion logic in contrib packages or application code.

The root package should stay minimal. Add configuration through `WithXxx` options when extending `NewAgent`. Advanced behavior should usually plug into `tools`, `policy`, `hook`, `prompt`, `compact`, `session`, or `flow` instead of broadening the root API.

Use `context.Context` for cancellation, deadlines, tracing, and small runtime-scoped capabilities only. Context helpers should follow the existing `pkg.NewContext(ctx, value)` / `pkg.FromContext(ctx)` style. Application identifiers such as user IDs, workspace IDs, channel IDs, and platform metadata should remain application-owned context keys, not core helpers.

Provider adapters should implement `model.Provider` and translate provider-specific request/response details at the edge. Do not leak provider SDK types into core protocol packages.

## Coding Style & Naming Conventions

Follow idiomatic Go and keep all files `gofmt`-clean. Use short lowercase package names, `PascalCase` for exported identifiers, and `camelCase` for internal identifiers. Prefer small interfaces, table-driven logic, clear error wrapping, and option-style configuration.

Respect sealed protocol types in their owning packages. Do not create parallel event, part, or option unions outside `event/`, `content/`, or `model/`. Keep comments useful for exported API documentation and non-obvious runtime behavior; avoid comments that restate simple code.

When adding public API, update relevant README or design docs when the behavior is not self-evident from tests. For larger changes, follow `docs/DEVELOPMENT_GUIDE.md` and update `docs/INDEX.md`.

## Testing Guidelines

Place focused unit tests next to implementation files as `*_test.go`. Use table-driven tests, subtests (`t.Run`), and `t.Parallel()` when the tested code has no shared mutable state. Use `tests/dummyprovider` and `tests/testtools` for agent-loop behavior instead of relying on real provider APIs.

Run targeted tests while iterating, then run `make test` before opening a PR or handing off broad changes. Add tests for every behavior change, especially around event ordering, tool execution, session commits, provider translation, policy decisions, hooks, and compaction.

## Documentation Guidelines

Important architecture and workflow changes should be reflected in `docs/`. Use the templates in `docs/templates/` for new design, feature, reference, decision, or architecture documents. Include the standard frontmatter described in `docs/DEVELOPMENT_GUIDE.md`, keep documents concise, and add new documents to `docs/INDEX.md`.

Treat existing design docs as the source of intent for ongoing refactors, but verify the current code before editing because some docs are drafts and may describe target architecture rather than implemented behavior.

## Commit & Pull Request Guidelines

Use focused Conventional Commit style with scopes, matching recent history: `feat(event): ...`, `fix(agent): ...`, `refactor(session): ...`, `chore: ...`.

PRs should include the problem statement, change summary, test evidence, and any doc updates. Link the relevant issue or proposal for larger features. Keep commits atomic and avoid mixing API changes, mechanical refactors, and documentation rewrites unless they are part of the same reviewed change.
