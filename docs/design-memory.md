---
type: design
title: Memory 系统
parent: design-agent-framework.md
date: 2026-05-01
status: draft
modules: [module-8]
---

# Memory 系统

Memory 为 AgentOS 提供跨 turn、跨 session 或跨应用的长期上下文能力。Core 只定义抽象接口和注入点，不规定文件名、目录布局、workspace 遍历策略或自动提取调度方式。

## 设计结论

- `memory/` 不依赖 root `blades.Agent`，也不直接依赖应用 workspace。
- Agent Loop 在构建模型上下文时调用 `memory.Loader` / `memory.Recaller`，把结果注入 prompt 或 `model.Message`。
- Memory 提取可以在 Agent 停止后同步执行，也可以由应用层异步 job 执行；core 不内置 `BackgroundAgent`。
- 文件 memory、`BLADES.md`、`.blades/`、include 指令和 workspace 向上遍历属于应用或 contrib 实现。

## Core Interfaces

```go
package memory

type Scope string

const (
    ScopeGlobal  Scope = "global"
    ScopeUser    Scope = "user"
    ScopeProject Scope = "project"
    ScopeSession Scope = "session"
    ScopeAgent   Scope = "agent"
)

type Item struct {
    ID       string
    Scope    Scope
    Topic    string
    Content  string
    Metadata map[string]any
}

type Store interface {
    Get(ctx context.Context, id string) (*Item, error)
    Put(ctx context.Context, item *Item) error
    Delete(ctx context.Context, id string) error
    Search(ctx context.Context, query Query) ([]*Item, error)
}

type Query struct {
    Scope    []Scope
    Topic    string
    Text     string
    Limit    int
    Metadata map[string]any
}
```

Core store 不规定后端。内存、文件、SQLite、vector database、remote memory service 都可以实现 `Store`。

## Loader 与 Recall

```go
type LoadRequest struct {
    SessionID string
    AgentName string
    Scopes    []Scope
    Query     string
}

type Loader interface {
    Load(ctx context.Context, req LoadRequest) ([]*Item, error)
}

type Recaller interface {
    Recall(ctx context.Context, req LoadRequest) ([]*Item, error)
}
```

`Loader` 用于稳定注入，例如用户偏好、项目约定和 session memory。`Recaller` 用于按当前任务动态召回。Agent Loop 只消费返回的 `Item`，不关心它们来自文件、数据库还是远程服务。

## Extractor

```go
type ExtractRequest struct {
    SessionID string
    AgentName string
    Messages  []*model.Message
    Existing  []*Item
}

type Extractor interface {
    Extract(ctx context.Context, req ExtractRequest) ([]*Item, error)
}
```

Core 不规定 extractor 是否调用 LLM。需要 LLM 的实现通过函数注入模型能力，避免 `memory/` 依赖 root Agent：

```go
type SummarizeFunc func(ctx context.Context, messages []*model.Message) (string, error)
```

应用层可以在 `hook.AgentEnd`、run manager cleanup 或异步 job 中调用 extractor，并把结果写回 store。

## Context Injection

Agent Loop 推荐把 memory 分为两类注入：

- Stable memory：进入 system prompt 或 `prompt.Builder` section，适合用户偏好、项目约定。
- Relevant memory：作为当前 turn 的 context message 或 prompt section，适合按任务召回的片段。

注入格式由 Agent Loop 或 `prompt.Builder` 控制，memory item 本身不携带 provider-specific schema。

```go
type Formatter interface {
    Format(ctx context.Context, items []*Item) (string, error)
}
```

## File Memory as App/Contrib

文件 memory 是推荐实现之一，但不属于 core contract。应用或 contrib 可以提供：

- `BLADES.md` / `.blades/BLADES.md` / `BLADES.local.md` 的发现与加载。
- `@include` 指令和循环检测。
- workspace 向上遍历、ignore 规则和安全边界。
- `~/.blades/memories/*.md` 这类自动提取文件。
- front matter、topic、tags、source 等文件格式约定。

推荐位置：

```go
package filememory

func NewStore(root string, opts ...Option) memory.Store
func NewLoader(store memory.Store, opts ...Option) memory.Loader
```

这样 core 保持通用，coding app 仍可复用类似 Claude Code 的项目 memory 体验。

## Session / Agent Memory

Session memory 和 Agent memory 都通过 `Scope` 表达，不需要独立 core package：

- `ScopeSession`：当前 session 的长期摘要、已确认事实、用户阶段性目标。
- `ScopeAgent`：某个 agent 的私有偏好、工具经验或 domain hint。
- `ScopeProject`：项目级共享约定，由应用决定是否来自文件。

如果需要快照或恢复，应用把对应 items 写入 session store 或 memory store；core 不规定快照文件格式。

## 关键设计决策

1. **抽象优先于文件约定**：core 定义 Store/Loader/Recaller/Extractor，文件布局留给 app/contrib。
2. **Memory 不依赖 Agent**：提取和摘要通过函数接口注入，避免循环依赖。
3. **异步提取在应用层**：core 不内置 BackgroundAgent 或 job scheduler。
4. **注入格式集中在 Agent Loop**：memory item 不泄漏 provider schema。
5. **Scope 是软边界**：权限、workspace 和可见性由应用 policy 解释，core 只保留结构化字段。
