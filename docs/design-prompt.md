---
type: design
title: Prompt 系统
parent: design-agent-framework.md
date: 2026-05-01
status: draft
modules: [module-2]
---

# Prompt 系统

`prompt/` 负责构建 system prompt。它从根包移出，避免 `blades/` 因 prompt section、cache breakpoint、动态 provider 等细节膨胀。

## API

```go
package prompt

type Builder struct {
    static  []Section
    dynamic []Section
}

type Section struct {
    Name      string
    Priority  int
    Cacheable bool
    Build     func(ctx context.Context) (string, error)
}

type SystemPrompt struct {
    Static      string
    Dynamic     string
    Full        string
    Breakpoints []Breakpoint
}

type Breakpoint struct {
    Offset int
    Scope  CacheScope
}

type CacheScope string

const (
    ScopeGlobal  CacheScope = "global"
    ScopeSession CacheScope = "session"
)
```

`Builder` 按 priority 稳定排序 section。静态 section 放在可缓存前缀，动态 section 放在后缀。工具描述、行为规范、安全规则属于静态 section；memory、环境信息、MCP 指令、技能列表属于动态 section。

## 与 Agent Loop 的关系

Agent 构造时通过 option 注入 `*prompt.Builder`。`internal/loop.ContextBuilder` 调用 prompt builder，把结果写入 `model.Request.System` 和 `model.Request.Cache`。`prompt/` 不导入 `model/`，cache breakpoint 到 provider-specific cache control 的转换由 Agent Loop 或 provider adapter 完成。

## 边界

- 根包不导出 `PromptBuilder`、`PromptSection` 或 prompt 构造 helper。
- 应用可以把 mode、workspace、memory、skill 等内容注册为 section，但这些应用概念不进入 `prompt/` 类型系统。
- Section 的 `Build` 接收 `context.Context`，用于取消、deadline、trace 和 typed capability；不得依赖全局变量读取会话状态。
- prompt builder 不负责 tool schema 排序；工具声明属于 `tools/` 到 `model.ToolSpec` 的映射。

## 设计决策

1. **独立包而非根包类型**：`prompt.Builder` 读法清晰，也让根包保持最小。
2. **section 是函数而非接口**：函数足够表达动态 prompt，避免为简单拼接引入宽接口。
3. **缓存信息保持 provider-neutral**：Provider cache control 由 contrib provider 解释，不污染 prompt 包。
