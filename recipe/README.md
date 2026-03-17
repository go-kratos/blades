# Recipe

Recipe is a declarative workflow configuration system for Blades. Define agent workflows in YAML — model selection, instructions, parameters, context management, middlewares, and multi-step orchestration — without writing Go code.

## Quick Start

### 1. Write an agent.yaml

```yaml
version: "1.0"
name: code-reviewer
description: Review code quality
model: gpt-4o
parameters:
  - name: language
    type: select
    required: required
    options: [go, python, typescript]
instruction: |
  You are a {{.language}} code review expert.
  Analyze code for bugs, style issues, and performance.
```

### 2. Load and run in Go

```go
// Register models
registry := recipe.NewModelRegistry()
registry.Register("gpt-4o", openai.NewModel("gpt-4o", openai.Config{
    APIKey: os.Getenv("OPENAI_API_KEY"),
}))

// Load & build
spec, _ := recipe.LoadFromFile("agent.yaml")
agent, _ := recipe.Build(spec,
    recipe.WithModelRegistry(registry),
    recipe.WithParams(map[string]any{"language": "go"}),
)

// Run
runner := blades.NewRunner(agent)
output, _ := runner.Run(ctx, blades.UserMessage("Review this code: ..."))
```

## YAML Reference

### Top-level Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `version` | string | Yes | Version number, currently `"1.0"` |
| `name` | string | Yes | Agent name |
| `description` | string | No | Description |
| `model` | string | Conditional | Model name in the registry. Required when no `sub_agents` or in `tool` mode |
| `instruction` | string | Conditional | System prompt template. Optional in `sequential`/`parallel` mode |
| `prompt` | string | No | Initial user message template, injected as the first user message |
| `parameters` | list | No | Parameter definitions, see [Parameters](#parameters) |
| `execution` | string | When `sub_agents` exist | Execution mode: `sequential` / `parallel` / `tool` |
| `sub_agents` | list | No | Sub-agent list, see [Sub-agents](#sub-agents) |
| `tools` | list | No | External tool names, must be registered via `ToolRegistry` |
| `output_key` | string | No | Key to store output in session state. Not supported in `sequential` / `parallel` mode |
| `max_iterations` | int | No | Max agent iterations. Not supported in `sequential` / `parallel` mode |
| `context` | object | No | Context window management, see [Context Management](#context-management) |
| `middlewares` | list | No | Middleware chain, see [Middleware](#middleware) |

### Parameters

Parameters are referenced in `instruction` and `prompt` using Go template syntax `{{.param_name}}`.

```yaml
parameters:
  - name: language
    type: select          # string / number / boolean / select
    description: Programming language
    required: required    # required / optional (default: optional)
    default: go           # optional, default value
    options: [go, python] # required for select type
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Parameter name, must be unique |
| `type` | string | Yes | `string` / `number` / `boolean` / `select` |
| `description` | string | No | Description |
| `required` | string | No | `required` or `optional` |
| `default` | any | No | Default value. For select type, must be one of the options |
| `options` | list | For select | Allowed values |

Pass parameter values at build time:

```go
recipe.Build(spec,
    recipe.WithModelRegistry(registry),
    recipe.WithParams(map[string]any{"language": "go"}),
)
```

### Sub-agents

Sub-agents are defined under `sub_agents`. Each one is built into an independent Agent.

```yaml
sub_agents:
  - name: step-name
    description: What this step does
    model: gpt-4o-mini    # optional, inherits parent model if omitted
    instruction: |
      Your instruction here...
    output_key: result     # optional, stores output in session state
    max_iterations: 5      # optional
    tools: [my-tool]       # optional, external tools
    context:               # optional, per-agent context management
      strategy: window
      max_messages: 20
    middlewares:           # optional, per-agent middleware chain
      - name: logging
        options:
          level: debug
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Sub-agent name, must be unique. In `tool` mode this becomes the tool name |
| `description` | string | No | Description. In `tool` mode this becomes the tool description |
| `model` | string | No | Override parent model. Inherits parent model if omitted |
| `instruction` | string | Yes | System prompt |
| `prompt` | string | No | Initial user message template |
| `parameters` | list | No | Sub-agent specific parameters |
| `tools` | list | No | External tool names |
| `output_key` | string | No | Output key (not supported in `tool` mode) |
| `max_iterations` | int | No | Max iterations |
| `context` | object | No | Context window management, see [Context Management](#context-management) |
| `middlewares` | list | No | Middleware chain, see [Middleware](#middleware) |

## Execution Modes

### sequential — Sequential Execution

Sub-agents run one after another in order. Each step can write to session state via `output_key`, and the next step can reference it with `{{.output_key}}`.

```yaml
version: "1.0"
name: code-review-pipeline
model: gpt-4o
execution: sequential
parameters:
  - name: language
    type: select
    required: required
    options: [go, python]
sub_agents:
  - name: syntax-checker
    instruction: |
      Check the {{.language}} code for syntax errors.
    output_key: syntax_report

  - name: quality-reviewer
    instruction: |
      Review code quality. Syntax report: {{.syntax_report}}
    output_key: quality_report
```

### parallel — Concurrent Execution

Sub-agents run concurrently with no dependencies between them.

```yaml
version: "1.0"
name: multi-review
model: gpt-4o
execution: parallel
sub_agents:
  - name: security-review
    instruction: Check for security vulnerabilities.
    output_key: security_report

  - name: performance-review
    instruction: Analyze performance issues.
    output_key: performance_report
```

### tool — Tool Dispatch

Each sub-agent is wrapped as a tool. The parent agent's LLM decides when to call which tool. You can also mix in function tools registered via `ToolRegistry`.

```yaml
version: "1.0"
name: research-assistant
model: gpt-4o
parameters:
  - name: topic
    type: string
    required: required
instruction: |
  Research "{{.topic}}" thoroughly.
  You MUST call the fact-checker and data-analyst tools.
  Use extract-emails when you find contact information.
tools:
  - extract-emails
execution: tool
sub_agents:
  - name: fact-checker
    description: Verify claims and provide citations
    instruction: |
      You are a fact-checking specialist. Verify the given claim
      and provide citations from reliable sources.

  - name: data-analyst
    description: Analyze data, statistics, and trends
    instruction: |
      You are a data analysis specialist. Analyze the given data
      and produce clear insights.
```

> **Tool mode notes:**
> - Parent must specify `model` (the LLM that drives tool calls)
> - Sub-agent `name` becomes the tool name, `description` becomes the tool description
> - `output_key` is not supported on sub-agents in tool mode
> - Sub-agent names must not conflict with names in the `tools` list
> - Function tools from `tools` list and sub-agent tools are merged together

## Context Management

The `context` field configures how the agent's message history is managed before each model call. Two strategies are supported.

### summarize — LLM-based Rolling Summary

Old messages are compressed into a rolling summary using an LLM. The most recent messages are always kept verbatim.

```yaml
context:
  strategy: summarize
  max_tokens: 80000    # compress when history exceeds this token budget
  keep_recent: 10      # always keep the last N messages verbatim (default: 10)
  batch_size: 20       # messages to summarize per compression pass (default: 20)
  model: gpt-4o-mini   # model for summarization; defaults to the agent's model
```

### window — Sliding Window

Old messages are dropped from the front of the history to stay within the budget.

```yaml
context:
  strategy: window
  max_tokens: 80000    # drop oldest messages when total tokens exceed this
  max_messages: 100    # drop oldest messages when count exceeds this
```

| Field | Type | Strategy | Description |
|-------|------|----------|-------------|
| `strategy` | string | — | Required. `summarize` or `window` |
| `max_tokens` | int | both | Token budget trigger. `0` disables token-based limiting |
| `keep_recent` | int | summarize | Messages always kept verbatim (default: 10) |
| `batch_size` | int | summarize | Messages summarized per pass (default: 20) |
| `max_messages` | int | window | Max message count before oldest are dropped |
| `model` | string | summarize | Summarizer model name; falls back to the agent's model if omitted |

The `context` field can appear on both the top-level `AgentSpec` and on individual `sub_agents`, allowing different strategies per step.

Register the context model the same way as any other model:

```go
registry.Register("gpt-4o-mini", openai.NewModel("gpt-4o-mini", openai.Config{
    APIKey: os.Getenv("OPENAI_API_KEY"),
}))
```

## Middleware

The `middlewares` field attaches a chain of middlewares to the agent. Middlewares are resolved by name from a `MiddlewareRegistry` at build time; the `options` map is passed as-is to the registered factory.

```yaml
middlewares:
  - name: tracing
  - name: logging
    options:
      level: info
```

Register middleware factories in Go:

```go
mwRegistry := recipe.NewMiddlewareRegistry()

// No-options middleware
mwRegistry.Register("tracing", func(_ map[string]any) (blades.Middleware, error) {
    return myTracingMiddleware, nil
})

// Options-aware middleware
mwRegistry.Register("logging", func(opts map[string]any) (blades.Middleware, error) {
    level, _ := opts["level"].(string)
    return newLoggingMiddleware(level), nil
})

agent, _ := recipe.Build(spec,
    recipe.WithModelRegistry(registry),
    recipe.WithMiddlewareRegistry(mwRegistry),
)
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Middleware name, must be registered in `MiddlewareRegistry`. Must be unique per agent |
| `options` | object | No | Key-value options passed to the factory function |

The `middlewares` field can appear on both the top-level `AgentSpec` and on individual `sub_agents`.

## Model Registration

The `model` field in YAML is a key in the registry. Register actual `ModelProvider` instances in Go:

```go
registry := recipe.NewModelRegistry()

// OpenAI models
registry.Register("gpt-4o", openai.NewModel("gpt-4o", openai.Config{
    APIKey: os.Getenv("OPENAI_API_KEY"),
}))

// OpenAI-compatible models (e.g. Zhipu, DeepSeek)
registry.Register("glm-5", openai.NewModel("glm-5", openai.Config{
    BaseURL: "https://open.bigmodel.cn/api/paas/v4",
    APIKey:  os.Getenv("ZHIPU_API_KEY"),
}))

// Anthropic models
registry.Register("claude-sonnet", anthropic.NewModel("claude-sonnet-4-5-20250514", anthropic.Config{
    APIKey: os.Getenv("ANTHROPIC_API_KEY"),
}))
```

Sub-agents without a `model` field inherit the parent model. Different sub-agents can use different models:

```yaml
model: gpt-4o                    # parent default
sub_agents:
  - name: fast-step
    model: gpt-4o-mini            # cheaper model
    instruction: ...
  - name: deep-step               # inherits gpt-4o
    instruction: ...
```

## Tool Registration

Register tools via `ToolRegistry` and reference them by name in YAML. Tools are application-defined — there are no built-in tools.

### Using `tools.NewFunc` (recommended)

Create strongly-typed function tools with automatic JSON schema generation:

```go
type ExtractEmailsReq struct {
    Text string `json:"text" jsonschema:"The text to extract email addresses from"`
}

type ExtractEmailsRes struct {
    Matches []string `json:"matches" jsonschema:"The extracted email addresses"`
}

var emailPattern = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)

func extractEmails(_ context.Context, req ExtractEmailsReq) (ExtractEmailsRes, error) {
    matches := emailPattern.FindAllString(req.Text, -1)
    if matches == nil {
        matches = []string{}
    }
    return ExtractEmailsRes{Matches: matches}, nil
}

// Create the tool
emailTool, _ := tools.NewFunc("extract-emails", "Extract email addresses from text", extractEmails)

// Register in a ToolRegistry
toolRegistry := recipe.NewToolRegistry()
toolRegistry.Register("extract-emails", emailTool)

// Build with the registry
agent, _ := recipe.Build(spec,
    recipe.WithModelRegistry(registry),
    recipe.WithToolRegistry(toolRegistry),
)
```

```yaml
tools: [extract-emails]
```

### Using `tools.NewTool` (raw handler)

For lower-level control, use `NewTool` with a raw `HandleFunc`:

```go
toolRegistry := recipe.NewToolRegistry()
toolRegistry.Register("web-search", mySearchTool)

agent, _ := recipe.Build(spec,
    recipe.WithModelRegistry(registry),
    recipe.WithToolRegistry(toolRegistry),
)
```

Function tools can be freely combined with sub-agent tools in `tool` execution mode. See [recipe-tool](../examples/recipe-tool/) for a complete example.

## API

```go
// Load
spec, err := recipe.LoadFromFile("agent.yaml")       // from file
spec, err := recipe.LoadFromFS(fs, "agent.yaml")     // from embed.FS
spec, err := recipe.Parse(yamlBytes)                  // from []byte

// Validate
err := recipe.Validate(spec)
err := recipe.ValidateParams(spec, params)

// Build
agent, err := recipe.Build(spec,
    recipe.WithModelRegistry(registry),          // required
    recipe.WithToolRegistry(toolRegistry),       // required when tools are used
    recipe.WithMiddlewareRegistry(mwRegistry),   // required when middlewares are used
    recipe.WithParams(map[string]any{...}),      // when parameters are defined
)

// The built agent is a standard blades.Agent
runner := blades.NewRunner(agent)
output, err := runner.Run(ctx, blades.UserMessage("..."))
stream := runner.RunStream(ctx, blades.UserMessage("..."))
```

## Examples

- [recipe-basic](../examples/recipe-basic/) — Single agent with parameterized instruction
- [recipe-sequential](../examples/recipe-sequential/) — Sequential pipeline with output_key passing
- [recipe-tool](../examples/recipe-tool/) — Sub-agents + function tools with LLM-driven dispatch
