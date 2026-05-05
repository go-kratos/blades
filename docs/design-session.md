---
type: design
title: 会话与持久化
parent: design-agent-framework.md
date: 2026-05-01
status: draft
modules: [module-5]
---

# 会话与持久化

### 现状对比

| 维度 | 当前 Blades | 新设计 |
|------|------------|--------|
| 接口 | 5 方法（ID/State/SetState/Append/History） | 7 方法（+Leaf/Branch） |
| 存储 | 仅内存（sessionInMemory） | JSONL 文件 + 内存双实现 |
| 结构 | 线性数组 | parentId 链形成消息树 |
| 分支 | 不支持 | 完整支持：分支创建、导航、摘要 |
| 压缩 | 挂在 Session 上（ContextCompressor） | 从 Session 移出到 Agent Loop（compact.Pipeline） |
| 压缩历史 | 丢弃 | 完整保留在 JSONL 中 |
| 并发安全 | sync.Mutex | Writer 单所有者 + 文件锁，保证单会话写入顺序 |
| 持久化接口 | 无 | Store（Open 模式，返回 Writer handle） |
| 会话列表 | 全量解析 | LiteReader 仅读头尾，跳过大块内容 |
| 写入模式 | 同步逐条 | BatchWriter 异步批量刷盘 |
| 大结果处理 | 无 | content_replacement 替换超限工具结果为存根 |

### 5.1 整体架构

```
┌─────────────────────────────────────────────────────┐
│  Agent Loop                                         │
│    session.Append(msg)                              │
│    messages := session.History()                    │
│    compressed := pipeline.Compress(messages)        │
│    session.Branch(nodeID)                           │
├─────────────────────────────────────────────────────┤
│  Session 接口（7 方法）                              │
│    ID / State / SetState                            │
│    Append / History                                 │
│    Leaf / Branch                                    │
├─────────────────────────────────────────────────────┤
│  实现层                                              │
│    ├── memorySession（纯内存，内部维护 Tree）         │
│    └── persistentSession（Store + 内存缓存）         │
│              │                                      │
│              ▼                                      │
│         Store 接口（Open 模式）                      │
│           ├── fileStore（JSONL）                     │
│           └── 其他实现（SQLite、S3 等）              │
└─────────────────────────────────────────────────────┘
```

核心设计原则：**Session 和 Store 职责分离**。

- **Session** 面向 Agent Loop，提供消息读写和树导航。Agent Loop 只依赖 Session 接口，不感知 Entry/Tree/JSONL 等持久化细节。
- **Store** 面向存储后端实现者，提供 Entry 级别的 CRUD。persistentSession 内部委托 Store 完成持久化。

### 5.2 session.Session 接口

```go
package session

// State 是会话级键值状态，支持任意类型的值。
// 框架内部 key 使用 __ 前缀（如 __compact_offset__），用户 key 不应使用此前缀。
type State map[string]any

// Session 是运行时会话接口，Agent Loop 的唯一依赖。
type Session interface {
    // ID 返回会话唯一标识。
    ID() string

    // State 返回会话状态的快照副本。
    State() State
    // SetState 设置会话状态中的一个键值对。
    SetState(key string, value any)

    // Append 追加一条消息到当前 Leaf 位置。
    // 新消息成为 Leaf 的子节点，并更新 Leaf 指针。
    Append(ctx context.Context, msg *model.Message) error

    // History 返回从 root 到当前 Leaf 的消息序列。
    // 如果存在 Compaction Entry，压缩边界前的消息被摘要替换。
    // History 不执行压缩——压缩由 Agent Loop 的 compact.Pipeline 负责。
    History(ctx context.Context) ([]*model.Message, error)

    // Leaf 返回当前位置的节点 ID。
    Leaf() string

    // Branch 将当前位置移动到指定节点。
    // 后续 Append 的消息将成为该节点的子节点。
    // 如果 nodeID 不存在，返回错误。
    Branch(nodeID string) error
}
```

**为什么 Leaf/Branch 在 Session 接口上？**

树是一等公民。即使简单 Agent 不调用 Branch，接口的存在保证了所有实现都支持树结构。memorySession 和 persistentSession 都完整实现树——内存中也维护 Tree 结构。

**为什么 History 不做压缩？**

压缩从 Session 移出到 Agent Loop（compact.Pipeline）。理由：
- 同一个 Session 可能被不同 Agent 共享（如 sub-agent），每个 Agent 可能有不同的压缩策略
- Session 是存储层，不应该知道 token 预算和模型上下文窗口
- 压缩结果（CompactionEntry）仍然写回 Session 作为持久化记录

调用链变为：
```
session.History() → 原始消息（含 Compaction 回放） → compact.Pipeline → model request
```

**为什么不暴露 AppendEntry？**

Session 接口只暴露 `Append(*model.Message)`，不暴露 `AppendEntry(Entry)`。Entry 是持久化层的概念，Agent Loop 不需要感知。Compaction Entry 等非消息类型的写入通过 persistentSession 的具体方法或内部机制处理，不污染 Session 接口。

### 5.3 session.Store 接口

```go
package session

// Store 是会话持久化的抽象接口。
type Store interface {
    // Open 打开一个会话用于写入。
    // 如果 id 对应的会话不存在则创建新会话（写入 Header）。
    // 如果已存在则打开现有文件追加写入。
    // 返回的 Writer 持有资源（文件句柄/连接），必须 Close。
    Open(ctx context.Context, id string, opts ...StoreOption) (Writer, error)

    // Load 加载会话的完整快照。
    Load(ctx context.Context, id string) (*Snapshot, error)

    // List 列出所有可用会话的元信息。
    List(ctx context.Context) ([]Header, error)
}

// Writer 是会话的写入句柄，持有底层资源。
type Writer interface {
    // Append 追加一条或多条 Entry 到会话。
    Append(ctx context.Context, entries ...Entry) error
    // Close 释放底层资源（文件句柄、锁等）。
    Close() error
}
```

**为什么用 Open 模式而不是 Create + Append？**

原设计中 `Create(header)` + `Append(sessionID, entries...)` 存在问题：
- 调用方需要管理两步操作，Create 和 Append 之间存在不一致窗口
- 每次 Append 都传 sessionID 是冗余的
- 没有明确的资源生命周期管理

Open 模式的优点：
- Writer 持有文件句柄/flock，生命周期明确
- Append 不需要每次传 sessionID——Writer 已经绑定了会话
- 新建和打开已有会话统一为一个操作，调用方不需要区分
- Close 释放资源，语义清晰，避免资源泄漏

### 5.4 StoreOption

```go
type StoreOption func(*storeOptions)

type storeOptions struct {
    CWD   string
    Title string
}

func WithCWD(cwd string) StoreOption {
    return func(o *storeOptions) { o.CWD = cwd }
}

func WithTitle(title string) StoreOption {
    return func(o *storeOptions) { o.Title = title }
}
```

StoreOption 仅在 Open 创建新会话时生效，用于设置 Header 中的 CWD 和 Title。

### 5.5 session.Entry 联合类型

```go
// Entry 是 JSONL 文件中每行的结构。
// 通过 ID + ParentID 构成树形结构。
type Entry struct {
    Type      EntryType       `json:"type"`
    ID        string          `json:"id"`
    ParentID  string          `json:"parentId,omitempty"`
    Timestamp int64           `json:"timestamp"`
    Data      json.RawMessage `json:"data"`
}

type EntryType string
const (
    EntryMessage            EntryType = "message"             // 对话消息
    EntryCompaction         EntryType = "compaction"           // 压缩摘要
    EntryConfigChange       EntryType = "config_change"        // 配置变更（State 持久化）
    EntryCustom             EntryType = "custom"               // 扩展自定义数据
    // EntryContentReplacement 替换之前写入的工具结果内容。
    // 当工具结果超过 ToolResultBudget 被持久化到磁盘时，
    // 原始 entry 中的完整结果被替换为截断预览 + 磁盘路径引用。
    // 会话恢复时回放 ContentReplacement 以还原存根。
    EntryContentReplacement EntryType = "content_replacement"
)
```

第一阶段实现 5 种核心 Entry 类型：

| EntryType | Data 结构 | 用途 |
|-----------|----------|------|
| `message` | `MessageData` | 对话消息（user/assistant/tool） |
| `compaction` | `CompactionData` | 压缩边界标记 + 摘要消息 |
| `config_change` | `ConfigChangeData` | State 变更持久化 |
| `content_replacement` | `ContentReplacementData` | 替换超限工具结果为截断存根 |
| `custom` | `json.RawMessage` | 扩展自定义数据 |

后续可按需添加的类型：`model_change`（模型切换）、`title`（会话标题更新）、`branch_summary`（分支摘要）。

```go
// MessageData 是 EntryMessage 的 Data 结构。
type MessageData struct {
    Message   *model.Message `json:"message"`
    AgentID   string         `json:"agentId,omitempty"`
    AgentName string         `json:"agentName,omitempty"`
}

// CompactionData 是 EntryCompaction 的 Data 结构。
type CompactionData struct {
    Summary          []*model.Message `json:"summary"`
    FirstKeptEntryID string           `json:"firstKeptEntryId"`
    TokensBefore     int64            `json:"tokensBefore"`
    TokensAfter      int64            `json:"tokensAfter"`
}

// ConfigChangeData 是 EntryConfigChange 的 Data 结构。
type ConfigChangeData struct {
    Key   string `json:"key"`
    Value any    `json:"value"`
}

// ContentReplacementData 是 EntryContentReplacement 的 Data 结构。
type ContentReplacementData struct {
    TargetEntryID string `json:"targetEntryId"` // 被替换的原始 entry ID
    StubContent   string `json:"stubContent"`   // 截断预览 + 磁盘路径
}
```

### 5.6 session.Header

```go
type Header struct {
    Version   int    `json:"version"`
    ID        string `json:"id"`
    CreatedAt int64  `json:"createdAt"`
    CWD       string `json:"cwd,omitempty"`
    Title     string `json:"title,omitempty"`
    Leaf      string `json:"leaf"` // 当前位置指针
}
```

JSONL 文件的第一行。`Leaf` 记录当前位置指针。元数据（Title、Leaf）采用 last-wins 读取策略——后续的 Entry 可以更新这些值。

### 5.7 消息树

```go
// Tree 通过 ParentID 链构建消息树。
type Tree struct {
    root     *TreeNode
    nodeByID map[string]*TreeNode
    leaf     string // 当前位置
}

type TreeNode struct {
    Entry    Entry
    Children []*TreeNode
    Parent   *TreeNode
}

// Leaf 返回当前 leaf 节点 ID。
func (t *Tree) Leaf() string

// Branch 移动 leaf 指针到指定节点。
// 后续通过 Add 添加的节点将以该节点为 parent。
func (t *Tree) Branch(nodeID string) error

// Path 返回从根到指定节点的 Entry 序列。
func (t *Tree) Path(nodeID string) []Entry

// Add 添加一个节点到树中。
// 如果 entry.ParentID 非空，节点成为对应 parent 的子节点。
// 如果 entry.ParentID 为空且树为空，节点成为根节点。
func (t *Tree) Add(entry Entry) error

// Rebuild 从 Entry 列表重建完整的树。
func Rebuild(entries []Entry, leaf string) (*Tree, error)
```

Tree 是内部数据结构，不在 Session 接口中暴露。memorySession 和 persistentSession 内部都维护 Tree 实例。

### 5.8 session.Snapshot

```go
// Snapshot 是加载会话后的完整快照。
type Snapshot struct {
    Header   Header
    Entries  []Entry           // 所有 Entry（用于重建 Tree）
    Messages []*model.Message  // 当前分支的消息序列（已回放 Compaction）
    State    State              // 会话状态（从 config_change 条目回放）
}
```

### 5.9 构造函数

```go
// NewMemory 创建纯内存 Session（测试、无状态场景、简单 Agent）。
func NewMemory(opts ...Option) Session

// New 创建持久化 Session（新建，通过 Store 写入）。
func New(store Store, opts ...Option) (Session, error)

// Open 恢复已有 Session（从 Store 加载快照 + 打开 Writer）。
func Open(store Store, sessionID string) (Session, error)

// NewFileStore 创建 JSONL 文件 Store。
func NewFileStore(baseDir string) Store
```

```go
type Option func(*options)

func WithID(id string) Option          // 自定义 Session ID
func WithCWD(cwd string) Option        // 工作目录
func WithTitle(title string) Option    // 会话标题
func WithState(state State) Option // 初始状态
```

### 5.10 文件实现

```go
// fileStore 使用 JSONL 文件实现 session.Store。
// 文件位置：~/.blades/sessions/<project-slug>/<sessionId>.jsonl
//
// JSONL 格式：
//   第 1 行：Header（JSON）
//   第 2+ 行：Entry（每行一个 JSON）
//
// 追加写入由 fileWriter 单所有者负责，进程间通过 flock 协调。
// 元数据（title、leaf）采用 last-wins 读取策略。
type fileStore struct {
    baseDir string
}

func NewFileStore(baseDir string) Store

// fileWriter 持有打开的文件句柄和 flock。
type fileWriter struct {
    file *os.File
}

func (w *fileWriter) Append(ctx context.Context, entries ...Entry) error
func (w *fileWriter) Close() error
```

并发安全策略：
- **写入**：`flock(2)` 文件锁，单次 `write()` syscall < PIPE_BUF 保证原子性
- **读取**：不需要锁（append-only 保证已写入的行不变）
- **崩溃恢复**：Load 时检测并跳过不完整的最后一行

### 5.11 会话恢复流程

```
1. 读取 JSONL 文件
2. 解析第一行为 Header
3. 解析后续每行为 Entry（跳过不完整的最后一行）
4. 通过 Rebuild(entries, header.Leaf) 重建 Tree
5. 调用 Tree.Path(leaf) 获取当前分支的 Entry 序列
6. 从 Entry 序列中提取 message 类型 → []*model.Message
7. 回放 Compaction：找到最近的 CompactionData，
   用 Summary 替换 FirstKeptEntryID 之前的所有消息
8. 回放 ContentReplacement：按序应用 content_replacement Entry，
   将目标 entry 的内容替换为截断存根（幂等操作，重复应用结果不变）
9. 回放 ConfigChange：按序应用所有 config_change Entry → State（last-wins）
10. 返回 Snapshot
```

### 5.12 persistentSession 实现

```go
// persistentSession 包装 Store，提供 Session 接口。
// 内部维护内存缓存（Tree + messages + state），避免每次 History 都读文件。
type persistentSession struct {
    id     string
    store  Store
    writer Writer

    mu       sync.RWMutex
    tree     *Tree
    messages []*model.Message // 当前分支的消息缓存（已回放 Compaction）
    state    State
}
```

操作语义：
- **Append(msg)**：创建 MessageEntry → Writer.Append + Tree.Add + 更新 messages 缓存
- **History()**：从内存缓存读取（读锁），不访问 Store
- **SetState(k, v)**：更新内存 state + Writer.Append(ConfigChangeEntry)
- **Branch(nodeID)**：Tree.Branch(nodeID) + 重建 messages 缓存（从 Tree.Path 重新提取）
- **Leaf()**：Tree.Leaf()

### 5.13 memorySession 实现

```go
// memorySession 是纯内存实现，内部维护完整的 Tree 结构。
// 不依赖 Store，适用于测试、无状态场景、简单 Agent。
type memorySession struct {
    id    string
    state State

    mu       sync.RWMutex
    tree     *Tree
    messages []*model.Message // 当前分支的消息缓存
}
```

memorySession 和 persistentSession 的 Tree 操作逻辑完全一致，区别仅在于 persistentSession 额外写穿到 Store。

### 5.14 State 管理

保持 `map[string]any`，加约定和泛型辅助：

```go
// 框架内部 key 用 __ 前缀（如 __compact_offset__）
// 用户 key 不应使用此前缀

// GetState 是类型安全的 State 读取辅助函数。
func GetState[T any](s Session, key string) (T, bool) {
    v, ok := s.State()[key]
    if !ok {
        var zero T
        return zero, false
    }
    typed, ok := v.(T)
    return typed, ok
}
```

持久化：每次 SetState 生成 `config_change` Entry 写入 Store。恢复时按序回放，last-wins。

注意：持久化的 State 值必须是 JSON 可序列化的。反序列化后 struct 类型会变成 `map[string]interface{}`，使用 `GetState[T]` 时需要注意类型匹配。

### 5.15 包结构

```
session/
├── session.go       // Session 接口 + NewMemory() + New() + Open()
│                    // + context 辅助函数（NewContext/FromContext/Ensure）
├── store.go         // Store 接口 + Writer 接口 + Header + Snapshot + StoreOption
├── entry.go         // Entry + EntryType + MessageData + CompactionData + ConfigChangeData
│                    // + ContentReplacementData
├── option.go        // Option + WithID/WithCWD/WithTitle/WithState
├── tree.go          // Tree + TreeNode + Path/Branch/Leaf/Add/Rebuild
├── memory.go        // memorySession（纯内存，内部维护 Tree）
├── persistent.go    // persistentSession（Store + 内存缓存 + Writer）
├── file.go          // fileStore（JSONL 读写 + flock）
├── lite_reader.go   // LiteReader（优化的会话列表读取，仅读头尾）
├── batch_writer.go  // BatchWriter（异步批量刷盘）
└── state.go         // GetState[T] 泛型辅助
```

### 5.16 Lite Reader

会话列表（`Store.List`）需要读取所有会话的元信息。对于大型 JSONL 文件，完整解析代价过高。LiteReader 提供优化的读取路径：仅读取文件头部（Header）和尾部窗口（最近的元数据行），跳过中间的大块内容。

```go
// --- Lite Reader ---

const LiteReadBufSize = 65536 // 64KB head+tail 窗口

// LiteReader 为大型 JSONL 文件提供优化的会话列表读取。
// 仅读取文件头部（Header）和尾部（最近元数据），
// 跳过 tool_result 内容和 compact 摘要。
type LiteReader struct {
    bufSize int // 默认 LiteReadBufSize
}

func NewLiteReader(opts ...LiteReaderOption) *LiteReader

// ReadLite 返回会话元数据，无需完整解析。
// 从文件头部读取 Header，从尾部读取最近的 title/tag/mode 等元数据。
func (r *LiteReader) ReadLite(path string) (*Header, error)

type LiteReaderOption func(*LiteReader)
func WithLiteBufSize(size int) LiteReaderOption
```

读取策略：
1. 读取文件前 N 字节，解析第一行为 Header
2. Seek 到文件末尾前 N 字节，扫描尾部的完整行
3. 从尾部行中提取 last-wins 元数据（title、leaf、tag 等）
4. 合并头部 Header 和尾部元数据，返回结果

### 5.17 Batch Writer

高频写入场景（如流式 assistant 响应中的多次 Append）下，逐条 `write()` syscall 开销显著。BatchWriter 在 Writer 之上提供异步批量写入能力。

```go
// --- Batch Writer ---

// BatchWriter 累积 entries 并批量刷盘。
// 减少高频写入场景下的系统调用开销。
type BatchWriter struct {
    inner         Writer
    queue         chan Entry
    batchSize     int           // 默认 10
    flushInterval time.Duration // 默认 100ms
    done          chan struct{}
}

func NewBatchWriter(inner Writer, opts ...BatchWriterOption) *BatchWriter

// Append 将 entry 放入队列，非阻塞。
// 队列满时阻塞直到有空间或 context 取消。
func (bw *BatchWriter) Append(ctx context.Context, entry Entry) error

// Drain 刷新所有待写入的 entries 并关闭 writer。
// 在会话结束前调用，确保所有数据持久化。
func (bw *BatchWriter) Drain(ctx context.Context) error

type BatchWriterOption func(*BatchWriter)
func WithBatchSize(n int) BatchWriterOption
func WithFlushInterval(d time.Duration) BatchWriterOption
```

刷盘触发条件（先到者触发）：
- 队列中累积的 entries 达到 `batchSize`
- 距离上次刷盘超过 `flushInterval`

BatchWriter 内部启动一个后台 goroutine 消费队列。`Drain` 发送关闭信号并等待后台 goroutine 退出，确保所有 entries 已持久化。

### 5.18 大文件优化

```go
// --- 大文件优化 ---

// SkipPrecompactThreshold 定义触发大文件优化的阈值。
// 超过此大小的 JSONL 文件使用 readTranscriptForLoad：
// 扫描 compact 边界偏移，仅读取边界后内容，
// 单独扫描边界前区域的元数据行。
const SkipPrecompactThreshold = 1 << 20 // 1MB
```

大文件加载策略（文件大小 > `SkipPrecompactThreshold`）：
1. 从文件尾部向前扫描，定位最近的 `compaction` Entry 偏移
2. 仅完整解析该偏移之后的内容（compact 边界后的活跃消息）
3. 单独扫描边界前区域，仅提取元数据行（config_change、content_replacement 等）
4. 跳过边界前的 message 和 tool_result 内容

这与 LiteReader 互补：LiteReader 用于列表场景（不需要消息内容），大文件优化用于加载场景（需要活跃消息但跳过已压缩的旧消息）。

### 关键设计决策

1. **Session + Store 分离** — Session 面向 Agent Loop（7 方法），Store 面向存储后端（Open/Load/List）。Agent Loop 不感知 Entry/Tree/JSONL 等持久化细节。这符合 Go 小接口原则，也使得 memorySession 不需要依赖 Store。

2. **Store 用 Open 模式** — 替代原设计的 Create + Append。Writer 持有文件句柄/flock，生命周期明确。新建和打开已有会话统一为一个操作。Close 释放资源，避免泄漏。

3. **树是一等公民** — Leaf/Branch 在 Session 接口上，所有实现都完整支持树结构。memorySession 和 persistentSession 内部都维护 Tree 实例。

4. **压缩从 Session 移出** — Session.History() 返回原始消息（含 Compaction 回放），压缩由 Agent Loop 的 compact.Pipeline 负责。同一个 Session 可被不同 Agent 共享，各自使用不同压缩策略。

5. **JSONL 追加写入** — 每个打开的会话由一个 `Writer` 持有文件句柄和 flock，进程内通过 single writer ownership 保证顺序，进程间通过文件锁避免交错写入。压缩时不删除旧消息，追加 CompactionData 标记边界。完整历史始终可从 JSONL 文件恢复。

6. **第一阶段 5 种 Entry 类型** — message、compaction、config_change、content_replacement、custom。其中 content_replacement 用于替换超限工具结果为截断存根。其他类型（model_change、title、branch_summary）后续按需添加，不影响已有数据格式。

7. **BatchWriter 使用后台 goroutine + flush interval** — 而非每次写入启动 goroutine。后台 goroutine 按 batchSize 或 flushInterval（先到者触发）批量刷盘，保证写入顺序与 Append 调用顺序一致。这避免了并发 goroutine 导致的乱序问题。

8. **LiteReader 以完整性换速度** — 仅读取文件头部和尾部窗口，跳过中间的大块内容（tool_result、compact 摘要等）。如果元数据（如 title）仅出现在文件中部且未在尾部重新追加，LiteReader 会丢失该信息。这是可接受的折衷——会话列表只需要基本元数据，完整信息在 Open 时加载。

9. **ContentReplacement 是回放安全的** — 对同一个 entry 重复应用 content_replacement 是幂等的：第二次应用时目标内容已经是存根，替换结果不变。这保证了崩溃恢复和重复回放的正确性。
