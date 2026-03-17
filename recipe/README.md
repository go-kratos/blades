# Recipe

Recipe is a declarative workflow configuration system for Blades. Define agent workflows in YAML — model selection, instructions, parameters, context management, middlewares, and multi-step orchestration — without writing Go code.

## Quick Start

Write a YAML spec:

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

Load and run in Go:

```go
modelRegistry := recipe.NewModelRegistry()
modelRegistry.Register("gpt-4o", openai.NewModel("gpt-4o", openai.Config{
    APIKey: os.Getenv("OPENAI_API_KEY"),
}))

spec, _ := recipe.LoadFromFile("agent.yaml")
agent, _ := recipe.Build(spec,
    recipe.WithModelRegistry(modelRegistry),
    recipe.WithParams(map[string]any{"language": "go"}),
)

runner := blades.NewRunner(agent)
output, _ := runner.Run(ctx, blades.UserMessage("Review this code: ..."))
```

## YAML Structure

A recipe YAML file has a `version` and `name` at the root. The `model` field names a `ModelProvider` registered in the `ModelRegistry`. The `instruction` field is the system prompt and supports Go template syntax (`{{.param_name}}`). The optional `prompt` field injects an initial user message before the first user turn.

When the recipe has `sub_agents`, an `execution` mode is required. Sub-agents without their own `model` inherit the parent's model.

### Parameters

Parameters are declared under `parameters` and referenced in `instruction` or `prompt` via `{{.name}}`. Each parameter has a `type` (`string`, `number`, `boolean`, or `select`), an optional `default`, and a `required` flag (`required` or `optional`). The `select` type requires an `options` list.

```yaml
parameters:
  - name: language
    type: select
    required: required
    options: [go, python]
    default: go
```

Pass values at build time via `recipe.WithParams`:

```go
recipe.Build(spec,
    recipe.WithModelRegistry(modelRegistry),
    recipe.WithParams(map[string]any{"language": "go"}),
)
```

### Sub-agents

Each entry under `sub_agents` is built into an independent agent. It shares the same fields as the top-level spec (`instruction`, `model`, `tools`, `output_key`, `max_iterations`, `context`, `middlewares`) with the addition of `prompt` for a per-step initial message. The `name` field is required and must be unique.

```yaml
sub_agents:
  - name: step-name
    description: What this step does
    model: gpt-4o-mini    # inherits parent model if omitted
    instruction: |
      Your instruction here...
    output_key: result
    tools: [my-tool]
```

## Execution Modes

### sequential

Sub-agents run one after another. Each step can write to session state via `output_key`, and subsequent steps can read it via `{{.output_key}}`.

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
    instruction: Check the {{.language}} code for syntax errors.
    output_key: syntax_report
  - name: quality-reviewer
    instruction: Review code quality. Syntax report: {{.syntax_report}}
    output_key: quality_report
```

`instruction` and `output_key` at the parent level are not used in sequential mode — the parent is a pure orchestrator.

### parallel

Sub-agents run concurrently with no data dependencies. Outputs are written to session state independently.

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

### tool

Each sub-agent is wrapped as a tool. The parent agent's LLM decides when to call which sub-agent. The parent requires a `model` and an `instruction`. Sub-agent `name` becomes the tool name, and `description` becomes the tool description. Function tools from the `tools` list and sub-agent tools are merged together.

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
    instruction: Verify the given claim and provide citations.
  - name: data-analyst
    description: Analyze data, statistics, and trends
    instruction: Analyze the given data and produce clear insights.
```

`output_key` is not supported on sub-agents in tool mode. Sub-agent names must not conflict with names in the `tools` list.

### loop

Sub-agents run repeatedly in a loop up to `max_iterations` (default: 10). After each full iteration the loop checks whether any sub-agent signalled an exit via the `exit` tool. The loop ends when max iterations is reached or when a sub-agent calls `exit`.

```yaml
version: "1.0"
name: iterative-writer
model: gpt-4o
execution: loop
max_iterations: 3
sub_agents:
  - name: writer
    instruction: Write a short paragraph about the requested topic.
    output_key: draft
  - name: reviewer
    instruction: |
      Review the draft. If the quality is acceptable, call the exit tool.
      Otherwise provide feedback for the next iteration.
    tools:
      - exit
```

Register the `exit` tool so sub-agents can signal an early stop:

```go
toolRegistry := recipe.NewToolRegistry()
toolRegistry.Register("exit", tools.NewExitTool())

agent, _ := recipe.Build(spec,
    recipe.WithModelRegistry(modelRegistry),
    recipe.WithToolRegistry(toolRegistry),
)
```

`output_key` and `instruction` at the parent level are not used in loop mode. `max_iterations` sets the upper bound on iterations.

## Context Management

The `context` field controls how message history is trimmed before each model call. It can appear on the top-level spec or on individual sub-agents.

### summarize

Old messages are compressed into a rolling LLM-generated summary. The most recent messages are always kept verbatim.

```yaml
context:
  strategy: summarize
  max_tokens: 80000    # compress when history exceeds this token budget
  keep_recent: 10      # always keep the last N messages verbatim (default: 10)
  batch_size: 20       # messages to summarize per pass (default: 20)
  model: gpt-4o-mini   # summarizer model; falls back to the agent's model
```

### window

Oldest messages are dropped to stay within the budget.

```yaml
context:
  strategy: window
  max_tokens: 80000    # drop oldest when total tokens exceed this
  max_messages: 100    # drop oldest when message count exceeds this
```

## Middleware

The `middlewares` field attaches a middleware chain to an agent. Middlewares are resolved by name from a `MiddlewareRegistry` at build time. The `options` map is passed as-is to the registered factory. It can appear on the top-level spec or on individual sub-agents.

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

mwRegistry.Register("tracing", func(_ map[string]any) (blades.Middleware, error) {
    return myTracingMiddleware, nil
})

mwRegistry.Register("logging", func(opts map[string]any) (blades.Middleware, error) {
    level, _ := opts["level"].(string)
    return newLoggingMiddleware(level), nil
})

agent, _ := recipe.Build(spec,
    recipe.WithModelRegistry(modelRegistry),
    recipe.WithMiddlewareRegistry(mwRegistry),
)
```

## Model Registration

The `model` field in YAML is a lookup key in the `ModelRegistry`. Register any `blades.ModelProvider` implementation:

```go
modelRegistry := recipe.NewModelRegistry()

modelRegistry.Register("gpt-4o", openai.NewModel("gpt-4o", openai.Config{
    APIKey: os.Getenv("OPENAI_API_KEY"),
}))

modelRegistry.Register("glm-5", openai.NewModel("glm-5", openai.Config{
    BaseURL: "https://open.bigmodel.cn/api/paas/v4",
    APIKey:  os.Getenv("ZHIPU_API_KEY"),
}))

modelRegistry.Register("claude-sonnet", anthropic.NewModel("claude-sonnet-4-5-20250514", anthropic.Config{
    APIKey: os.Getenv("ANTHROPIC_API_KEY"),
}))
```

Sub-agents inherit the parent model when their own `model` field is omitted:

```yaml
model: gpt-4o
sub_agents:
  - name: fast-step
    model: gpt-4o-mini   # override for this step
    instruction: ...
  - name: deep-step      # inherits gpt-4o
    instruction: ...
```

## Tool Registration

Register tools via `ToolRegistry` and reference them by name in YAML:

```go
type SearchReq struct {
    Query string `json:"query" jsonschema:"The search query"`
}
type SearchRes struct {
    Results []string `json:"results"`
}

searchTool, _ := tools.NewFunc("web-search", "Search the web", func(ctx context.Context, req SearchReq) (SearchRes, error) {
    // ...
})

toolRegistry := recipe.NewToolRegistry()
toolRegistry.Register("web-search", searchTool)

agent, _ := recipe.Build(spec,
    recipe.WithModelRegistry(modelRegistry),
    recipe.WithToolRegistry(toolRegistry),
)
```

```yaml
tools: [web-search]
```

## API

```go
// Load
spec, err := recipe.LoadFromFile("agent.yaml")
spec, err := recipe.LoadFromFS(fs, "agent.yaml")
spec, err := recipe.Parse(yamlBytes)

// Validate
err := recipe.Validate(spec)
err := recipe.ValidateParams(spec, params)

// Build
agent, err := recipe.Build(spec,
    recipe.WithModelRegistry(modelRegistry),            // required
    recipe.WithToolRegistry(toolRegistry),              // required when tools are used
    recipe.WithMiddlewareRegistry(mwRegistry),  // required when middlewares are used
    recipe.WithParams(map[string]any{...}),             // when parameters are defined
)

// The built agent is a standard blades.Agent
runner := blades.NewRunner(agent)
output, err := runner.Run(ctx, blades.UserMessage("..."))
stream := runner.RunStream(ctx, blades.UserMessage("..."))
```

## Examples

- [recipe-basic](../examples/recipe-basic/) — Single agent with parameterized instruction
- [recipe-sequential](../examples/recipe-sequential/) — Sequential pipeline with `output_key` passing
- [recipe-tool](../examples/recipe-tool/) — Sub-agents and function tools with LLM-driven dispatch

