---
type: design
title: Memory 系统
parent: design-agent-framework.md
date: 2026-05-01
status: draft
modules: [module-8]
---

## 模块 8：Memory 系统

### 现状对比

| 维度 | 当前 Blades | 新设计 |
|------|------------|--------|
| 存储 | InMemoryStore（子串搜索） | 5 层层级 Memory |
| 来源 | 单一内存 | Managed/User/Project/Local/Auto |
| 自动提取 | 无 | 后台 Fork Agent 自动提取 |
| 文件处理 | 无 | @include 解析 + 截断管线 |
| 注入策略 | 全量注入 | globs 条件注入 |

### 8.1 Memory 层级

```go
package memory

// Type 定义 Memory 条目的来源和优先级。
// 加载顺序（优先级从高到低）：
//   Managed → User → Project → Local → Auto
type Type string
const (
    Managed Type = "managed" // ~/.blades/BLADES.md（框架管理）
    User    Type = "user"    // ~/.blades/BLADES.md（用户编写）
    Project Type = "project" // CWD 向上遍历：BLADES.md, .blades/BLADES.md
    Local   Type = "local"   // CWD 向上遍历：BLADES.local.md
    Auto    Type = "auto"    // ~/.blades/memories/*.md（自动提取）
)

// Entry 表示一个加载的 Memory 文件。
type Entry struct {
    Path       string   `json:"path"`
    Type       Type     `json:"type"`
    Content    string   `json:"content"`
    RawContent string   `json:"rawContent"`
    Globs      []string `json:"globs,omitempty"` // 文件匹配模式，决定何时注入
    Parent     string   `json:"parent,omitempty"` // 父文件路径（@include 链）
}
```

### 8.2 memory.Loader

```go
// Loader 发现和加载所有来源的 Memory 文件。
type Loader struct {
    homeDir    string
    projectDir string
    maxDepth   int // @include 最大深度，默认 5
    maxChars   int // 每文件字符上限，默认 40000
}

func NewLoader(homeDir, projectDir string) *Loader

// Load 加载所有 Memory 条目。
func (l *Loader) Load(ctx context.Context) ([]Entry, error)

// LoadForFile 加载与指定文件匹配的 Memory 条目（基于 globs）。
func (l *Loader) LoadForFile(ctx context.Context, filePath string) ([]Entry, error)
```

#### Memory 文件处理管线

```
1. 从磁盘读取文件
2. 剥离 HTML 注释（<!-- ... -->）
3. 解析 YAML frontmatter（globs 等元数据）
4. 解析 @include 指令（最大深度 5）
   - @path        — 绝对路径
   - @./relative  — 相对于当前文件
   - @~/home      — home 目录相对
5. 截断到 maxChars（默认 40000）
6. 返回 memory.Entry
```

### 8.3 Memory 文件格式

```markdown
---
globs: ["*.go", "**/*_test.go"]
---

# 项目约定

- 使用 Go 1.24+
- 测试文件使用 table-driven tests
- 错误处理使用 fmt.Errorf + %w

@./coding-standards.md
@./architecture-decisions.md
```

### 8.4 自动 Memory 提取

```go
// Extractor 在每轮结束后 fire-and-forget 运行，
// 从对话中提取持久性事实写入 ~/.blades/memories/。
type Extractor struct {
    loader     *Loader
    forkConfig blades.ForkConfig
    memDir     string // ~/.blades/memories/
    throttle   *Throttle
}

func NewExtractor(loader *Loader, opts ...ExtractorOption) *Extractor

// Extract 启动后台提取。
// 如果主 Agent 已在当前轮次写入 Memory 文件，则跳过（互斥）。
func (e *Extractor) Extract(ctx context.Context, messages []*model.Message) *blades.BackgroundAgent

// Drain 等待进行中的提取完成（关闭前调用）。
func (e *Extractor) Drain(timeout time.Duration) error
```

提取流程：

```
1. 检查节流（避免过于频繁提取）
2. 检查主 Agent 是否已写入 Memory（互斥）
3. Fork 新 Agent（QuerySource: extract_memory）
   - 工具限制：只读工具 + Memory 目录写入
   - 共享 prompt cache 前缀
4. 从对话中提取持久性事实
   - 用户偏好、项目约定、架构决策
   - 排除：临时状态、调试信息、代码片段
5. 写入 ~/.blades/memories/<topic>.md
   - 更新已有文件或创建新文件
   - 使用 YAML frontmatter 标记类型和描述
```

### 8.5 Memory 注入 System Prompt

```go
// Section 是 prompt.Builder 的动态 section。
// 根据当前工作目录和文件上下文，选择性注入 Memory 内容。
type Section struct {
    loader *Loader
}

func (s *Section) Build(ctx context.Context) (string, error) {
    entries, err := s.loader.Load(ctx)
    if err != nil {
        return "", err
    }

    var sb strings.Builder
    for _, entry := range entries {
        // 按类型分组，高优先级在前
        fmt.Fprintf(&sb, "# %s (%s)\n%s\n\n", entry.Path, entry.Type, entry.Content)
    }
    return sb.String(), nil
}
```

### 8.6 Session Memory

Session Memory 是会话级摘要，在自然断点（无工具调用的轮次结束时）更新，用于 compact 捷径（跳过 LLM 摘要调用）。

```go
// SessionMemory 管理每会话的摘要状态。
// 在自然断点更新（!hasToolCallsInLastTurn），
// 用作 compact 捷径避免昂贵的 LLM 摘要调用。
type SessionMemory struct {
    sessionID string
    memDir    string // ~/.blades/sessions/<project>/<sessionId>/memory/
    counter   model.Counter
    config    SessionMemoryConfig
}

type SessionMemoryConfig struct {
    MinTokensToInit         int64 // 初始化最小 token 数，默认 10_000
    MinTokensBetweenUpdate  int64 // 更新间隔最小 token 数，默认 5_000
    ToolCallsBetweenUpdates int   // 更新间隔最小工具调用数，默认 3
}

func NewSessionMemory(sessionID, memDir string, counter model.Counter, opts ...SessionMemoryOption) *SessionMemory

// ShouldUpdate 检查阈值决定是否需要刷新 session memory。
// 仅在自然断点调用（hasToolCallsInLastTurn == false）。
func (sm *SessionMemory) ShouldUpdate(tokensSinceLastUpdate int64, toolCallsSince int, hasToolCallsInLastTurn bool) bool

// Update 运行 forked agent 更新 session memory 文件。
// fork 仅允许 FileEdit 工具，作用域限定为 memory 文件路径。
// 文件权限：目录 0o700，文件 0o600。
func (sm *SessionMemory) Update(ctx context.Context, messages []*model.Message) error

// Load 返回当前 session memory 内容，未初始化时返回空字符串。
func (sm *SessionMemory) Load() (string, error)

// IsActive 返回 session memory 是否已初始化。
// 用于 SessionMemoryCompactStrategy 判断是否可以跳过 LLM 调用。
func (sm *SessionMemory) IsActive() bool
```

Integration: Stop Hooks 触发 SessionMemory.Update()；compact.SessionMemoryCompactStrategy 调用 SessionMemory.Load() 作为摘要。

### 8.7 Agent Memory

每种 agent 类型可拥有独立的持久化 Memory，三级作用域：

```go
// AgentMemoryScope 定义 agent memory 的存储位置。
type AgentMemoryScope string
const (
    AgentScopeUser    AgentMemoryScope = "user"    // ~/.blades/agent-memories/<agentType>/
    AgentScopeProject AgentMemoryScope = "project"  // <cwd>/.blades/agent-memories/<agentType>/
    AgentScopeLocal   AgentMemoryScope = "local"    // <cwd>/.blades/agent-memories/<agentType>/local/
)

// AgentMemory 提供每种 agent 类型的持久化 Memory。
type AgentMemory struct {
    AgentType string
    Scope     AgentMemoryScope
    BaseDir   string
}

func NewAgentMemory(agentType string, scope AgentMemoryScope, baseDir string) *AgentMemory

// Load 返回该 agent 类型的所有 memory 条目。
func (am *AgentMemory) Load(ctx context.Context) ([]Entry, error)

// Connect 将 agent memory 附加到 prompt builder 作为动态 section。
func (am *AgentMemory) Connect(builder *prompt.Builder) prompt.Section
```

路径清理：`:` 替换为 `-`（插件命名空间）。`isAgentMemoryPath()` 规范化路径防止 `..` 遍历。

### 8.8 Agent Memory 快照

Agent Memory 快照使 agent memory 成为可分发资产——项目可随 agent 定义一起发布初始 memory：

```go
// SnapshotState 追踪初始化状态。
type SnapshotState string
const (
    SnapshotNone         SnapshotState = "none"           // 无快照
    SnapshotInitialize   SnapshotState = "initialize"     // 需要从快照初始化
    SnapshotPromptUpdate SnapshotState = "prompt-update"  // 需要更新 prompt
)

// InitializeFromSnapshot 将快照文件复制到 agent memory 目录。
// 快照存储在 <cwd>/.blades/agent-memory-snapshots/<agentType>/
// 目前仅适用于 AgentScopeUser 作用域的 agent。
func InitializeFromSnapshot(agentType, snapshotDir, targetDir string) error

// ReplaceFromSnapshot 先删除已有 .md 文件，再从快照初始化。
func ReplaceFromSnapshot(agentType, snapshotDir, targetDir string) error
```

### 8.9 Relevant Memory Recall

不是将所有 memory 注入 prompt，而是通过轻量模型查询选择最相关的 top-N：

```go
// Recaller 为当前上下文选择最相关的 memory 条目。
// 避免将所有 memory 注入 system prompt 浪费 token。
type Recaller struct {
    loader    *Loader
    model     model.Provider // 轻量/快速模型
    maxRecall int            // 最大召回数，默认 5
}

func NewRecaller(loader *Loader, model model.Provider, opts ...RecallerOption) *Recaller

// Recall 返回与当前上下文最相关的 top-N memory 条目。
// 流程：
//   1. 扫描所有 memory 文件头部，格式化清单
//   2. 调用轻量模型侧查询，选择最相关的条目
//   3. 过滤 alreadySurfaced 避免重复
//   4. 排除 BLADES.md 本身（已在 system prompt 中）
func (r *Recaller) Recall(
    ctx context.Context,
    messages []*model.Message,
    alreadySurfaced map[string]bool,
) ([]Entry, error)

type RecallerOption func(*Recaller)
func WithMaxRecall(n int) RecallerOption
```

### 8.10 更新：Memory 注入策略

更新 Section.Build() 支持两种注入模式：

```go
type Section struct {
    loader   *Loader
    recaller *Recaller // 可选，nil 时回退到全量注入
}

func (s *Section) Build(ctx context.Context) (string, error) {
    if s.recaller != nil {
        // 选择性注入：轻量模型查询选择 top-N
        entries, err := s.recaller.Recall(ctx, currentMessages, alreadySurfaced)
        // ...
    }
    // 回退：全量注入所有匹配的 memory
    entries, err := s.loader.Load(ctx)
    // ...
}
```

### 关键设计决策

1. **5 层层级而非单一存储** — 当前 `InMemoryStore` 是扁平的键值存储。新设计将 Memory 分为 5 层，从框架管理到自动提取，每层有明确的职责和优先级。项目级 Memory（BLADES.md）类似 Claude Code 的 CLAUDE.md，是团队共享的项目约定。

2. **@include 指令** — Memory 文件可以通过 `@include` 引用其他文件，支持模块化组织。例如项目根目录的 BLADES.md 可以 `@include` 子目录的特定约定文件，避免单文件过大。

3. **globs 条件注入** — 不是所有 Memory 都需要在每次对话中注入。通过 `globs` 字段，Memory 条目只在用户操作匹配的文件时才注入 system prompt，减少不必要的 token 消耗。

4. **自动提取互斥** — 如果主 Agent 在当前轮次已经写入了 Memory 文件（用户显式要求记住某事），自动提取器跳过本轮。避免主 Agent 和后台提取器同时写入同一文件产生冲突。

5. **四层 Memory 架构** — 不是单一存储，而是四层架构：文件 Memory（BLADES.md 5 层层级）、Session Memory（会话级摘要）、Agent Memory（每种 agent 类型持久化）、Team Memory（团队共享，未来扩展）。每层有独立的生命周期和更新策略。

6. **选择性召回而非全量注入** — 当 memory 文件数量增长后，全量注入会浪费大量 token。Recaller 通过轻量模型查询选择 top-5 最相关的 memory，显著降低 token 消耗。当 Recaller 未配置时，回退到全量注入保持向后兼容。

7. **Session Memory 作为 compact 捷径** — Session Memory 的核心价值不仅是持久化会话摘要，更是作为 compact 捷径：当 session memory 已激活时，SessionMemoryCompactStrategy 直接使用它作为摘要，跳过昂贵的 LLM 调用。这是一个显著的成本优化。

8. **Agent Memory 快照可分发** — 项目可以在 `.blades/agent-memory-snapshots/` 中发布初始 memory，新用户首次使用时自动初始化。这使 agent 的知识积累可以跨团队共享。
