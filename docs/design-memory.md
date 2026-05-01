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

### 关键设计决策

1. **5 层层级而非单一存储** — 当前 `InMemoryStore` 是扁平的键值存储。新设计将 Memory 分为 5 层，从框架管理到自动提取，每层有明确的职责和优先级。项目级 Memory（BLADES.md）类似 Claude Code 的 CLAUDE.md，是团队共享的项目约定。

2. **@include 指令** — Memory 文件可以通过 `@include` 引用其他文件，支持模块化组织。例如项目根目录的 BLADES.md 可以 `@include` 子目录的特定约定文件，避免单文件过大。

3. **globs 条件注入** — 不是所有 Memory 都需要在每次对话中注入。通过 `globs` 字段，Memory 条目只在用户操作匹配的文件时才注入 system prompt，减少不必要的 token 消耗。

4. **自动提取互斥** — 如果主 Agent 在当前轮次已经写入了 Memory 文件（用户显式要求记住某事），自动提取器跳过本轮。避免主 Agent 和后台提取器同时写入同一文件产生冲突。
