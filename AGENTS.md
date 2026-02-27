# Repository Guidelines

## Project Structure & Module Organization
The root package contains the framework core (`agent.go`, `runner.go`, `message.go`, etc.). Feature areas are split into focused packages such as `flow/`, `graph/`, `memory/`, `middleware/`, `tools/`, `stream/`, and `evaluator/`.  
`contrib/` contains provider integrations (`openai`, `anthropic`, `gemini`, `mcp`, `otel`), each with its own `go.mod`.  
`examples/` is a separate module with runnable demos.  
`cmd/docs/` contains documentation tooling.  
`internal/` holds non-public implementation details.

## Build, Test, and Development Commands
Use the root `Makefile` (it discovers all `go.mod` directories and runs commands per module):

- `make tidy`: run `go mod tidy` in each module.
- `make build`: run `go build ./...` in each module.
- `make test`: run `go test -race ./...` in each module.
- `make all`: run tidy, build, and test.
- `make examples`: run a curated set of demos from `examples/`.

For targeted checks during development, run package tests directly, for example:  
`go test -race ./middleware -run TestConversationBuffered`

## Coding Style & Naming Conventions
Follow idiomatic Go and keep code `gofmt`-clean before commit. Use `PascalCase` for exported symbols, `camelCase` for internal identifiers, and short lowercase package names. Keep package scope tight and place new model/provider adapters under `contrib/<provider>`. Prefer established option-style APIs (`WithXxx`) for new configuration hooks.

## Testing Guidelines
Keep tests next to implementation files using `*_test.go`. Prefer table-driven tests, subtests (`t.Run`), and `t.Parallel()` when safe. Always run `make test` before opening a PR. No strict coverage threshold is defined; add tests for each behavior change and optionally verify with `go test -cover ./...`.

## Commit & Pull Request Guidelines
Recent history favors Conventional Commit style with scopes, e.g. `feat(flow): ...`, `fix(agent): ...`, `refactor(evaluator): ...`. Keep commits atomic and focused.  
PRs should include a clear problem statement, change summary, test evidence (commands run), and linked issue/proposal. For larger features, follow the project flow documented in issue templates: Feature Request -> Proposal -> Pull Request.
