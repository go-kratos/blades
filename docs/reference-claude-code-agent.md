---
type: reference
title: Claude Code Agent 参考设计
date: 2026-05-02
status: draft
author: chenzhihui
related: [reference-pi-agent-framework.md]
tags: [agent, framework, architecture, context-management, memory, tools, permissions, hooks, sandbox, multi-agent, plugins]
source:
  project: Claude Code
  url: https://github.com/anthropics/claude-code
  version: local
---

# Claude Code Agent 参考设计

## 参考来源

- **项目**: Claude Code（Anthropic 官方 CLI Agent）
- **链接**: https://github.com/anthropics/claude-code
- **版本/提交**: local（/Users/chenzhihui/Workspace/AgentOS/claude-code）

Claude Code 是 Anthropic 基于 TypeScript/Bun 构建的 Agentic Coding Assistant。核心是一个流式多轮 Agent Loop，编排模型调用、工具执行、上下文管理、Memory、权限和 Hook 系统。代码围绕少量高杠杆抽象构建，组合性强。同一内核支持多种运行时形态：交互式 REPL/TUI、无头 SDK、MCP Server 模式、Remote/Bridge 模式。

---

## 原始设计分析

### 核心设计思路

Claude Code 的架构围绕一个核心循环展开：**query loop**。所有功能（工具执行、上下文压缩、Memory 提取、子 Agent 派生）都是这个循环的扩展点，而非独立子系统。

关键设计原则：

1. **每轮状态不可变** — `State` 对象在每次循环迭代中重建，不原地修改消息数组
2. **缓存感知的 Fork** — 子 Agent 共享父 Agent 的 prompt cache 前缀，使压缩和 Memory 提取成本低廉
3. **分层权限决策** — 权限经过 规则 → 模式 → Hook → 分类器 → 用户提示 的链式判断，每层可短路
4. **追加式会话记录** — JSONL 追加写入，元数据 last-wins 读取，并发写入安全
5. **编译时特性门控** — `bun:bundle` feature flag 在构建时消除死代码路径
6. **工具并发分区** — 工具自声明 `isConcurrencySafe()`，编排器自动分批
7. **Fire-and-forget 后台任务** — Memory 提取和任务摘要作为 fork agent 运行，不阻塞主轮次
8. **Fail-closed 默认值** — 工具默认非并发安全、非只读；沙箱默认启用；权限默认 ask
9. **Prompt Cache 作为一等工程关注点** — 静态/动态分界、section 级缓存、fork agent 复用父级渲染字节

### 整体架构

#### 六层架构

```
┌─────────────────────────────────────────────────────────────┐
│  Layer 1: CLI Bootstrap                                      │
│    entrypoints/cli.tsx → main.tsx                            │
│    早期快速路径：--version, --dump-system-prompt,            │
│    remote-control, daemon/bg/runner（避免加载完整应用）       │
├─────────────────────────────────────────────────────────────┤
│  Layer 2: Initialization                                     │
│    init.ts + setup.ts                                        │
│    分为 pre-trust 和 post-trust 两阶段；                     │
│    环境变量仅在信任建立后完全应用                             │
├─────────────────────────────────────────────────────────────┤
│  Layer 3: Control/Command                                    │
│    commands.ts                                               │
│    特性门控命令，内部命令通过 USER_TYPE === 'ant' 过滤       │
├─────────────────────────────────────────────────────────────┤
│  Layer 4: TUI/REPL                                           │
│    REPL.tsx + AppStateStore.ts                               │
│    AppState 共享状态总线（~20+ 字段）                        │
├─────────────────────────────────────────────────────────────┤
│  Layer 5: Query/Agent Execution Kernel                       │
│    QueryEngine.ts → query.ts → queryLoop()                  │
│    核心 Agentic 循环：流式 API → 提取 tool_use →            │
│    执行工具 → 追加 tool_result → 循环                       │
├─────────────────────────────────────────────────────────────┤
│  Layer 6: Extension Layer                                    │
│    ├── Tools + Permissions + Sandbox                        │
│    ├── Memory + Session Persistence                         │
│    ├── MCP / Remote / Swarm / Plugins                       │
│    └── Hooks（27 种事件）                                   │
└─────────────────────────────────────────────────────────────┘
```

#### 多运行时形态

同一内核支持四种运行时形态：

| 形态 | 入口 | 说明 |
|------|------|------|
| REPL/TUI | `entrypoints/cli.tsx` | 默认交互式模式，基于 Ink/React |
| Headless/SDK | `QueryEngine` | 无 UI 依赖，供 SDK 调用方使用 |
| MCP Server | `entrypoints/mcp.ts` | 将内部工具重新暴露为 MCP tool schema |
| Remote/Bridge | `bridge/bridgeMain.ts` | WebSocket 连接远程编排器 |

---

### QueryEngine（QueryEngine.ts）

`QueryEngine`（~1295 行）是 `query()` 之上的编排层，为 SDK/Headless 路径提供类封装。

```typescript
class QueryEngine {
  // 每次 submitMessage() 启动一个新轮次
  submitMessage(prompt, options?): AsyncGenerator<SDKMessage>

  // 内部状态
  mutableMessages: Message[]
  abortController: AbortController
  permissionDenials: Map<string, number>
  totalUsage: TokenUsage
  readFileState: Map<string, string>
  discoveredSkillNames: Set<string>
  loadedNestedMemoryPaths: Set<string>
}
```

`QueryEngineConfig` 包含：cwd、tools、commands、mcpClients、agents、canUseTool、initialMessages、customSystemPrompt、appendSystemPrompt、userSpecifiedModel、fallbackModel、thinkingConfig、maxTurns、maxBudgetUsd、taskBudget、jsonSchema、verbose、replayUserMessages、handleElicitation 等。

QueryEngine 是 SDK 入口与原始 `query()` 循环之间的桥梁，处理会话持久化、模型选择、thinking 配置和 SDK 消息映射。

---

### Agent Loop（query.ts）

Agent Loop 是整个系统的心脏。`query.ts`（~1729 行）是一个 `AsyncGenerator`，驱动所有模型调用、工具执行和状态转换。

#### 入口结构

```typescript
query()           // 外层 generator：setup、hooks、attachments
  └── queryLoop() // 内层 while(true)：实际迭代

// query/ 目录拆分模块：
query/
  ├── config.ts        // buildQueryConfig() — 快照不可变环境/statsig/session 状态
  ├── deps.ts          // QueryDeps 接口 + productionDeps() 工厂（依赖注入/可测试性）
  ├── stopHooks.ts     // handleStopHooks() — 循环结束时触发 extractMemories、autoDream、promptSuggestion
  └── tokenBudget.ts   // createBudgetTracker() — +500k auto-continue 特性的 token 预算追踪
```

#### 状态对象

```typescript
type State = {
  messages: Message[]
  toolUseContext: ToolUseContext
  autoCompactTracking: AutoCompactTracking
  maxOutputTokensRecoveryCount: number
  hasAttemptedReactiveCompact: boolean
  pendingToolUseSummary: ToolUseSummary | null
  stopHookActive: boolean
  turnCount: number
  transition: Transition | null
}
```

每次迭代产生新的 `State` 对象（`state = { ... }` 赋值模式），不原地修改。`taskBudgetRemaining` 跨压缩边界追踪。

#### 单次迭代流程

```
1. Apply tool result budget     ← 裁剪超大工具结果
2. Snip                         ← 硬限制：丢弃最旧消息（95% 阈值）
3. Microcompact                 ← 小窗口内联压缩
4. Context collapse             ← 特性门控的分段折叠（UUID 范围）
5. Autocompact                  ← token 阈值触发完整压缩（85% 阈值）
6. Call model                   ← 流式 API 调用，yield StreamEvent
7. Execute tools                ← StreamingToolExecutor 流式执行 + 并发/串行分批
8. Get attachments              ← 文件读取、环境状态
9. Build next State, continue   ← 判断是否需要 follow-up（Terminal | Continue）
```

#### 终止条件

Loop 通过返回 `Terminal` 判别联合类型终止：

```typescript
type Terminal = {
  reason:
    | 'completed'              // 模型正常结束
    | 'aborted_streaming'      // 流式中止
    | 'aborted_tools'          // 工具执行中止
    | 'prompt_too_long'        // 上下文超限
    | 'blocking_limit'         // 阻塞限制
    | 'max_turns'              // 最大轮次
    | 'hook_stopped'           // Hook 中止
    | 'stop_hook_prevented'    // Stop hook 阻止
    | 'model_error'            // 模型错误
}
```

#### 恢复路径

| 条件 | 恢复策略 |
|------|---------|
| `max_output_tokens` | 升级到 64K tokens（`ESCALATED_MAX_TOKENS`），重试（最多 `MAX_OUTPUT_TOKENS_RECOVERY_LIMIT = 3` 次） |
| 多轮失败 | 最多 3 次 continuation 尝试 |
| `prompt_too_long` API 错误 | Reactive compact 后重试；每次重试截断 20% 最旧消息组（`truncateHeadForPTLRetry`） |
| Context collapse drain | 派生 ctx-agent 等待解决 |
| Auto-compact 连续失败 | 熔断器：`MAX_CONSECUTIVE_AUTOCOMPACT_FAILURES = 3` 次后禁用本会话 auto-compact |

#### QuerySource 标记

每个 query 携带来源标记，用于区分行为（影响重试策略、权限模式等）：

- `repl_main_thread` — 交互式用户输入
- `agent:*` — 子 Agent（AgentTool）
- `compact` — 压缩摘要生成
- `extract_memories` — 自动 Memory 提取 fork
- `sdk` — SDK 调用方
- `task_summary` — 周期性任务摘要 fork

前台查询源（`repl_main_thread`、`sdk`、`agent`、`compact`）在 529 过载时重试；后台查询源立即失败，避免 3-10x 网关放大效应。

#### 特性门控

`query.ts` 大量使用 `feature()` 进行编译时特性门控：

```typescript
// bun:bundle 在构建时消除死代码路径
feature('REACTIVE_COMPACT')    // 响应式压缩
feature('CONTEXT_COLLAPSE')    // 上下文折叠
feature('HISTORY_SNIP')        // 历史裁剪
feature('BG_SESSIONS')         // 后台会话
feature('EXTRACT_MEMORIES')    // 自动 Memory 提取
feature('FORK_SUBAGENT')       // Fork 子 Agent
feature('COORDINATOR_MODE')    // 协调器模式
```

特性门控代码使用条件 `require()` 而非顶层 import，使 bundler 可以 tree-shake 整个模块。

---

### 上下文管理

Claude Code 采用**多策略分层压缩**，每种策略在不同粒度和时机触发：

| 策略 | 触发时机 | 作用范围 | 说明 |
|------|---------|---------|------|
| Tool Result Budget | 每轮开始 | 单个工具结果 | 超大结果写入磁盘，发送预览（`TOOL_RESULT_BUDGET_CHARS = 30_000`） |
| Snip | 每轮开始 | 最旧消息 | 硬限制丢弃（`SNIP_THRESHOLD_RATIO = 0.95`） |
| Microcompact | 每轮开始 | 小窗口旧消息 | 内联摘要替换（`MICROCOMPACT_WINDOW_SIZE = 3`） |
| Context Collapse | 每轮开始 | 指定 UUID 范围 | 分段折叠（特性门控），UUID 范围边界 |
| Autocompact | token 阈值 | 全部/部分对话 | 完整 LLM 摘要（`AUTOCOMPACT_THRESHOLD_RATIO = 0.85`） |
| Session Memory Compact | 会话 Memory 存在时 | 全部对话 | 跳过额外 API 调用，直接使用已有 session memory 作为摘要 |
| Reactive Compact | API 413 错误 | 全部对话 | 紧急恢复 |

#### 关键常量

```
MODEL_CONTEXT_WINDOW_DEFAULT     = 200_000    // 默认上下文窗口
CAPPED_DEFAULT_MAX_TOKENS        = 8_000      // 槽位预留优化（BQ p99 输出 = 4,911 tokens）
ESCALATED_MAX_TOKENS             = 64_000     // max_output_tokens 重试上限
MAX_OUTPUT_TOKENS_FOR_SUMMARY    = 20_000     // 压缩摘要生成预留
AUTOCOMPACT_BUFFER_TOKENS        = 13_000     // 自动压缩触发缓冲
WARNING_THRESHOLD_BUFFER_TOKENS  = 20_000     // 上下文警告阈值
POST_COMPACT_MAX_FILES_TO_RESTORE = 5         // 压缩后恢复文件数
POST_COMPACT_TOKEN_BUDGET        = 50_000     // 压缩后文件恢复 token 预算
TOOL_RESULT_BUDGET_CHARS         = 30_000     // 单工具结果字符上限
MICROCOMPACT_WINDOW_SIZE         = 3          // 微压缩窗口大小
MAX_CONSECUTIVE_AUTOCOMPACT_FAILURES = 3      // 自动压缩熔断器
```

有效上下文窗口计算：`getContextWindowForModel(model) - min(getMaxOutputTokensForModel(model), 20_000)`

#### 完整压缩流程（Autocompact）

```
1. 运行 PreCompact hooks
2. 调用 runForkedAgent() 生成摘要（共享 prompt cache — "perfect fork"）
   - 压缩 prompt 使用 NO_TOOLS_PREAMBLE 禁止所有工具调用
   - 要求 <analysis> 分析块 + <summary> 摘要块
   - <analysis> 块在摘要到达上下文前被剥离
   - 两种变体：BASE（全对话）和 PARTIAL（近期消息）
3. 清除 readFileState
4. 构建压缩后消息：[boundaryMarker, ...summaryMessages, ...messagesToKeep, ...attachments, ...hookResults]
5. 恢复最近读取的文件（最多 5 个，50K token 预算，每文件 5K 上限）
6. 恢复活跃 plan 和 skill 状态
7. 恢复延迟的工具声明（MCP 工具等）
8. 运行 SessionStart hooks（重新注入环境）
9. 运行 PostCompact hooks
```

#### Session Memory 压缩捷径

`trySessionMemoryCompaction()` — 如果 session memory 已激活，跳过额外 API 调用，直接使用已有 session memory 文件作为摘要：

- `calculateMessagesToKeepIndex` 保留最小 token 下限（10K-40K 可配置）
- `adjustIndexToPreserveAPIInvariants` 防止在 `tool_use`/`tool_result` 对或共享 `message.id` 的 thinking 流中间切割

#### 熔断器

`MAX_CONSECUTIVE_AUTOCOMPACT_FAILURES = 3` — 连续 3 次自动压缩失败后，本会话完全禁用 auto-compact。防止无限重试循环（每天节省约 250K 死锁 API 调用）。

#### Forked Agent 模式

`runForkedAgent()` 创建轻量级 agent fork，共享父 agent 的 prompt cache 前缀（cache-key 匹配）。用于：
- 压缩摘要生成
- 自动 Memory 提取
- 任务摘要生成
- Context collapse agent
- Auto-dream（Memory 整合）

避免昂贵操作的冷缓存未命中。

---

### System Prompt 构建

System prompt 采用**六层 prompt 系统**，被分为**静态可缓存前缀**和**动态后缀**两部分，中间用 `__SYSTEM_PROMPT_DYNAMIC_BOUNDARY__` 分隔。

#### Prompt 优先级（`buildEffectiveSystemPrompt()`）

```
0. overrideSystemPrompt       ← 完全替换一切
1. coordinatorSystemPrompt    ← 协调器模式激活时
2. agentSystemPrompt          ← 替换默认 prompt（非追加）
3. customSystemPrompt         ← 替换默认 prompt（非追加）
4. defaultSystemPrompt        ← 兜底
+ appendSystemPrompt          ← 始终追加，无论来源
```

关键设计：`customSystemPrompt` 和 `agentSystemPrompt` **替换**默认 prompt，不是追加。只有 `appendSystemPrompt` 是纯追加的。

#### 静态部分（跨会话缓存）

```
getSimpleIntroSection()       ← "You are an interactive agent…"
getSimpleSystemSection()      ← 工具使用规则、system-reminder 标签
getSimpleDoingTasksSection()  ← 代码风格、任务方法、安全指导
getActionsSection()           ← 可逆性/影响范围指导
getUsingYourToolsSection()    ← 优先使用专用工具、并行调用
getSimpleToneAndStyleSection()← 简洁、无 emoji、file:line 引用
getOutputEfficiencySection()  ← 简洁规则
```

静态部分使用 `scope: 'global'` cache control（跨组织可缓存）。

#### 动态部分（每会话变化）

通过 `systemPromptSection()` 注册表管理（默认可缓存）：

- `session_guidance` — Agent 工具指导、技能调用说明
- `memory` — CLAUDE.md 文件内容
- `env_info_simple` — CWD、git 状态、OS、模型名
- `language` — 语言偏好
- `output_style` — 输出风格
- `mcp_instructions` — MCP 服务器指令（**不可缓存** — 使用 `DANGEROUS_uncachedSystemPromptSection`）
- `scratchpad` — 草稿目录说明
- `ant_model_override` — 内部模型覆盖
- `frc` — 功能相关上下文
- `summarize_tool_results` — 工具结果摘要指令

缓存失效时机：`/clear`、`/compact`、worktree 切换、resume。

#### Prompt Cache 纪律

```typescript
systemPromptSection(name, compute)                    // 默认可缓存
DANGEROUS_uncachedSystemPromptSection(name, compute, reason)  // 显式标记缓存破坏
```

`SYSTEM_PROMPT_DYNAMIC_BOUNDARY` 分隔稳定前缀和会话变化后缀。Fork 子 agent 复用父级已渲染的 prompt 字节（不重新生成），保证 prompt cache 命中稳定性。

#### 工具 Prompt 贡献

每个工具通过 `prompt(context)` 方法贡献自己的描述到 system prompt。工具按名称排序以保证 prompt cache 稳定性。

#### 任务特定 Prompt

| 任务 | 约束 |
|------|------|
| Compact | `NO_TOOLS_PREAMBLE` 禁止所有工具调用，要求 `<analysis>` + `<summary>` 块 |
| Session Memory | 仅允许 `Edit` 工具，不得修改 section headers |
| Memory Extraction | 有限工具集（Read/Grep/Glob/只读 Bash/Edit/Write），禁止 MCP/Agent/可写 Bash |

#### 可观测性

- `dump-prompts` 将 API 请求写入 `~/.claude/dump-prompts/<id>.jsonl`
- `/context` 命令分析每个 section 的 token 开销

---

### Memory 系统

#### 四层 Memory 架构

Claude Code 的 Memory 不是单一数据库，而是四层文件系统架构：

| 层 | 用途 | 作用域 |
|----|------|--------|
| Auto Memory | 长期用户/项目协作知识 | 全局（`~/.claude/memories/`） |
| Session Memory | 当前会话摘要，用于 compact 稳定性 | 会话级 |
| Agent Memory | 每种 agent 类型的持久化 Memory | Agent 类型级 |
| Team Memory | 共享仓库级知识，支持 pull/push/checksum/乐观锁 | 团队级 |

#### CLAUDE.md 层级

加载顺序（优先级从高到低）：

```
Managed   → ~/.claude/CLAUDE.md（由 Claude 管理）
User      → ~/.claude/CLAUDE.md（用户编写）
Project   → 从 CWD 向上遍历：CLAUDE.md, .claude/CLAUDE.md
Local     → 从 CWD 向上遍历：CLAUDE.local.md, .claude/CLAUDE.local.md
AutoMem   → ~/.claude/memories/*.md（自动提取）
TeamMem   → 团队共享 Memory（特性门控）
```

#### Memory 文件处理管线

```
1. 从磁盘读取文件（buildMemoryPrompt() 使用 readFileSync — React 渲染路径不能 await）
2. 剥离 HTML 注释
3. 解析 YAML frontmatter（globs 等）
4. 解析 @include 指令（最大深度 5）
   - @path        — 绝对路径
   - @./relative  — 相对于文件
   - @~/home      — home 相对
   - @/absolute   — 绝对路径
5. 截断到 MAX_MEMORY_CHARACTER_COUNT = 40000
```

关键常量：
- `ENTRYPOINT_NAME = 'MEMORY.md'`
- `MAX_ENTRYPOINT_LINES = 200`
- `MAX_ENTRYPOINT_BYTES = 25_000`

#### MemoryFileInfo 类型

```typescript
type MemoryFileInfo = {
  path: string
  type: MemoryType  // 'User' | 'Project' | 'Local' | 'Managed' | 'AutoMem' | 'TeamMem'
  content: string
  parent?: string
  globs?: string[]       // 文件匹配模式，决定何时注入
  contentDiffersFromDisk?: boolean
  rawContent?: string
}
```

#### 相关 Memory 召回（Relevant Memory Recall）

`findRelevantMemories()` — 不是将所有 memory 注入 prompt，而是选择性注入：

1. 扫描文件头部，格式化清单
2. 调用轻量级模型侧查询
3. 最多选择 **5 个** memory 文件
4. 过滤 `alreadySurfaced` 避免重复
5. `MEMORY.md` 本身被排除（已在 system prompt 中）

#### Agent Memory

每种 agent 类型可拥有独立的持久化 Memory，作用域分三级：

- `user` — 跨项目（`~/.claude/agent-memories/`）
- `project` — 当前项目共享
- `local` — 当前机器/工作区

路径清理：`:` 替换为 `-`（插件命名空间）。`isAgentMemoryPath()` 规范化路径防止 `..` 遍历。

#### Agent Memory 快照

`agentMemorySnapshot.ts` — 使 agent memory 成为可分发资产，项目可随 agent 定义一起发布初始 memory：

- 快照存储在 `<cwd>/.claude/agent-memory-snapshots/<agentType>/`
- 三种状态：`none`、`initialize`、`prompt-update`
- `initializeFromSnapshot()` 复制文件；`replaceFromSnapshot()` 先删除已有 `.md` 文件
- 目前仅适用于 `memory === 'user'` 作用域的 agent

#### Session Memory

Session Memory 有独立的阈值和更新策略：

```
minimumMessageTokensToInit  = 10_000    // 初始化最小 token 数
minimumTokensBetweenUpdate  = 5_000     // 更新间隔最小 token 数
toolCallsBetweenUpdates     = 3         // 更新间隔最小工具调用数
```

- 仅在自然断点提取（`!hasToolCallsInLastTurn`），避免中链截断
- 文件权限：`0o700`（目录）/ `0o600`（文件）
- 更新通过 forked subagent 运行，沙箱限制为仅 `FileEditTool` 操作精确的 memory 文件路径

#### 自动 Memory 提取

`executeExtractMemories()` 在每轮结束后 fire-and-forget 运行（通过 `handleStopHooks`）：

1. Fork 新 agent（`querySource: 'extract_memories'`，共享 prompt cache）
2. 限制 fork 只能使用 Read/Grep/Glob/只读 Bash/Edit-Write-in-memdir 工具
3. 从对话中提取持久性事实（**不调查或验证内容**，仅使用最近 N 条消息）
4. 两步保存：(1) 写入 memory 文件（含 frontmatter），(2) 添加指针到 MEMORY.md 索引
5. 高效策略：第 1 轮 = 并行读取，第 2 轮 = 并行写入
6. 如果主 agent 已写入 memory 文件则跳过（互斥）
7. 通过 feature flag 节流

`drainPendingExtraction()` 在关闭前等待进行中的提取完成。

---

### Tool 系统

#### Tool 接口

每个工具实现 `Tool<Input, Output, Progress>` 接口，这是一个全面的运行时协议：

```typescript
interface Tool<Input, Output, P> {
  // 标识
  name: string
  aliases?: string[]
  searchHint?: string
  inputSchema: ZodSchema<Input>

  // 核心执行
  call(input, context, canUseTool, parentMessage, onProgress?): Promise<ToolResult<Output>>
  checkPermissions(input, context): Promise<PermissionResult>
  validateInput(input, context): Promise<ValidationResult>

  // 并发与安全声明
  isConcurrencySafe(input): boolean   // 是否可并发执行
  isReadOnly(): boolean               // 是否只读
  isDestructive(input): boolean       // 是否有破坏性
  isEnabled(context): boolean         // 是否启用

  // Prompt 贡献
  prompt(context): string             // 工具描述注入 system prompt
  description(): string               // 工具描述

  // UI 渲染
  renderToolUseMessage(): ReactNode
  renderToolResultMessage(): ReactNode
  renderToolUseRejectedMessage(): ReactNode
  renderToolUseErrorMessage(): ReactNode

  // 控制
  interruptBehavior(): InterruptBehavior
  requiresUserInteraction(): boolean
  backfillObservableInput(): Input     // 回填隐式依赖

  // 结果处理
  maxResultSizeChars: number          // 超出则持久化到磁盘，发送预览
  mapToolResultToToolResultBlockParam(content, toolUseID): ToolResultBlockParam

  // 安全分类器
  preparePermissionMatcher(): PermissionMatcher
  toAutoClassifierInput(): string     // 空字符串 = 安全分类器短路
}
```

`buildTool(def)` 工厂函数提供 **fail-closed 默认值**：`isConcurrencySafe → false`、`isReadOnly → false`、`isDestructive → false`、`toAutoClassifierInput → ''`。

#### ToolUseContext

传递给每个工具调用的上下文对象：

```typescript
type ToolUseContext = {
  options: ClaudeCodeOptions
  abortController: AbortController
  readFileState: Map<string, string>    // 文件读取缓存
  getAppState: () => AppState
  setAppState: (state: AppState) => void
  agentId?: AgentId
  messages: Message[]
}
```

#### 工具执行生命周期

```
1. Zod parse input                    ← 输入校验（失败发回 LLM 重试）
2. validateInput()                    ← 工具自定义语义校验
3. backfillObservableInput()          ← 回填隐式依赖（expandPath 等）
4. PreToolUse hooks                   ← 前置 hook（可修改输入、拒绝、询问）
5. resolveHookPermissionDecision()    ← hook 权限决策（deny 优先于 allow）
6. canUseTool() check                 ← 权限检查（allow | deny | ask）
7. tool.call() → stream results       ← 实际执行
8. normalizeMessagesForAPI()          ← 结果规范化
9. PostToolUse hooks                  ← 后置 hook
10. PostToolUseFailure hooks（出错时）← 失败 hook
```

#### 工具并发编排

`runTools()` 的分区策略（`partitionToolCalls()`）：

1. 将连续的 `isConcurrencySafe` 工具分组为并行批次
2. 非安全工具获得独立的串行批次
3. 对每个分区：
   - 并发安全组：`runToolsConcurrently()` — 并行执行，最大并发 10
   - 串行组：`runToolsSerially()` — 顺序执行，支持 context modifier 传递

```
[Read, Read, Write, Read, Grep, Grep]
  │     │      │     │      │     │
  └─────┘      │     │      └─────┘
  concurrent   │   serial   concurrent
             serial
```

并行批次的 context modifier 延迟到批次完成后顺序应用。

`getMaxToolUseConcurrency()` 通过环境变量 `CLAUDE_CODE_MAX_TOOL_USE_CONCURRENCY` 配置，默认 10。

#### 流式工具执行（Streaming Tool Executor）

`StreamingToolExecutor` 在模型仍在流式输出时就开始执行工具，状态机：

```
queued → executing → completed → yielded
```

关键机制：
- 并发安全工具在 Zod 解析通过后立即启动，不等待完整响应
- 非安全兄弟工具排队等待
- 结果缓冲并按顺序 yield
- **Sibling abort**：创建子 `AbortController`，当 Bash 工具出错时取消并行子进程，不中止父 query
- 为被取消的工具生成合成错误消息

#### 工具池组装

`assembleToolPool()` — 内置工具（排序）+ MCP 工具（排序），通过 `uniqBy('name')` 合并。**内置工具优先于同名 MCP 工具**。

#### 工具结果预算

超大工具结果的处理：
- `maxResultSizeChars` 定义每个工具的结果大小上限
- 超出时，完整结果持久化到磁盘
- 向模型发送截断预览 + 磁盘路径引用

#### 内置工具清单

| 工具 | 说明 | 并发安全 |
|------|------|---------|
| BashTool | Shell 命令执行 | 否 |
| FileReadTool | 文件读取 | 是 |
| FileEditTool | 文件编辑（精确替换） | 否 |
| FileWriteTool | 文件写入 | 否 |
| GlobTool | 文件模式匹配 | 是 |
| GrepTool | 内容搜索 | 是 |
| LSPTool | 语言服务器协议 | 是 |
| AgentTool | 子 Agent 派生 | 否 |
| SkillTool | 技能/命令调用 | 否 |
| WebFetchTool | 网页获取 | 是 |
| WebSearchTool | 网页搜索 | 是 |
| MCPTool | MCP 协议工具 | 视服务器 |
| TaskCreate/Update/Get/List | 任务管理 | 是 |
| EnterPlanMode/ExitPlanMode | 计划模式 | 否 |
| EnterWorktree/ExitWorktree | Git worktree 隔离 | 否 |
| SendMessageTool | 多 Agent 通信 | 否 |

---

### 子 Agent 与多 Agent 系统

Claude Code 支持三种共存的多 Agent 模型：

| 模型 | 说明 | 触发方式 |
|------|------|---------|
| Plain Subagent | 子 agent 继承部分上下文/工具，运行 `runAgent()` → `query()` | AgentTool 默认 |
| Coordinator Mode | 主线程变为编排器，worker 返回 `<task-notification>` XML | `CLAUDE_CODE_COORDINATOR_MODE` 环境变量 |
| Swarm/Teammates | 显式团队实体，共享任务列表，邮箱通信 | `team_name` + `name` 参数 |

#### 派生路由

AgentTool 根据参数选择不同的派生策略：

```typescript
// 输入 schema
{
  description: string       // 任务描述
  prompt: string            // 子 agent 的 prompt
  subagent_type?: string    // 内置 agent 类型
  model?: string            // 模型覆盖（sonnet/opus/haiku）
  run_in_background?: bool  // 后台运行
  isolation?: 'worktree' | 'remote'
  team_name?: string        // 多 agent 团队
  name?: string
  mode?: string
}
```

路由逻辑：
1. `team_name + name` → `spawnTeammate()`（tmux/进程内多 agent）
2. `isolation: 'remote'` → `teleportToRemote()`（远程执行）
3. `isolation: 'worktree'` → `createAgentWorktree()`（Git worktree 隔离）
4. `run_in_background` → `registerAsyncAgent()`（后台任务）
5. 无 `subagent_type` + Fork 特性启用 → `forkSubagent()`（隐式 fork，继承父级完整上下文）
6. 默认 → 同步 `runAgent()` 调用

#### Fork Subagent

当 `subagent_type` 省略且 `FORK_SUBAGENT` 特性启用时，触发隐式 fork：
- 子 agent 继承父级完整对话上下文 AND 父级已渲染的 system prompt 字节（不重新生成）
- 保证 prompt cache 命中稳定性
- `FORK_AGENT` 定义：`tools: ['*']`、`permissionMode: 'bubble'`、`model: 'inherit'`
- 与 coordinator 模式互斥

#### runAgent 流程

`runAgent()` 是一个 `AsyncGenerator<Message>`（~973 行）：

```
1. 解析 agent 模型、权限模式、工具集
2. 构建 agent system prompt（getAgentSystemPrompt）
3. 创建子 agent 上下文（createSubagentContext）
   - 同步 agent：共享 setAppState/abortController
   - 异步 agent：隔离的 controller
4. 执行 SubagentStart hooks
5. 注册 frontmatter hooks（生命周期作用域）
6. 预加载 frontmatter 中的 skills
7. 初始化 agent 专属 MCP 服务器（叠加到父 agent）
8. 连接 agent memory（如有）
9. 调用 query() 循环，yield messages
10. finally：清理 MCP 服务器、session hooks、prompt cache、todos、后台 bash
```

#### 内置 Agent 类型

定义在 `src/tools/AgentTool/built-in/`：

| Agent | 说明 | 特性门控 |
|-------|------|---------|
| `general-purpose` | 默认通用子 agent | 无 |
| `explore` | 快速只读搜索 agent | 无 |
| `plan` | 软件架构规划 agent | `tengu_amber_stoat` |
| `claude-code-guide` | Claude Code 使用指南 | 非 SDK 模式 |
| `verification` | 验证 agent | `tengu_hive_evidence` |
| `statusline-setup` | 状态栏配置 agent | 无 |

自定义 agent 从 `.claude/agents/` 加载（markdown 文件 + YAML frontmatter）。

Agent 定义的 frontmatter：
- `model` — 模型覆盖
- `tools` / `disallowedTools` — 可用/禁用工具列表
- `permissionMode` — 权限模式
- `effort` — 推理努力级别（low/medium/high）
- `maxTurns` — 最大轮次
- `hooks` — agent 专属 hooks
- `mcpServers` — agent 专属 MCP 服务器
- `skills` — 预加载技能
- `color` — 显示颜色
- `memoryScope` — Memory 作用域
- `background` — 是否后台运行
- `omitClaudeMd` — 是否跳过 CLAUDE.md 加载

#### Coordinator 模式

通过 `CLAUDE_CODE_COORDINATOR_MODE` 环境变量激活（`COORDINATOR_MODE` 特性门控）：

- 主线程 system prompt 重写为 dispatcher 角色
- 可用工具：Agent、SendMessage、TaskStop、TeamCreate、TeamDelete、SyntheticOutput
- Worker 返回结果作为 `<task-notification>` XML 注入 user-role 消息
- 支持 scratchpad 目录用于跨 worker 知识共享（`tengu_scratch` 门控）

工作流阶段：Research（workers）→ Synthesis（coordinator）→ Implementation（workers）→ Verification（workers）

#### Swarm/Teammates

显式团队实体，三个持久化工件：team file、task list directory、`AppState.teamContext`。

**拓扑约束**：
- Teammates 不能派生其他 teammates（无无限嵌套）
- 进程内 teammates 不能派生后台 agents

**进程内 Teammates**：
- 使用 `AsyncLocalStorage` 在同一 Node.js 进程内实现上下文隔离
- 每个 teammate 获得独立的 identity/context/abortController
- 注册为 task（与 shell task 和 local agent task 并列）

**通信：双轨制**：
1. **邮箱**：文件系统 inbox（`.claude/teams/{team_name}/inboxes/{agent_name}.json`），lockfile 并发写入安全
2. **直接恢复**：本地 task queue 或 `resumeAgentBackground()`

邮箱消息类型：permission request/response、shutdown request/approval、plan approval request/response、regular messages。

`SendMessageTool` 路由：本地 agentId（queue/resume）或 mailbox（teammate）或 broadcast（`*`）。

**权限桥接**：
- 进程内 teammates 没有自己的权限 UI
- `leaderPermissionBridge.ts` 暴露 leader 的 `ToolUseConfirmQueue` setter
- Teammate 权限请求出现在 leader UI 中（带 `workerBadge`：名称 + 颜色）
- 降级：如果 bridge 不可用，使用邮箱权限同步

**任务协作**：
- 团队自动绑定共享任务列表
- Teammates 通过 `tryClaimNextTask()` 从工作队列领取任务
- Teammate 工具池强制注入 swarm 必需工具（SendMessage、TeamCreate、TeamDelete、TaskCreate/Get/List/Update），无论 agent 定义如何

---

### 权限与沙箱系统

#### 权限模式

```typescript
type PermissionMode =
  | 'default'           // 破坏性操作需确认
  | 'acceptEdits'       // 自动接受文件编辑
  | 'bypassPermissions' // 跳过所有权限检查
  | 'dontAsk'           // 从不询问，仅使用规则
  | 'plan'              // 只读计划模式
  | 'auto'              // 分类器自动审批
  | 'bubble'            // 决策冒泡到父 agent
```

#### 权限决策链

```
PermissionResult: allow | deny | ask | passthrough

决策来源（优先级）：
  rule              → 匹配配置规则
  mode              → 由权限模式决定
  hook              → PreToolUse hook 决策（deny 优先于 allow）
  classifier        → YoloClassifier（auto 模式）
  sandboxOverride   → 沙箱环境覆盖
  asyncAgent        → 异步 agent 决策
  workingDir        → 工作目录检查
  safetyCheck       → 安全系统检查
```

#### 权限规则

```typescript
type PermissionRule = {
  source: PermissionRuleSource  // cliArg | session | projectSettings | userSettings | policySettings
  ruleBehavior: 'allow' | 'deny'
  ruleValue: {
    toolName: string
    ruleContent?: string  // glob 模式、正则等
  }
}
```

规则按顺序评估，首次匹配生效。

#### Auto 模式分类器

```typescript
type YoloClassifierResult = {
  thinking: string       // 分类器推理过程
  shouldBlock: boolean   // 是否阻止
  reason: string         // 阻止原因
  model: string          // 使用的模型
  usage: TokenUsage      // token 消耗
}
```

在 `auto` 权限模式下，快速模型调用判断工具使用是否应被阻止，无需用户交互。

#### 四层沙箱架构

| 层 | 职责 |
|----|------|
| `shouldUseSandbox()` | 每命令路由：全局启用 + `dangerouslyDisableSandbox` 参数 + `containsExcludedCommand()` 检查 |
| `convertToSandboxRuntimeConfig()` | 语义翻译：Claude Code 设置 → 沙箱运行时配置 |
| `bashPermissions.ts` | 沙箱自动允许尊重显式 deny/ask 规则优先 |
| `Shell.ts` + `cleanupAfterCommand()` | 命令隔离包装 + 执行后宿主清理 |

**`excludedCommands` 不是安全边界**（源码中明确文档化）。实际安全控制是权限系统和沙箱运行时。

#### 路径语义差异

两种配置来源的路径语义不同：

| 来源 | `//path` | `/path` | `~/path` | `./path` |
|------|----------|---------|----------|----------|
| 权限规则 | 文件系统根绝对路径 | 相对于设置文件目录 | — | — |
| `sandbox.filesystem.*` | — | 绝对路径 | home 相对 | 相对于设置文件 |

#### 内置保护

- **设置文件**始终在 `denyWrite`（防止沙箱命令修改 Claude 自身配置）
- **`.claude/skills`** 始终在 `denyWrite`（skills 具有自动发现、自动加载、完全能力的特权级别）
- **Git bare repo 逃逸防护**：`HEAD`、`objects`、`refs`、`hooks`、`config` 文件在配置时拒绝（如存在）或执行后清理（`scrubBareGitRepoFiles()`）— 防止攻击者植入带恶意 `core.fsmonitor` 的假 bare repo

#### 网络规则

WebFetch 权限规则翻译为沙箱网络允许列表。`allowManagedDomainsOnly`（策略设置）完全禁用运行时 ask 回调。

#### 沙箱-权限耦合

- `autoAllowBashIfSandboxed` 需要三重条件：沙箱启用 + 自动允许启用 + 命令实际进入沙箱
- `checkSandboxAutoAllow()` 先检查显式 deny/ask 规则（包括复合命令拆分）
- `isPathInSandboxWriteAllowlist()` 将沙箱写入配置反馈到应用级路径验证（双向耦合）

#### 热重载

设置变更触发 `convertToSandboxRuntimeConfig()` → `BaseSandboxManager.updateConfig()`（通过 `settingsChangeDetector.subscribe()`）。无需重启。

---

### Hook 系统

Claude Code 有两套独立的 hook 系统：**后端 hook 基础设施**（`utils/hooks/`，来自 settings.json 的命令/prompt/agent/HTTP hooks）和 **React hooks**（`src/hooks/`，Ink UI 层）。

#### Hook 事件（27 种）

```
生命周期：    SessionStart  SessionEnd  Setup
Agent：      SubagentStart  SubagentStop
工具：       PreToolUse  PostToolUse  PostToolUseFailure
权限：       PermissionRequest  PermissionDenied
压缩：       PreCompact  PostCompact
停止：       Stop  StopFailure
通知：       Notification  UserPromptSubmit
任务：       TaskCreated  TaskCompleted
交互：       Elicitation  ElicitationResult
配置：       ConfigChange
文件系统：   WorktreeCreate  WorktreeRemove  FileChanged
其他：       InstructionsLoaded  CwdChanged
```

`SessionStart` 和 `Setup` 始终触发，不受 `includeHookEvents` 配置影响。

#### Hook 执行类型

| 类型 | 文件 | 说明 |
|------|------|------|
| Shell | `execAgentHook.ts` | Shell 命令执行 |
| HTTP | `execHttpHook.ts` | HTTP 请求（含 SSRF 防护） |
| Prompt | `execPromptHook.ts` | Prompt 模板执行 |
| Function | `sessionHooks.ts` | 进程内回调（`FunctionHook` 类型） |

#### Hook 响应 Schema

```typescript
{
  continue: boolean              // 是否继续执行
  suppressOutput?: boolean       // 是否抑制输出
  stopReason?: string            // 停止原因
  decision?: 'allow' | 'deny' | 'ask'  // 权限决策
  reason?: string                // 决策原因
  systemMessage?: string         // 注入系统消息
  hookSpecificOutput?: HookSpecificOutput  // 按事件类型的特定输出
}
```

Hook 可以：
- 阻止工具执行（`continue: false`）
- 覆盖权限决策（`decision: 'allow' | 'deny'`）
- 注入系统消息（`systemMessage`）
- 修改工具输入（通过 `hookSpecificOutput`，如重写 Bash 命令）

多个 hook 响应聚合时，**deny 优先于 allow**（`resolveHookPermissionDecision()`）。

#### Hook 运行时

- 默认超时 30s（可配置）
- Hook 环境变量：`CLAUDE_TOOL_NAME`、`CLAUDE_TOOL_INPUT`、`CLAUDE_SESSION_ID` 等
- 事件缓冲：最大 100 个 pending events
- Session hooks 使用 `Map`（非 Record）实现 O(1) 变更（高并发 schema-mode agents 场景）

#### Session Hooks

`SessionHooksState = Map<string, SessionStore>` — 会话作用域的 hooks，支持：
- `addSessionHook()` — 注册会话级 hook
- `clearSessionHooks()` — 清理（agent 生命周期结束时）
- `FunctionHook` 类型 — 进程内回调

#### Post-Sampling Hooks

模型响应后、工具执行前的程序化 hook：

```typescript
type PostSamplingHook = (context: REPLHookContext) => Promise<void> | void

type REPLHookContext = {
  messages: Message[]
  systemPrompt: SystemPrompt
  userContext: { [k: string]: string }
  systemContext: { [k: string]: string }
  toolUseContext: ToolUseContext
  querySource?: QuerySource
}
```

仅内部/程序化 API，不通过 `settings.json` 暴露。

#### SSRF 防护

`ssrfGuard.ts`（~294 行）— HTTP hooks 的 SSRF 防护，防止 hook 请求被重定向到内部网络。

---

### 会话管理

#### 会话存储

会话以 **追加式 JSONL 事件流**存储在 `~/.claude/projects/<slug>/<sessionId>.jsonl`。写入路径刻意简单（追加），所有复杂性推到读取/恢复路径。

写入特性：
- 异步批量队列（`drainWriteQueue()`）— 非每条一次写入
- 文件权限：`0o600`（文件）、`0o700`（目录）
- 主链通过 UUID 去重；侧链允许重复 UUID（fork 上下文继承）
- 远程入口：主 transcript 消息同时 PUT 到远程端点，带 `Last-Uuid` 乐观并发；每会话顺序队列防止链头竞争
- `progress` 条目显式排除在 transcript 链之外（旧版本中它们会截断对话链导致 resume 失败）

子 agent 侧链：`<projectDir>/<sessionId>/subagents/agent-<agentId>.jsonl`

#### Entry 联合类型

```typescript
type Entry =
  | TranscriptMessage              // 对话消息（含 parentUuid 链表）
  | SummaryMessage                 // 压缩摘要
  | CustomTitleMessage             // 用户自定义标题
  | AiTitleMessage                 // AI 生成标题
  | LastPromptMessage              // 最后一次 prompt
  | TaskSummaryMessage             // 任务摘要
  | TagMessage                     // 标签
  | AgentNameMessage               // Agent 名称
  | FileHistorySnapshotMessage     // 文件历史快照
  | AttributionSnapshotMessage     // 归因快照
  | QueueOperationMessage          // 队列操作
  | ModeEntry                      // 模式切换
  | WorktreeStateEntry             // Worktree 状态
  | ContentReplacementEntry        // 内容替换（工具结果存根）
  | ContextCollapseCommitEntry     // 上下文折叠提交
  | ContextCollapseSnapshotEntry   // 上下文折叠快照
```

#### TranscriptMessage 结构

```typescript
type TranscriptMessage = SerializedMessage & {
  parentUuid: UUID | null          // 链表结构
  logicalParentUuid?: UUID | null  // 跨 session break 保留
  isSidechain: boolean             // 子 agent 侧链
  agentId?: string
  teamName?: string
  agentName?: string
  promptId?: string                // 关联 OTel prompt.id
}
```

`parentUuid` 链形成链表，表示对话树。子 agent 以 `isSidechain: true` 分支。

#### 元数据持久化

Title、tag、agent-setting、mode、worktree-state、pr-link 写入同一 JSONL transcript，但周期性重新追加到尾部（`reAppendSessionMetadata()`），使其保持在 lite 读取的尾部窗口内。

#### Lite Reader

`LITE_READ_BUF_SIZE = 65536`（64KB head+tail）— 会话列表页使用 head/tail 窗口提取，非完整解析。跳过 `tool_result`、`isMeta`、compact summaries、`<command-name>` 包装器。

#### 大文件优化

超过 `SKIP_PRECOMPACT_THRESHOLD` 的文件触发 `readTranscriptForLoad()`：扫描 compact 边界偏移，仅读取边界后内容，单独扫描边界前区域的元数据行。

#### 会话恢复管线

`--resume` 时的完整恢复管线（`loadTranscriptFile()`）：

```
1. 解析 JSONL 为类型化 maps（messages, summaries, titles, tags, agent-settings, content-replacements, context-collapse-commits）
2. 桥接遗留 progress 条目（修复断裂的 parentUuid 链）
3. 应用保留段重链接
4. 应用 snip 移除 + 父链修复（applySnipRemovals() 遍历已删除父节点找到存活祖先）
5. 恢复孤立的并行工具结果（recoverOrphanedParallelToolResults() — 处理流式拆分的多 tool_use 块 assistant 消息）
6. 重新计算叶 UUID
7. checkResumeConsistency() 比较检查点消息数与恢复链长度
```

完整恢复（`loadConversationForResume()`）还包括：
- 恢复已调用的 skills 状态
- 过滤未解决的 tool uses、孤立的 thinking-only 消息、空白 assistant
- 检测中断的轮次，注入 "Continue from where you left off."
- 重新运行 session start hooks（resume 是新的运行时接管，非静态回放）
- 恢复：sessionId、asciicast 录制、cost tracker、agent identity、session metadata cache、worktree state、context collapse state

#### 归因追踪

```typescript
type FileAttributionState = {
  contentHash: string        // 文件内容 SHA-256
  claudeContribution: number // Claude 写入的字符数
  mtime: number
}
```

用于 commit 时的 Claude 贡献归因。

---

### Skill 系统

Skill 是基于 prompt 的命令（markdown 文件 + 可选 YAML frontmatter），在 forked 子 agent 中运行。

#### Skill 来源

```
1. 内置 skills（src/skills/bundled/）
2. 项目 .claude/skills/ 目录
3. 用户 ~/.claude/skills/ 目录
4. 插件提供的 skills
5. MCP 服务器 prompts
```

发现过程（`getSkillDirCommands()`）：
- 按 `cwd` memoize
- 从所有来源并行加载（`Promise.all`）
- 通过 `fs.realpath` inode 级去重（处理符号链接）
- `--bare` 模式仅加载 `--add-dir` 显式指定的路径

#### Skill Frontmatter

```yaml
name: string              # 技能名称
description: string       # 描述
when_to_use: string       # 使用时机说明
allowed_tools: string[]   # 可用工具列表
model: string             # 模型覆盖
effort: low|medium|high   # 推理努力级别
user_invocable: boolean   # false = 仅模型可调用，REPL 中隐藏
paths: string[]           # 条件触发 glob 模式
version: string           # 版本
context: inline|fork      # 执行上下文
agent: string             # 关联 agent
shell: bash|powershell    # Shell 类型
hooks: object             # skill 专属 hooks
mcpServers: object        # skill 专属 MCP 服务器
```

#### 条件 Skills

带 `paths` 字段的 skills 在匹配文件被修改时自动激活 — 文件变更 hook 订阅模式。

#### 嵌入式 Shell 执行

`executeShellCommandsInPrompt()` 支持两种语法：

- 内联：`` !`command` ``
- 块级：` ```!\ncommand\n``` `

安全约束：
- 命令通过 `hasPermissionsToUseTool` 权限系统执行
- **MCP 来源的 skills 跳过 shell 执行**（防止恶意远程服务器 RCE）
- 使用函数形式 `.replace()` 防止 PowerShell 输出中的 `$&` 特殊替换字符

内置变量：`${CLAUDE_SKILL_DIR}`、`${CLAUDE_SESSION_ID}`

#### Skill 调用流程

通过 `SkillTool` 调用时，skill 在 **forked 子 agent** 中运行（`executeForkedSkill`），使用 `runAgent()` 以 skill 的 prompt 作为初始消息。Skill frontmatter 可指定完全自定义的执行环境。

---

### 配置系统

#### 设置优先级（从高到低）

```
1. policySettings    ← 远程托管 > MDM > managed-settings.json
2. flagSettings      ← --settings-file CLI 参数
3. projectSettings   ← .claude/settings.json（项目级）
4. localSettings     ← .claude/settings.local.json（本地级）
5. userSettings      ← ~/.claude/settings.json（用户级）
```

`getInitialSettings()` 合并所有来源。每个来源独立缓存，写入时失效。

#### Settings Schema

使用 Zod 校验，包含：`model`、`permissions`（allow/deny/ask 规则）、`hooks`、`mcpServers`、`language`、`theme`、`prefersReducedMotion`、`autoUpdaterStatus`、`cleanupPeriodDays` 等。

无效的权限规则在 schema 校验前被过滤，避免单条坏规则拒绝整个文件。

---

### MCP 集成

MCP 是最大的服务模块（~12,278 行），支持多种传输协议和认证方式。

#### 传输协议

| 类型 | 传输类 | 说明 |
|------|--------|------|
| `stdio` | `StdioClientTransport` | 子进程 stdin/stdout（最常见） |
| `sse` / `sse-ide` | `SSEClientTransport` | Server-Sent Events over HTTP（含 OAuth） |
| `http` / `streamable-http` | `StreamableHTTPClientTransport` | HTTP + claude.ai 代理 |
| `ws` / `ws-ide` | `WebSocketTransport` | WebSocket 双向流（IDE 集成） |
| `sdk` | `SdkControlTransport` | 进程内 SDK 控制 |

#### 工具命名约定

MCP 工具注册为 `mcp__${serverName}__${toolName}`（如 `mcp__filesystem__read_file`），与内置工具共享相同的权限和执行生命周期。

#### 连接管理

- `connectToServer()` 使用 `memoize`（cache key = name + JSON(serverRef)）— 相同 name+config 返回同一连接
- 工具描述截断到 `MAX_MCP_DESCRIPTION_LENGTH = 2048` 字符（OpenAPI 生成的描述可达 15-60KB）
- 会话过期检测：HTTP 404 + JSON-RPC -32001，自动清除缓存并重连
- 认证缓存：`~/.claude/mcp-needs-auth-cache.json`，15 分钟 TTL
- 批量连接：本地服务器 batch size 3，远程 20（可通过环境变量配置）
- Elicitation 处理：JSON-RPC 错误码 -32042

#### 配置作用域

MCP 服务器配置可来自 7 种作用域：

| 作用域 | 说明 |
|--------|------|
| `local` | `.claude/settings.local.json` |
| `user` | `~/.claude/settings.json` |
| `project` | `.claude/settings.json` |
| `dynamic` | 运行时动态添加 |
| `enterprise` | 企业策略 |
| `claudeai` | claude.ai 代理 |
| `managed` | 托管设置 |

#### Bun 运行时适配

使用 `setTimeout` + 手动 `AbortController` 替代 `AbortSignal.timeout()`，因为 Bun 存在已知内存泄漏（每请求约 2.4KB）。

#### 认证雪崩防护

如果服务器认证失败，15 分钟内的后续调用短路到 `needs-auth`，不发起网络请求。

#### IDE 工具白名单

仅特定 `mcp__ide__*` 工具被允许（如 `getDiagnostics`、`getOpenEditorFiles`）。非 IDE MCP 工具不受限制。

#### XAA（Cross-App Access）

`xaa.ts` + `xaaIdpLogin.ts`（~998 行）— 企业 SSO 集成的跨应用访问子系统（SEP-990）。

#### claude.ai 代理

`createClaudeAiProxyFetch()` 包装 fetch，在 401 响应时自动刷新 OAuth token。

---

### API 调用与重试

`withRetry()` 是一个 `AsyncGenerator`，处理所有 API 错误恢复。重试系统是 **query-source 感知**的。

| 错误 | 策略 |
|------|------|
| 401 | 强制 OAuth token 刷新 |
| 429 | 退避重试，fast mode 可切换到标准速度 |
| 529（过载） | 前台查询源最多 3 次重试，超过触发 `FallbackTriggeredError` 切换备用模型 |
| 5xx | 标准退避重试 |
| 非前台查询源的 529 | **立即失败**，避免 3-10x 网关放大效应 |
| ECONNRESET / EPIPE | 检测陈旧连接，重试 |

关键常量：
```
DEFAULT_MAX_RETRIES      = 10
MAX_529_RETRIES          = 3
BASE_DELAY_MS            = 500
FLOOR_OUTPUT_TOKENS      = 3_000
PERSISTENT_MAX_BACKOFF_MS = 300_000  // 5 分钟（持久重试模式）
```

前台查询源（重试 529）：`repl_main_thread`、`sdk`、`agent`、`compact`。后台/分类器查询立即失败以减少容量级联期间的网关放大。

持久重试模式（`CLAUDE_CODE_UNATTENDED_RETRY`）：无限重试 429/529 + 每 30s 心跳 yield。用于无人值守会话。

退避策略：指数退避 + 抖动。`RetryContext` 携带 maxTokensOverride、model、thinkingConfig、fastMode。

---

### 状态管理（State）

`src/state/` 提供最小化的响应式状态管理（非 Redux/Zustand）。

#### Store

```typescript
createStore<T>(initialState, onChange?) → { getState, setState, subscribe }
```

使用 `Object.is()` 变更检测，listeners 为 `Set<() => void>`。

#### AppState

`AppState` 类型包装在 `DeepImmutable<>` 中（~20+ 字段），关键字段：

```typescript
type AppState = DeepImmutable<{
  // 核心
  settings: Settings
  mainLoopModel: string
  cwd: string

  // UI
  statusLineText: string
  expandedView: boolean
  verbose: boolean

  // 权限
  toolPermissionContext: ToolPermissionContext

  // Agent
  agent: AgentState
  agentNameRegistry: Map<string, AgentId>  // 排除 DeepImmutable

  // Remote
  remoteSessionUrl?: string
  remoteConnectionStatus: ConnectionStatus
  remoteBackgroundTaskCount: number

  // MCP
  mcp: { clients, tools, commands, resources, pluginReconnectKey }

  // Plugins
  plugins: { enabled, disabled, commands, errors, installationStatus }

  // Tasks（排除 DeepImmutable — 包含函数类型）
  tasks: { [taskId]: TaskState }

  // Speculation
  SpeculationState: 'idle' | { abort, messagesRef, writtenPathsRef, boundary, pipelinedSuggestion }

  // CompletionBoundary
  CompletionBoundary: 'complete' | 'bash' | 'edit' | 'denied_tool'
}>
```

`tasks` 和 `agentNameRegistry` 排除 `DeepImmutable` 包装，因为它们包含函数类型和 Map。

`setAppState()` 触发动态 system prompt section 重新评估。`onChangeAppState.ts` 处理状态转换的副作用。

---

### 任务系统（Task）

#### Task 类型

```typescript
type TaskType = 'local_bash' | 'local_agent' | 'remote_agent' | 'in_process_teammate' | 'local_workflow' | 'monitor_mcp' | 'dream'
type TaskStatus = 'pending' | 'running' | 'completed' | 'failed' | 'killed'
```

Task ID 生成：前缀字母 + 8 随机字符（大小写不敏感字母表，36^8 组合，抗符号链接攻击）。

| 前缀 | 类型 |
|------|------|
| `b` | local_bash |
| `a` | local_agent |
| `r` | remote_agent |
| `t` | in_process_teammate |
| `w` | local_workflow |
| `m` | monitor_mcp |
| `d` | dream |

#### Dream Task

`DreamTask`（auto-dream）— Memory 整合子 agent：
- 追踪 `DreamPhase`（'starting' | 'updating'）
- 追踪 `DreamTurn`（text + toolUseCount）
- 记录 filesTouched、sessionsReviewing
- 通过 `handleStopHooks` 在循环结束时触发

#### 用户可见任务工具

TaskCreate、TaskUpdate、TaskGet、TaskList — 用于在对话中创建和追踪结构化任务列表，支持依赖关系（blocks/blockedBy）。

---

### 插件系统（Plugins）

`src/plugins/` 提供插件架构，用于扩展 Claude Code。

```typescript
interface BuiltinPluginDefinition {
  id: string              // 格式：{name}@builtin
  name: string
  description: string
  isAvailable(): boolean  // 可用性门控

  // 扩展点
  skills?: SkillDefinition[]
  hooks?: HookDefinition[]
  mcpServers?: MCPServerConfig[]
}
```

- 用户可通过 `/plugin` UI 启用/禁用
- 插件可贡献：skills、hooks、MCP 服务器
- 插件来源使用 `'bundled'`（非 `'builtin'`），使 skills 出现在 Skill tool 列表中
- `registerBuiltinPlugin()`、`getBuiltinPlugins()` → `{ enabled, disabled }`

---

### Remote 执行

`src/remote/`（~1,127 行）处理远程 agent 执行（将工作"传送"到远程机器）。

#### RemoteSessionManager

管理远程会话生命周期：
- `RemoteSessionConfig`：sessionId、getAccessToken、orgUuid、hasInitialPrompt、viewerOnly
- `RemoteSessionCallbacks`：onMessage、onPermissionRequest、onPermissionCancelled、onConnected、onDisconnected
- 处理 SDKMessage 路由 vs 控制消息（权限请求/响应）

#### WebSocket 连接

```
RECONNECT_DELAY_MS           = 2_000
MAX_RECONNECT_ATTEMPTS       = 5
PING_INTERVAL_MS             = 30_000
MAX_SESSION_NOT_FOUND_RETRIES = 3
```

- 永久关闭码：4003（unauthorized）
- 4001（session not found）有限重试（压缩期间可能是暂时性的）
- 支持主动会话和 viewer-only 模式（`claude assistant`）

---

### 上下文构建（context.ts）

`src/context.ts`（~189 行）提供 memoize 的上下文构建器：

- `getGitStatus()` — memoize，返回 git branch/status/log/user（截断到 2000 字符）
- `getSystemContext()` — memoize，返回 `{ gitStatus?, cacheBreaker? }`
- `getUserContext()` — memoize，返回 `{ claudeMd?, currentDate }`
- `getSystemPromptInjection()` / `setSystemPromptInjection()` — 缓存破坏（ant-only）

两个上下文函数都是 `memoize` 的 — 每会话计算一次，不刷新。Git 状态快照明确标记为陈旧（"will not update during the conversation"）。

---

### 跨切面模式

#### 1. Fail-Closed 默认值

工具默认非并发安全、非只读；沙箱默认启用；权限默认 ask。`toAutoClassifierInput` 默认空字符串（安全分类器短路）。

#### 2. Prompt Cache 作为一等工程关注点

- `SYSTEM_PROMPT_DYNAMIC_BOUNDARY` 分隔稳定前缀和变化后缀
- Section 级缓存（`systemPromptSection` vs `DANGEROUS_uncachedSystemPromptSection`）
- Fork 子 agent 复用父级已渲染的 prompt 字节
- Compact agent 借用主对话的 prompt cache 前缀
- 工具按名称排序保证 cache 稳定性

#### 3. 熔断器与优雅降级

- Auto-compact 熔断器（3 次失败后禁用）
- 认证缓存 TTL（15 分钟防雪崩）
- PTL 重试每次截断 20% 最旧消息组
- 沙箱优雅降级（不可用时降级而非失败）
- 529 过载时后台查询立即失败（防网关放大）

#### 4. AsyncGenerator 无处不在

query loop、工具执行、stop hooks、streaming 全部使用 `async function*` generator + `yield*` 委托。

#### 5. 追加式写入 + 复杂恢复

写入路径刻意简单（JSONL 追加）；所有复杂性推到读取/恢复路径（链修复、snip 移除、并行工具结果恢复、元数据尾部重追加）。

#### 6. 层间双向耦合

- 沙箱配置反馈到路径验证
- 权限规则翻译为沙箱网络允许列表
- 工具安全声明驱动并发调度

#### 7. 安全纵深防御

- Git bare repo 逃逸防护
- 设置文件写保护
- Skills 目录写保护
- MCP shell 执行阻断
- 认证雪崩防护
- `excludedCommands` 明确标记为非安全边界
- `AnalyticsMetadata_I_VERIFIED_THIS_IS_NOT_CODE_OR_FILEPATHS` 类型名强制日志不含代码/文件路径

---

### 优点

- **单一循环驱动一切** — 所有功能都是 query loop 的扩展点，架构简单统一
- **多策略压缩** — 从 tool result budget 到 full compact + session memory compact，7 种策略覆盖不同粒度，优雅降级
- **Forked Agent 共享缓存** — 压缩、Memory 提取等昂贵操作共享 prompt cache，成本低
- **流式工具执行** — 模型生成与工具执行重叠，降低端到端延迟
- **工具自声明并发安全性** — 编排器无需了解工具内部，自动分区
- **27 种 Hook 事件** — 几乎所有生命周期节点都可被外部拦截和修改
- **分层权限 + 四层沙箱** — 规则 → 模式 → Hook → 分类器 → 用户，灵活且安全
- **追加式 JSONL** — 并发安全、支持分支、支持增量恢复
- **四层 Memory 架构** — Auto/Session/Agent/Team Memory + 相关 Memory 召回 + 快照分发
- **编译时特性门控** — 零运行时开销的功能开关，条件 require() 支持 tree-shaking
- **三种多 Agent 模型** — Plain subagent / Coordinator / Swarm 覆盖不同协作场景
- **多运行时形态** — 同一内核支持 REPL、SDK、MCP Server、Remote/Bridge 四种形态
- **Prompt Cache 纪律** — 静态/动态分界、section 级缓存、fork 复用，系统性优化缓存命中率
- **安全纵深防御** — Git bare repo 逃逸、设置文件保护、MCP shell 阻断、SSRF 防护等多层防线

### 局限性

- **query.ts 职责过重** — 单文件 ~1700 行，压缩、恢复、工具编排、状态转换全在一个函数中（虽已拆分 query/ 子模块缓解）
- **State 对象非类型安全的转换** — 多种压缩策略通过 mutation 和条件分支交织，难以追踪状态流
- **Hook 系统复杂度** — 27 种事件 + 4 种执行类型（shell/HTTP/prompt/function）+ 权限决策，交互路径多
- **工具接口方法过多** — `Tool` 接口约 30 个方法（标识、执行、安全、UI、控制），新工具实现成本高（虽有 `buildTool` 缓解）
- **MCP 模块体量** — ~12,278 行，7 种配置作用域 + 6 种传输协议 + XAA 子系统，复杂度高
- **子 Agent 清理复杂** — `runAgent` 的 finally 块需要清理 MCP、hooks、cache、todos、bash、agent memory 等多种资源
- **memoize 的上下文** — `getGitStatus()` 和 `getUserContext()` 每会话计算一次不刷新，长会话中可能陈旧
- **React 渲染路径的同步约束** — `buildMemoryPrompt()` 使用 `readFileSync`，memory 目录创建是 fire-and-forget
- **层间双向耦合** — 沙箱配置 ↔ 路径验证、权限规则 ↔ 网络允许列表，增加理解和修改难度

---

## 参考资料

- https://github.com/anthropics/claude-code — Claude Code 项目仓库
  - `src/query.ts` — 核心 Agent Loop（~1729 行）
  - `src/query/` — Agent Loop 子模块（config、deps、stopHooks、tokenBudget）
  - `src/QueryEngine.ts` — SDK/Headless 编排层（~1295 行）
  - `src/Tool.ts` — Tool 接口定义
  - `src/services/compact/` — 上下文压缩（~3960 行，11 文件）
  - `src/services/tools/` — 工具编排与执行（~3113 行）
  - `src/services/tools/StreamingToolExecutor.ts` — 流式工具执行器
  - `src/services/extractMemories/` — 自动 Memory 提取
  - `src/constants/prompts.ts` — System Prompt 构建
  - `src/utils/systemPrompt.ts` — Prompt 组装与优先级
  - `src/tools/AgentTool/` — 子 Agent 系统（~4514 行，18 文件）
  - `src/tools/AgentTool/runAgent.ts` — 子 Agent 执行（~973 行）
  - `src/tools/AgentTool/forkSubagent.ts` — Fork 子 Agent
  - `src/coordinator/coordinatorMode.ts` — Coordinator 模式
  - `src/types/permissions.ts` — 权限系统类型
  - `src/utils/hooks/` — Hook 基础设施（~3721 行，17 文件）
  - `src/hooks/toolPermission/` — 权限处理（coordinator/interactive/swarmWorker）
  - `src/utils/settings/settings.ts` — 配置系统
  - `src/services/mcp/` — MCP 集成（~12278 行，22 文件）
  - `src/services/mcp/xaa.ts` — XAA 跨应用访问
  - `src/services/api/withRetry.ts` — API 重试逻辑（~822 行）
  - `src/services/api/claude.ts` — 核心 API 客户端（~3419 行）
  - `src/state/AppStateStore.ts` — 应用状态管理（~569 行）
  - `src/context.ts` — 上下文构建（memoize）
  - `src/plugins/builtinPlugins.ts` — 插件系统
  - `src/remote/` — 远程会话管理（~1127 行）
  - `src/tasks/` — 任务系统（~1102 行）
  - `src/memdir/` — Memory 目录管理
  - `src/skills/` — Skill 系统
  - `src/buddy/` — Companion 系统

