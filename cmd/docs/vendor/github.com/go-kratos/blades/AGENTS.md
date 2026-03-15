# Repository Guidelines

Concise rules for contributing to this Go-based agent framework. Keep changes scoped, documented, and tested.

## Project Structure & Module Organization
- Root: core runtime and types in `agent.go`, `message.go`, `mime.go`, `model.go`, `stream.go`.
- `examples/` runnable demos: `chat/`, `output/`, `tools/`, `server/`, `routing/`, `streaming/`, `translation/`, `chain/`, `reasoning/`, `conversation/`, `template/` (run each via `go run examples/<name>/main.go`).
- `memory/` memory abstractions and helpers; `memory.go` entry types.
- `contrib/` provider integrations (e.g., `openai/` with `chat.go`, `image.go`).
- `flow/` flow orchestration utilities.
- `docs/` repository docs; `README.md` and `README_zh.md` at root.
- Tests live beside code as `*_test.go` (add next to source files).

## Build, Test, and Development Commands
- `go fmt ./...` formats the codebase.
- `go vet ./...` runs static analysis.
- `go build ./...` builds all packages.
- `go test ./... -race -cover` runs tests with race detector and coverage.
- `go run examples/chat/main.go` runs the chat demo (similar for other examples).

## Coding Style & Naming Conventions
- Idiomatic Go; code must be `gofmt`-clean and `go vet`-quiet.
- Package names: short, lowercase, no underscores; files grouped by feature (e.g., `message.go`).
- Exported APIs require brief doc comments.
- Wrap errors: `fmt.Errorf("context: %w", err)`; avoid panics in libraries.

## Testing Guidelines
- Use the standard `testing` package; prefer table-driven tests.
- Keep tests deterministic; avoid network calls and external services.
- Place tests next to code as `*_test.go`; aim for meaningful coverage.
- Run with `go test ./... -race -cover` locally.

## Commit & Pull Request Guidelines
- Conventional Commits: `feat(memory): ...`, `fix(contrib/openai): ...`, `docs: ...`.
- Subject ≤ 50 chars; body explains what and why.
- PRs include description, rationale, linked issues, tests, and updated examples/docs when APIs change.
- Call out breaking changes and provide migration notes.

## Security & Configuration Tips
- Do not commit secrets; use environment variables and keep `.env` untracked.
- Minimize new dependencies; discuss heavyweight additions in an issue.
- Document required provider vars in examples (e.g., OpenAI keys).

## Agent-Specific Notes
- This file applies repo-wide; nested `AGENTS.md` override locally.
- Keep changes minimal and focused; match existing style and structure.
