# Repository Guidelines

This guide helps contributors work efficiently on this Go-based agent framework. Keep changes scoped, documented, and tested.

## Project Structure & Module Organization
- `agent/` — core types and runtime (`Agent`, `Prompt`, `Message`, MIME).
- `models/` — model provider interface and providers (e.g., `openai/`).
- `memory/` — memory abstractions used by agents.
- `tools/` — tool-calling interfaces.
- `examples/` — runnable demos: `chat/`, `text/`, `output/`.
- `docs/`, `compose/` — documentation and container assets.
- Tests live beside code as `*_test.go` (e.g., `agent/message_test.go`).

## Build, Test, and Development Commands
- `go fmt ./...` — format code.
- `go vet ./...` — static checks.
- `go build ./...` — build all packages.
- `go run examples/chat/main.go` — run the chat example (similar: `text`, `output`).
- `go test ./... -race -cover` — run tests with race detector and coverage.
- Prereqs: Go (version per `go.mod`), Git; provider keys via env.

## Coding Style & Naming Conventions
- Idiomatic Go; code must be `gofmt`-clean.
- Packages: short, lowercase, no underscores (e.g., `agent`, `memory`).
- Files grouped by feature (e.g., `message.go`, `provider.go`).
- Exported APIs require brief doc comments.
- Prefer wrapped errors (`fmt.Errorf("context: %w", err)`); avoid panics in libraries.

## Testing Guidelines
- Use the standard `testing` package; prefer table-driven tests.
- Keep tests deterministic; avoid network calls in unit tests.
- Place tests next to code as `*_test.go`.
- Run `go test ./...` locally; target meaningful coverage.

## Commit & Pull Request Guidelines
- Conventional Commits: `feat(agent): ...`, `fix(models): ...`, `docs: ...`.
- Subject ≤ 50 chars; concise body explaining what/why.
- PRs include description, rationale, linked issues, tests, and example/docs updates when APIs change.
- Call out breaking changes and include migration notes.

## Security & Configuration Tips
- Do not commit secrets; use environment variables and `.env` ignored by Git.
- Minimize new dependencies; discuss heavyweight additions in an issue first.

## Agent-Specific Notes
- Agents should follow this file across the repo; nested `AGENTS.md` take precedence.
- Keep changes minimal and focused; match existing style and structure.

