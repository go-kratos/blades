# Recipe

Recipe 是 Blades 的声明式工作流配置系统。通过 YAML 文件定义 Agent 工作流，无需编写 Go 代码即可描述模型选择、提示词、参数化、多步骤编排等能力。

## 快速开始

### 1. 编写 recipe.yaml

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

### 2. Go 代码加载并运行

```go
// 注册模型
registry := recipe.NewRegistry()
registry.Register("gpt-4o", openai.NewModel("gpt-4o", openai.Config{
    APIKey: os.Getenv("OPENAI_API_KEY"),
}))

// 加载 & 构建
spec, _ := recipe.LoadFromFile("recipe.yaml")
agent, _ := recipe.Build(spec,
    recipe.WithModelRegistry(registry),
    recipe.WithParams(map[string]any{"language": "go"}),
)

// 运行
runner := blades.NewRunner(agent)
output, _ := runner.Run(ctx, blades.UserMessage("Review this code: ..."))
```

## YAML 配置参考

### 顶层字段

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `version` | string | 是 | 版本号，当前为 `"1.0"` |
| `name` | string | 是 | Recipe 名称，也是构建出的 Agent 名称 |
| `description` | string | 否 | 描述信息 |
| `model` | string | 视情况 | 注册表中的模型名。无 sub_recipes 或 tool 模式时必填 |
| `instruction` | string | 视情况 | 系统提示词模版。sequential/parallel 模式可省略 |
| `prompt` | string | 否 | 初始用户消息模版，构建时会作为第一条 user message 注入 |
| `parameters` | list | 否 | 参数定义，见[参数配置](#参数配置) |
| `execution` | string | 有 sub_recipes 时必填 | 执行模式：`sequential` / `parallel` / `tool` |
| `sub_recipes` | list | 否 | 子配方列表，见[子配方](#子配方) |
| `tools` | list | 否 | 外部工具名列表，需通过 `ToolRegistry` 注册 |
| `output_key` | string | 否 | 将输出写入 session state 的 key |
| `max_iterations` | int | 否 | Agent 最大迭代次数 |

### 参数配置

参数在 `instruction` 和 `prompt` 中通过 Go 模版语法 `{{.参数名}}` 引用。

```yaml
parameters:
  - name: language
    type: select          # string / number / boolean / select
    description: 编程语言
    required: required    # required / optional（默认 optional）
    default: go           # 可选，默认值
    options: [go, python] # select 类型必填
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | 是 | 参数名，不可重复 |
| `type` | string | 是 | `string` / `number` / `boolean` / `select` |
| `description` | string | 否 | 参数描述 |
| `required` | string | 否 | `required` 或 `optional` |
| `default` | any | 否 | 默认值。select 类型的默认值必须在 options 中 |
| `options` | list | select 时必填 | 可选值列表 |

运行时通过 `WithParams` 传入参数值：

```go
recipe.Build(spec,
    recipe.WithModelRegistry(registry),
    recipe.WithParams(map[string]any{"language": "go"}),
)
```

### 子配方

子配方定义在 `sub_recipes` 中，每个子配方会被构建为一个独立的 Agent。

```yaml
sub_recipes:
  - name: step-name
    description: 这个步骤做什么
    model: gpt-4o-mini    # 可选，不填则继承父级 model
    instruction: |
      你的指令...
    output_key: result     # 可选，输出写入 session state
    max_iterations: 5      # 可选
    tools: [my-tool]       # 可选，外部工具
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | 是 | 子配方名称，不可重复。tool 模式下就是工具名 |
| `description` | string | 否 | 描述。tool 模式下是工具描述，建议填写 |
| `model` | string | 否 | 覆盖父级模型。不填则继承父级 |
| `instruction` | string | 是 | 系统提示词 |
| `prompt` | string | 否 | 初始用户消息模版 |
| `parameters` | list | 否 | 子配方自己的参数 |
| `tools` | list | 否 | 外部工具名列表 |
| `output_key` | string | 否 | 输出 key（tool 模式下不支持） |
| `max_iterations` | int | 否 | 最大迭代次数 |

## 执行模式

### sequential — 顺序执行

子配方按定义顺序依次执行。前一步通过 `output_key` 写入 session state，后一步通过 `{{.output_key}}` 引用。

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
sub_recipes:
  - name: syntax-checker
    instruction: |
      Check the {{.language}} code for syntax errors.
    output_key: syntax_report

  - name: quality-reviewer
    instruction: |
      Review code quality. Syntax report: {{.syntax_report}}
    output_key: quality_report
```

### parallel — 并发执行

子配方同时执行，互不依赖。

```yaml
version: "1.0"
name: multi-review
model: gpt-4o
execution: parallel
sub_recipes:
  - name: security-review
    instruction: Check for security vulnerabilities.
    output_key: security_report

  - name: performance-review
    instruction: Analyze performance issues.
    output_key: performance_report
```

### tool — 工具调度

每个子配方被包装为工具，由父 Agent 的 LLM 自主决定何时调用哪个工具。

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
execution: tool
sub_recipes:
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

> tool 模式要点：
> - 父级必须指定 `model`（用于驱动工具调用的 LLM）
> - 子配方的 `name` 就是工具名，`description` 就是工具描述
> - 子配方不支持 `output_key`
> - 子配方名不能与 `tools` 列表中的外部工具重名

## 模型注册

YAML 中的 `model` 字段是注册表中的 key，需要在 Go 代码中注册实际的 ModelProvider：

```go
registry := recipe.NewRegistry()

// 注册 OpenAI 模型
registry.Register("gpt-4o", openai.NewModel("gpt-4o", openai.Config{
    APIKey: os.Getenv("OPENAI_API_KEY"),
}))

// 注册 OpenAI 协议兼容的模型（如智谱、DeepSeek）
registry.Register("glm-5", openai.NewModel("glm-5", openai.Config{
    BaseURL: "https://open.bigmodel.cn/api/paas/v4",
    APIKey:  os.Getenv("ZHIPU_API_KEY"),
}))

// 注册 Anthropic 模型
registry.Register("claude-sonnet", anthropic.NewModel("claude-sonnet-4-5-20250514", anthropic.Config{
    APIKey: os.Getenv("ANTHROPIC_API_KEY"),
}))
```

子配方未指定 `model` 时自动继承父级模型。不同子配方可以使用不同模型：

```yaml
model: gpt-4o                    # 父级默认模型
sub_recipes:
  - name: fast-step
    model: gpt-4o-mini            # 用便宜的小模型
    instruction: ...
  - name: deep-step               # 继承父级 gpt-4o
    instruction: ...
```

## 工具注册

通过 `ToolRegistry` 注册外部工具，在 YAML 中按名引用：

```go
toolRegistry := recipe.NewStaticToolRegistry()
toolRegistry.Register("web-search", mySearchTool)

agent, _ := recipe.Build(spec,
    recipe.WithModelRegistry(registry),
    recipe.WithToolRegistry(toolRegistry),
)
```

```yaml
tools: [web-search]
```

## API

```go
// 加载
spec, err := recipe.LoadFromFile("recipe.yaml")       // 从文件
spec, err := recipe.LoadFromFS(fs, "recipe.yaml")     // 从 embed.FS
spec, err := recipe.Parse(yamlBytes)                   // 从 []byte

// 校验
err := recipe.Validate(spec)
err := recipe.ValidateParams(spec, params)

// 构建
agent, err := recipe.Build(spec,
    recipe.WithModelRegistry(registry),       // 必填
    recipe.WithToolRegistry(toolRegistry),    // 有 tools 时必填
    recipe.WithParams(map[string]any{...}),   // 有 parameters 时
)

// 构建出的 agent 就是标准的 blades.Agent，正常使用即可
runner := blades.NewRunner(agent)
output, err := runner.Run(ctx, blades.UserMessage("..."))
stream := runner.RunStream(ctx, blades.UserMessage("..."))
```

## 示例

- [recipe-basic](../examples/recipe-basic/) — 单 Agent，参数化 instruction
- [recipe-sequential](../examples/recipe-sequential/) — 顺序流水线，output_key 传递
- [recipe-tool](../examples/recipe-tool/) — 子配方作为工具，LLM 动态调度
