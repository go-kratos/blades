# Recipe

Recipe is a declarative workflow configuration system for Blades. Define agent workflows in YAML — model selection, instructions, parameters, and multi-step orchestration — without writing Go code.

## Quick Start

### 1. Write a recipe.yaml

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
registry := recipe.NewRegistry()
registry.Register("gpt-4o", openai.NewModel("gpt-4o", openai.Config{
    APIKey: os.Getenv("OPENAI_API_KEY"),
}))

// Load & build
spec, _ := recipe.LoadFromFile("recipe.yaml")
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
| `name` | string | Yes | Recipe name, becomes the Agent name |
| `description` | string | No | Description |
| `model` | string | Conditional | Model name in the registry. Required when no sub_agents or in tool mode |
| `instruction` | string | Conditional | System prompt template. Optional in sequential/parallel mode |
| `prompt` | string | No | Initial user message template, injected as the first user message |
| `parameters` | list | No | Parameter definitions, see [Parameters](#parameters) |
| `execution` | string | When sub_agents exist | Execution mode: `sequential` / `parallel` / `tool` |
| `sub_agents` | list | No | Sub-recipe list, see [Sub-recipes](#sub-recipes) |
| `tools` | list | No | External tool names, must be registered via `ToolRegistry` |
| `output_key` | string | No | Key to store output in session state. Not supported in `sequential` / `parallel` mode |
| `max_iterations` | int | No | Max agent iterations. Not supported in `sequential` / `parallel` mode |

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

### Sub-recipes

Sub-recipes are defined under `sub_agents`. Each one is built into an independent Agent.

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
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Sub-recipe name, must be unique. In tool mode this becomes the tool name |
| `description` | string | No | Description. In tool mode this becomes the tool description |
| `model` | string | No | Override parent model. Inherits if omitted |
| `instruction` | string | Yes | System prompt |
| `prompt` | string | No | Initial user message template |
| `parameters` | list | No | Sub-recipe specific parameters |
| `tools` | list | No | External tool names |
| `output_key` | string | No | Output key (not supported in tool mode) |
| `max_iterations` | int | No | Max iterations |

## Execution Modes

### sequential — Sequential Execution

Sub-recipes run one after another in order. Each step can write to session state via `output_key`, and the next step can reference it with `{{.output_key}}`.

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

Sub-recipes run concurrently with no dependencies between them.

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

Each sub-recipe is wrapped as a tool. The parent agent's LLM decides when to call which tool. You can also mix in function tools registered via `ToolRegistry`.

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
> - Sub-recipe `name` becomes the tool name, `description` becomes the tool description
> - `output_key` is not supported on sub-recipes
> - Sub-recipe names must not conflict with names in the `tools` list
> - Function tools from `tools` list and sub-recipe tools are merged together

## Model Registration

The `model` field in YAML is a key in the registry. Register actual ModelProvider instances in Go:

```go
registry := recipe.NewRegistry()

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

Sub-recipes without a `model` field inherit the parent model. Different sub-recipes can use different models:

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
toolRegistry := recipe.NewStaticToolRegistry()
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
toolRegistry := recipe.NewStaticToolRegistry()
toolRegistry.Register("web-search", mySearchTool)

agent, _ := recipe.Build(spec,
    recipe.WithModelRegistry(registry),
    recipe.WithToolRegistry(toolRegistry),
)
```

Function tools can be freely combined with sub-recipe tools in `tool` execution mode. See [recipe-tool](../examples/recipe-tool/) for a complete example.

## API

```go
// Load
spec, err := recipe.LoadFromFile("recipe.yaml")       // from file
spec, err := recipe.LoadFromFS(fs, "recipe.yaml")     // from embed.FS
spec, err := recipe.Parse(yamlBytes)                   // from []byte

// Validate
err := recipe.Validate(spec)
err := recipe.ValidateParams(spec, params)

// Build
agent, err := recipe.Build(spec,
    recipe.WithModelRegistry(registry),       // required
    recipe.WithToolRegistry(toolRegistry),    // required when tools are used
    recipe.WithParams(map[string]any{...}),   // when parameters are defined
)

// The built agent is a standard blades.Agent
runner := blades.NewRunner(agent)
output, err := runner.Run(ctx, blades.UserMessage("..."))
stream := runner.RunStream(ctx, blades.UserMessage("..."))
```

## Examples

- [recipe-basic](../examples/recipe-basic/) — Single agent with parameterized instruction
- [recipe-sequential](../examples/recipe-sequential/) — Sequential pipeline with output_key passing
- [recipe-tool](../examples/recipe-tool/) — Sub-recipes + function tools with LLM-driven dispatch
