# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build, tidy, and test all modules
make all

# Individual targets
make tidy    # go mod tidy in each module
make build   # go build ./... in each module
make test    # go test -race ./... in each module

# Run a specific test (in any module directory)
go test -v -run TestName ./...

# Run tests in a specific contrib module
cd contrib/openai && go test -race ./...
```

## Module Structure

This repo contains multiple independent Go modules, each with its own `go.mod`:

| Module | Path |
|--------|------|
| `github.com/go-kratos/blades` | `.` (root) |
| `github.com/go-kratos/blades/contrib/anthropic` | `contrib/anthropic/` |
| `github.com/go-kratos/blades/contrib/openai` | `contrib/openai/` |
| `github.com/go-kratos/blades/contrib/gemini` | `contrib/gemini/` |
| `github.com/go-kratos/blades/contrib/mcp` | `contrib/mcp/` |
| `github.com/go-kratos/blades/contrib/otel` | `contrib/otel/` |
| examples | `examples/` |

The `contrib/` providers are **separate modules** — changes to the root module interfaces require updating each contrib module independently.

## Architecture

Blades is a multimodal AI agent framework for Go built on Go 1.24 iterators (`iter.Seq2`).

### Core Interfaces (root package)

```go
// Agent — the primary execution interface
type Agent interface {
    Name() string
    Description() string
    Run(context.Context, *Invocation) Generator[*Message, error]
}

// Generator is iter.Seq2 — range over it to consume messages
type Generator[T, E any] = iter.Seq2[T, E]

// ModelProvider — pluggable LLM backend
type ModelProvider interface {
    Name() string
    Generate(context.Context, *ModelRequest) (*ModelResponse, error)
    NewStreaming(context.Context, *ModelRequest) Generator[*ModelResponse, error]
}

// Middleware — onion-model cross-cutting concerns
type Middleware func(Handler) Handler
```

### Invocation Loop

`Runner` → builds `*Invocation` → calls `Agent.Run()` → `agent.handle()` iterates up to `maxIterations` (default 10):
1. Call `ModelProvider.Generate` or `NewStreaming`
2. If response has `RoleTool` parts, execute all tools in parallel via `errgroup`
3. Append tool response to message history, loop back to step 1
4. If response has `RoleAssistant` with `StatusCompleted`, yield and return

Streaming vs non-streaming is toggled via `Invocation.Stream`; the `Runner.RunStream` path sets it to `true`.

### Message & Parts

`Message` is the fundamental unit. A message has a `Role` (User/System/Assistant/Tool), `Status` (Streaming/Completed), and `Parts` — a slice of `any` that can be:
- `TextPart` — text content
- `FilePart` — file reference with MIME type
- `DataPart` — structured data
- `ToolPart` — tool call with `Name`, `Request`, `Response`, `Completed`

### Key Packages

- **`flow/`** — Agent compositions: `Sequential`, `Parallel`, `Routing`, `Loop`, `Deep`
- **`graph/`** — DAG execution with conditional edges, checkpointing, retry, and state
- **`recipe/`** — Declarative YAML-based workflow definitions parsed into agent graphs
- **`skills/`** — Skills with YAML frontmatter (name, description, resources, tools); compile into a `Toolset`
- **`tools/`** — `Tool` interface, `Handler`, `JSONAdapter` (typed Go functions → tools), `Resolver` (dynamic tool loading e.g. MCP)
- **`memory/`** — `MemoryStore` interface for cross-session persistence
- **`middleware/`** — Reusable middleware (logging, confirm, retry)
- **`context/window/`** — Context window truncation (token-budget based)
- **`context/summary/`** — LLM-based context summarization
- **`contrib/mcp/`** — MCP server integration as a `tools.Resolver`
- **`contrib/otel/`** — OpenTelemetry tracing middleware

### Configuration Pattern

Agents use the functional options pattern:

```go
agent, err := blades.NewAgent("my-agent",
    blades.WithModel(model),
    blades.WithInstruction("You are helpful."),  // supports Go template against session.State()
    blades.WithTools(myTool),
    blades.WithMiddleware(myMiddleware),
    blades.WithContextManager(window.New(...)),
    blades.WithMaxIterations(5),
)
runner := blades.NewRunner(agent)
output, err := runner.Run(ctx, blades.UserMessage("hello"))
```

### Session & State

`Session` tracks history and a key-value `State`. Instructions are rendered as Go templates against `session.State()`, enabling dynamic prompts. `WithOutputKey("key")` saves completed assistant text to state automatically.

### Resume

`Runner.Run` supports `WithResume(true)` + `WithInvocationID(id)` to replay already-completed messages from session history without re-calling the model.
