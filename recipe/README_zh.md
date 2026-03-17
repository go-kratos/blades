# Recipe

Recipe 是 Blades 的声明式工作流配置系统。通过 YAML 定义 Agent 工作流 —— 模型选择、提示词、参数、上下文管理、中间件和多步骤编排 —— 无需编写 Go 代码。

## 快速开始

### 1. 编写 agent.yaml

```yaml
version: "1.0"
name: code-reviewer
description: 代码审查 Agent
model: gpt-4o
parameters:
  - name: language
    type: select
    required: required
    options: [go, python, typescript]
instruction: |
  你是一位 {{.language}} 代码审查专家。
  分析代码中的 bug、风格问题和性能问题。
```

### 2. 在 Go 中加载并运行

```go
// 注册模型
registry := recipe.NewRegistry()
registry.Register("gpt-4o", openai.NewModel("gpt-4o", openai.Config{
    APIKey: os.Getenv("OPENAI_API_KEY"),
}))

// 加载并构建
spec, _ := recipe.LoadFromFile("agent.yaml")
agent, _ := recipe.Build(spec,
    recipe.WithModelRegistry(registry),
    recipe.WithParams(map[string]any{"language": "go"}),
)

// 运行
runner := blades.NewRunner(agent)
output, _ := runner.Run(ctx, blades.UserMessage("审查这段代码: ..."))
```

## YAML 参考

### 顶层字段

| 字段 | 类型 | 是否必填 | 说明 |
|------|------|----------|------|
| `version` | string | 是 | 版本号，当前为 `"1.0"` |
| `name` | string | 是 | Agent 名称 |
| `description` | string | 否 | 描述 |
| `model` | string | 条件必填 | 注册表中的模型名。无 `sub_agents` 时或 `tool` 模式时必填 |
| `instruction` | string | 条件必填 | 系统提示词模板。`sequential`/`parallel` 模式下可选 |
| `prompt` | string | 否 | 初始用户消息模板，作为第一条用户消息注入 |
| `parameters` | list | 否 | 参数定义，见 [参数](#参数) |
| `execution` | string | 有 `sub_agents` 时 | 执行模式：`sequential` / `parallel` / `tool` |
| `sub_agents` | list | 否 | 子 Agent 列表，见 [子 Agent](#子-agent) |
| `tools` | list | 否 | 外部工具名，需通过 `ToolRegistry` 注册 |
| `output_key` | string | 否 | 输出写入 Session 状态的键名。`sequential`/`parallel` 模式不支持 |
| `max_iterations` | int | 否 | 最大迭代次数。`sequential`/`parallel` 模式不支持 |
| `context` | object | 否 | 上下文窗口管理，见 [上下文管理](#上下文管理) |
| `middlewares` | list | 否 | 中间件链，见 [中间件](#中间件) |

### 参数

参数在 `instruction` 和 `prompt` 中用 Go 模板语法 `{{.param_name}}` 引用。

```yaml
parameters:
  - name: language
    type: select          # string / number / boolean / select
    description: 编程语言
    required: required    # required / optional（默认 optional）
    default: go           # 可选，默认值
    options: [go, python] # select 类型必填
```

| 字段 | 类型 | 是否必填 | 说明 |
|------|------|----------|------|
| `name` | string | 是 | 参数名，须唯一 |
| `type` | string | 是 | `string` / `number` / `boolean` / `select` |
| `description` | string | 否 | 描述 |
| `required` | string | 否 | `required` 或 `optional` |
| `default` | any | 否 | 默认值。select 类型须在 options 中 |
| `options` | list | select 必填 | 允许的值列表 |

在构建时传入参数值：

```go
recipe.Build(spec,
    recipe.WithModelRegistry(registry),
    recipe.WithParams(map[string]any{"language": "go"}),
)
```

### 子 Agent

子 Agent 定义在 `sub_agents` 下，每个都会构建为独立的 Agent。

```yaml
sub_agents:
  - name: step-name
    description: 该步骤的功能描述
    model: gpt-4o-mini    # 可选，省略时继承父 Agent 的模型
    instruction: |
      你的提示词...
    output_key: result     # 可选，将输出存入 Session 状态
    max_iterations: 5      # 可选
    tools: [my-tool]       # 可选，外部工具
    context:               # 可选，子 Agent 独立的上下文管理
      strategy: window
      max_messages: 20
    middlewares:           # 可选，子 Agent 独立的中间件链
      - name: logging
        options:
          level: debug
```

| 字段 | 类型 | 是否必填 | 说明 |
|------|------|----------|------|
| `name` | string | 是 | 子 Agent 名称，须唯一。`tool` 模式下作为工具名 |
| `description` | string | 否 | 描述。`tool` 模式下作为工具描述 |
| `model` | string | 否 | 覆盖父模型，省略时继承 |
| `instruction` | string | 是 | 系统提示词 |
| `prompt` | string | 否 | 初始用户消息模板 |
| `parameters` | list | 否 | 子 Agent 独立参数 |
| `tools` | list | 否 | 外部工具名 |
| `output_key` | string | 否 | 输出键名（`tool` 模式不支持） |
| `max_iterations` | int | 否 | 最大迭代次数 |
| `context` | object | 否 | 上下文窗口管理，见 [上下文管理](#上下文管理) |
| `middlewares` | list | 否 | 中间件链，见 [中间件](#中间件) |

## 执行模式

### sequential — 顺序执行

子 Agent 依次串行执行。每个步骤可通过 `output_key` 写入 Session 状态，下一步骤通过 `{{.output_key}}` 引用。

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
      检查 {{.language}} 代码的语法错误。
    output_key: syntax_report

  - name: quality-reviewer
    instruction: |
      审查代码质量。语法报告：{{.syntax_report}}
    output_key: quality_report
```

### parallel — 并发执行

子 Agent 并发执行，相互无依赖。

```yaml
version: "1.0"
name: multi-review
model: gpt-4o
execution: parallel
sub_agents:
  - name: security-review
    instruction: 检查安全漏洞。
    output_key: security_report

  - name: performance-review
    instruction: 分析性能问题。
    output_key: performance_report
```

### tool — 工具分发

每个子 Agent 被封装为工具，父 Agent 的 LLM 决定何时调用哪个工具。也可以混入通过 `ToolRegistry` 注册的函数工具。

```yaml
version: "1.0"
name: research-assistant
model: gpt-4o
parameters:
  - name: topic
    type: string
    required: required
instruction: |
  深入研究"{{.topic}}"。
  必须调用 fact-checker 和 data-analyst 工具。
  发现联系方式时使用 extract-emails。
tools:
  - extract-emails
execution: tool
sub_agents:
  - name: fact-checker
    description: 核实声明并提供引用来源
    instruction: |
      你是事实核查专家，核实给定声明并提供可靠来源引用。

  - name: data-analyst
    description: 分析数据、统计和趋势
    instruction: |
      你是数据分析专家，分析给定数据并给出清晰洞察。
```

> **tool 模式注意事项：**
> - 父 Agent 必须指定 `model`（驱动工具调用的 LLM）
> - 子 Agent 的 `name` 作为工具名，`description` 作为工具描述
> - tool 模式下子 Agent 不支持 `output_key`
> - 子 Agent 名称不能与 `tools` 列表中的名称冲突
> - `tools` 中的函数工具与子 Agent 工具合并使用

## 上下文管理

`context` 字段配置每次模型调用前对消息历史的管理方式，支持两种策略。

### summarize — LLM 滚动摘要

旧消息通过 LLM 压缩为滚动摘要，最近的消息始终保留原文。

```yaml
context:
  strategy: summarize
  max_tokens: 80000    # 历史超出此 token 预算时触发压缩
  keep_recent: 10      # 始终保留最近 N 条消息原文（默认 10）
  batch_size: 20       # 每次压缩的消息数（默认 20）
  model: gpt-4o-mini   # 摘要模型；省略时使用 Agent 自身的模型
```

### window — 滑动窗口

超出预算时从历史最前端丢弃旧消息。

```yaml
context:
  strategy: window
  max_tokens: 80000    # 总 token 超出时丢弃最旧消息
  max_messages: 100    # 消息数超出时丢弃最旧消息
```

| 字段 | 类型 | 适用策略 | 说明 |
|------|------|----------|------|
| `strategy` | string | — | 必填。`summarize` 或 `window` |
| `max_tokens` | int | 两者 | token 预算触发阈值，`0` 禁用基于 token 的限制 |
| `keep_recent` | int | summarize | 始终保留原文的消息数（默认 10） |
| `batch_size` | int | summarize | 每次压缩的消息数（默认 20） |
| `max_messages` | int | window | 触发丢弃的最大消息数 |
| `model` | string | summarize | 摘要模型名；省略时回退到 Agent 自身的模型 |

`context` 字段可同时出现在顶层 `AgentSpec` 和各 `sub_agents` 中，允许每个步骤使用不同策略。

摘要模型与其他模型注册方式相同：

```go
registry.Register("gpt-4o-mini", openai.NewModel("gpt-4o-mini", openai.Config{
    APIKey: os.Getenv("OPENAI_API_KEY"),
}))
```

## 中间件

`middlewares` 字段为 Agent 挂载中间件链。中间件在构建时通过 `MiddlewareRegistry` 按名称解析；`options` 映射原样传递给注册的工厂函数。

```yaml
middlewares:
  - name: tracing
  - name: logging
    options:
      level: info
```

在 Go 中注册中间件工厂：

```go
mwRegistry := recipe.NewStaticMiddlewareRegistry()

// 无选项的中间件
mwRegistry.Register("tracing", func(_ map[string]any) (blades.Middleware, error) {
    return myTracingMiddleware, nil
})

// 支持选项的中间件
mwRegistry.Register("logging", func(opts map[string]any) (blades.Middleware, error) {
    level, _ := opts["level"].(string)
    return newLoggingMiddleware(level), nil
})

agent, _ := recipe.Build(spec,
    recipe.WithModelRegistry(registry),
    recipe.WithMiddlewareRegistry(mwRegistry),
)
```

| 字段 | 类型 | 是否必填 | 说明 |
|------|------|----------|------|
| `name` | string | 是 | 中间件名，须在 `MiddlewareRegistry` 中注册，同一 Agent 内不可重复 |
| `options` | object | 否 | 键值对选项，传递给工厂函数 |

`middlewares` 字段可同时出现在顶层 `AgentSpec` 和各 `sub_agents` 中。

## 模型注册

YAML 中的 `model` 字段是注册表中的键名。在 Go 中注册实际的 `ModelProvider` 实例：

```go
registry := recipe.NewRegistry()

// OpenAI 模型
registry.Register("gpt-4o", openai.NewModel("gpt-4o", openai.Config{
    APIKey: os.Getenv("OPENAI_API_KEY"),
}))

// OpenAI 兼容模型（如智谱、DeepSeek）
registry.Register("glm-5", openai.NewModel("glm-5", openai.Config{
    BaseURL: "https://open.bigmodel.cn/api/paas/v4",
    APIKey:  os.Getenv("ZHIPU_API_KEY"),
}))

// Anthropic 模型
registry.Register("claude-sonnet", anthropic.NewModel("claude-sonnet-4-5-20250514", anthropic.Config{
    APIKey: os.Getenv("ANTHROPIC_API_KEY"),
}))
```

未指定 `model` 的子 Agent 继承父 Agent 的模型。不同子 Agent 可使用不同模型：

```yaml
model: gpt-4o                    # 父 Agent 默认模型
sub_agents:
  - name: fast-step
    model: gpt-4o-mini            # 使用更廉价的模型
    instruction: ...
  - name: deep-step               # 继承 gpt-4o
    instruction: ...
```

## 工具注册

通过 `ToolRegistry` 注册工具，并在 YAML 中按名称引用。工具由应用层定义，框架不内置任何工具。

### 使用 `tools.NewFunc`（推荐）

通过强类型函数工具自动生成 JSON Schema：

```go
type ExtractEmailsReq struct {
    Text string `json:"text" jsonschema:"要提取邮箱地址的文本"`
}

type ExtractEmailsRes struct {
    Matches []string `json:"matches" jsonschema:"提取到的邮箱地址列表"`
}

var emailPattern = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)

func extractEmails(_ context.Context, req ExtractEmailsReq) (ExtractEmailsRes, error) {
    matches := emailPattern.FindAllString(req.Text, -1)
    if matches == nil {
        matches = []string{}
    }
    return ExtractEmailsRes{Matches: matches}, nil
}

emailTool, _ := tools.NewFunc("extract-emails", "从文本中提取邮箱地址", extractEmails)

toolRegistry := recipe.NewStaticToolRegistry()
toolRegistry.Register("extract-emails", emailTool)

agent, _ := recipe.Build(spec,
    recipe.WithModelRegistry(registry),
    recipe.WithToolRegistry(toolRegistry),
)
```

函数工具与子 Agent 工具可在 `tool` 执行模式下自由混用。完整示例见 [recipe-tool](../examples/recipe-tool/)。

## API

```go
// 加载
spec, err := recipe.LoadFromFile("agent.yaml")       // 从文件
spec, err := recipe.LoadFromFS(fs, "agent.yaml")     // 从 embed.FS
spec, err := recipe.Parse(yamlBytes)                  // 从 []byte

// 验证
err := recipe.Validate(spec)
err := recipe.ValidateParams(spec, params)

// 构建
agent, err := recipe.Build(spec,
    recipe.WithModelRegistry(registry),          // 必填
    recipe.WithToolRegistry(toolRegistry),       // 使用工具时必填
    recipe.WithMiddlewareRegistry(mwRegistry),   // 使用中间件时必填
    recipe.WithParams(map[string]any{...}),      // 定义了参数时填写
)

// 构建结果是标准的 blades.Agent
runner := blades.NewRunner(agent)
output, err := runner.Run(ctx, blades.UserMessage("..."))
stream := runner.RunStream(ctx, blades.UserMessage("..."))
```

## 示例

- [recipe-basic](../examples/recipe-basic/) — 带参数化提示词的单 Agent
- [recipe-sequential](../examples/recipe-sequential/) — 通过 output_key 传递数据的顺序流水线
- [recipe-tool](../examples/recipe-tool/) — 子 Agent + 函数工具的 LLM 驱动分发
