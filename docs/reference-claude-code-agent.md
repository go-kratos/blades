---
type: reference
title: Claude Code Agent 参考设计
date: 2026-04-30
status: draft
author: chenzhihui
related: [reference-pi-agent-framework.md]
tags: [agent, framework, architecture, context-management, memory, tools, permissions, hooks]
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

Claude Code 是 Anthropic 基于 TypeScript/Bun 构建的 Agentic Coding Assistant。核心是一个流式多轮 Agent Loop，编排模型调用、工具执行、上下文管理、Memory、权限和 Hook 系统。代码围绕少量高杠杆抽象构建，组合性强。

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

### 整体架构

```
┌─────────────────────────────────────────────────────────┐
│  CLI / SDK / REPL 入口                                   │
├─────────────────────────────────────────────────────────┤
│  query()  ← 核心 Agent Loop（AsyncGenerator）            │
│    ├── Model Streaming（Anthropic API）                  │
│    ├── Tool Orchestration（并发/串行分区执行）            │
│    ├── Context Management（多策略压缩）                  │
│    └── Hook System（27 种事件）                          │
├─────────────────────────────────────────────────────────┤
│  Services 层                                             │
│    ├── compact/     上下文压缩                           │
│    ├── tools/       工具编排与执行                       │
│    ├── mcp/         MCP 协议客户端                       │
│    ├── api/         API 调用与重试                       │
│    └── extractMemories/  自动 Memory 提取               │
├─────────────────────────────────────────────────────────┤
│  Infrastructure 层                                       │
│    ├── Memory（CLAUDE.md 层级 + auto-memory）            │
│    ├── Permissions（规则 + 模式 + Hook + 分类器）        │
│    ├── Settings（5 级优先级合并）                        │
│    ├── Session（JSONL 追加式持久化）                     │
│    └── Hooks（Shell 命令 + SDK 回调）                    │
└─────────────────────────────────────────────────────────┘
```

---

### Agent Loop（query.ts）

Agent Loop 是整个系统的心脏。它是一个 `AsyncGenerator`，驱动所有模型调用、工具执行和状态转换。

#### 入口结构

```typescript
query()           // 外层 generator：setup、hooks、attachments
  └── queryLoop() // 内层 while(true)：实际迭代
```

#### 状态对象

```typescript
type State = {
  messages: Message[]
  toolUseContext: ToolUseContext
  autoCompactTracking: AutoCompactTracking
  maxOutputTokensRecoveryCount: number
  hasAttemptedReactiveCompact: boolean
  turnCount: number
  transition: Transition | null
}
```

每次迭代产生新的 `State` 对象，不原地修改。

#### 单次迭代流程

```
1. Apply tool result budget     ← 裁剪超大工具结果
2. Snip                         ← 硬限制：丢弃最旧消息
3. Microcompact                 ← 小窗口内联压缩
4. Context collapse             ← 特性门控的分段折叠
5. Autocompact                  ← token 阈值触发完整压缩
6. Call model                   ← 流式 API 调用，yield MessageUpdate
7. Execute tools                ← 并发 + 串行分批执行
8. Get attachments              ← 文件读取、环境状态
9. Build next State, continue   ← 判断是否需要 follow-up
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
| `max_output_tokens` | 升级到 64k tokens，重试（最多 N 次） |
| 多轮失败 | 最多 3 次 continuation 尝试 |
| `prompt_too_long` API 错误 | Reactive compact 后重试 |
| Context collapse drain | 派生 ctx-agent 等待解决 |

#### QuerySource 标记

每个 query 携带来源标记，用于区分行为：

- `repl_main_thread` — 交互式用户输入
- `agent:*` — 子 Agent（AgentTool）
- `compact` — 压缩摘要生成
- `extract_memories` — 自动 Memory 提取 fork
- `sdk` — SDK 调用方
- `task_summary` — 周期性任务摘要 fork

---

### 上下文管理

Claude Code 采用**多策略分层压缩**，每种策略在不同粒度和时机触发：

| 策略 | 触发时机 | 作用范围 | 说明 |
|------|---------|---------|------|
| Tool Result Budget | 每轮开始 | 单个工具结果 | 超大结果写入磁盘，发送预览 |
| Snip | 每轮开始 | 最旧消息 | 硬限制丢弃 |
| Microcompact | 每轮开始 | 小窗口旧消息 | 内联摘要替换 |
| Context Collapse | 每轮开始 | 指定 UUID 范围 | 分段折叠（特性门控） |
| Autocompact | token 阈值 | 全部/部分对话 | 完整 LLM 摘要 |
| Reactive Compact | API 413 错误 | 全部对话 | 紧急恢复 |

#### 完整压缩流程（Autocompact）

```
1. 运行 PreCompact hooks
2. 调用 runForkedAgent() 生成摘要（共享 prompt cache）
3. 清除 readFileState
4. 构建压缩后消息：[boundaryMarker, ...summaryMessages, ...messagesToKeep, ...attachments, ...hookResults]
5. 恢复最近读取的文件（最多 5 个，50K token 预算，每文件 5K 上限）
6. 运行 SessionStart hooks（重新注入环境）
7. 运行 PostCompact hooks
```

关键常量：
- `POST_COMPACT_MAX_FILES_TO_RESTORE = 5`
- `POST_COMPACT_TOKEN_BUDGET = 50_000`
- `AUTOCOMPACT_BUFFER_TOKENS = 13_000`

#### Forked Agent 模式

`runForkedAgent()` 创建轻量级 agent fork，共享父 agent 的 prompt cache 前缀。用于：
- 压缩摘要生成
- 自动 Memory 提取
- 任务摘要生成
- Context collapse agent

避免昂贵操作的冷缓存未命中。

---

### System Prompt 构建

System prompt 被分为**静态可缓存前缀**和**动态后缀**两部分，中间用 `__SYSTEM_PROMPT_DYNAMIC_BOUNDARY__` 分隔。

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

通过 `systemPromptSection()` 注册表管理：

- `session_guidance` — Agent 工具指导、技能调用说明
- `memory` — CLAUDE.md 文件内容
- `env_info_simple` — CWD、git 状态、OS、模型名
- `language` — 语言偏好
- `mcp_instructions` — MCP 服务器指令
- `scratchpad` — 草稿目录说明

#### 工具 Prompt 贡献

每个工具通过 `prompt(context)` 方法贡献自己的描述到 system prompt。工具按名称排序以保证 prompt cache 稳定性。

---

### Memory 系统

#### Memory 层级

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
1. 从磁盘读取文件
2. 剥离 HTML 注释
3. 解析 YAML frontmatter（globs 等）
4. 解析 @include 指令（最大深度 5）
   - @path        — 绝对路径
   - @./relative  — 相对于文件
   - @~/home      — home 相对
   - @/absolute   — 绝对路径
5. 截断到 MAX_MEMORY_CHARACTER_COUNT = 40000
```

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

#### 自动 Memory 提取

`executeExtractMemories()` 在每轮结束后 fire-and-forget 运行：

1. Fork 新 agent（`querySource: 'extract_memories'`）
2. 限制 fork 只能使用 Read/Grep/Glob/只读 Bash/Edit-Write-in-memdir 工具
3. 从对话中提取持久性事实
4. 写入 `~/.claude/memories/`
5. 如果主 agent 已写入 memory 文件则跳过（互斥）
6. 通过 feature flag 节流

`drainPendingExtraction()` 在关闭前等待进行中的提取完成。

---

### Tool 系统

#### Tool 接口

每个工具实现 `Tool<Input, Output, Progress>` 接口，核心方法：

```typescript
interface Tool<Input, Output, P> {
  name: string
  aliases?: string[]
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

  // 结果处理
  maxResultSizeChars: number          // 超出则持久化到磁盘，发送预览
  mapToolResultToToolResultBlockParam(content, toolUseID): ToolResultBlockParam
}
```

`buildTool(def)` 工厂函数提供安全默认值：`isConcurrencySafe → false`、`isReadOnly → false`、`checkPermissions → allow`。

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
1. Zod parse input                    ← 输入校验
2. validateInput()                    ← 工具自定义校验
3. Speculative classifier（auto 模式）← 快速模型判断是否阻止
4. PreToolUse hooks                   ← 前置 hook
5. resolveHookPermissionDecision()    ← hook 权限决策
6. canUseTool() check                 ← 权限检查
7. tool.call() → stream results       ← 实际执行
8. PostToolUse hooks                  ← 后置 hook
9. PostToolUseFailure hooks（出错时） ← 失败 hook
```

#### 工具并发编排

`runTools()` 的分区策略：

1. `partitionToolCalls()` — 将连续的 `isConcurrencySafe` 工具分组
2. 对每个分区：
   - 并发安全组：`runToolsConcurrently()` — 并行执行，最大并发 10
   - 串行组：`runToolsSerially()` — 顺序执行，支持 context modifier 传递

```
[bash, read, read, edit, grep, grep]
  │      │     │     │     │     │
  │      └─────┘     │     └─────┘
  │    concurrent     │   concurrent
  serial              serial
```

`getMaxToolUseConcurrency()` 通过环境变量 `CLAUDE_CODE_MAX_TOOL_USE_CONCURRENCY` 配置，默认 10。

#### 流式工具执行（Streaming Tool Executor）

`StreamingToolExecutor` 在模型仍在流式输出时就开始执行工具。工具执行与模型生成重叠，降低端到端延迟。

关键机制：
- 并发安全工具在流式过程中立即启动
- 结果缓冲并按顺序 yield
- Sibling abort：当 Bash 工具出错时，`siblingAbortController` 触发取消并行子进程
- 为被取消的工具生成合成错误消息

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

---

### 子 Agent 系统（AgentTool）

#### 派生路由

AgentTool 根据参数选择不同的派生策略：

```typescript
// 输入 schema
{
  description: string       // 任务描述
  prompt: string            // 子 agent 的 prompt
  subagent_type?: string    // 内置 agent 类型
  model?: string            // 模型覆盖
  run_in_background?: bool  // 后台运行
  isolation?: 'worktree' | 'remote'
  team_name?: string        // 多 agent 团队
  name?: string
}
```

路由逻辑：
- `team_name + name` → `spawnTeammate()`（tmux/进程内多 agent）
- `isolation: 'remote'` → `teleportToRemote()`（远程执行）
- `isolation: 'worktree'` → `createAgentWorktree()`（Git worktree 隔离）
- `run_in_background` → `registerAsyncAgent()`（后台任务）
- 默认 → 同步 `runAgent()` 调用

#### runAgent 流程

`runAgent()` 是一个 `AsyncGenerator<Message>`：

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
8. 调用 query() 循环，yield messages
9. finally：清理 MCP 服务器、session hooks、prompt cache、todos、后台 bash
```

#### 内置 Agent 类型

定义在 `src/tools/AgentTool/built-in/`，自定义 agent 从 `.claude/agents/` 加载。

Agent 定义的 frontmatter：
- `model` — 模型覆盖
- `tools` — 可用工具列表
- `permissionMode` — 权限模式
- `hooks` — agent 专属 hooks
- `mcpServers` — agent 专属 MCP 服务器
- `skills` — 预加载技能
- `maxTurns` — 最大轮次
- `background` — 是否后台运行
- `omitClaudeMd` — 是否跳过 CLAUDE.md 加载

---

### 权限系统

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
  hook              → PreToolUse hook 决策
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

---

### Hook 系统

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
- 修改工具输入（通过 `hookSpecificOutput`）

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

---

### 会话管理

#### 会话存储

会话以 JSONL 文件存储在 `~/.claude/projects/<slug>/<sessionId>.jsonl`。每行一个 `Entry`，追加写入。

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

#### 会话恢复

`--resume` 时：
1. 读取 JSONL 文件
2. 通过 `parentUuid` 链重建 `Message[]`
3. 回放 `ContentReplacementEntry`（工具结果存根）
4. 恢复 `ContextCollapseCommitEntry`（边界 UUID，归档延迟填充）
5. 恢复 `WorktreeStateEntry`（如果 worktree 路径仍存在）

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

Skill 是基于 prompt 的命令（markdown 文件 + 可选 YAML frontmatter）。

#### Skill 来源

```
1. 内置 skills（src/skills/bundled/）
2. 项目 .claude/commands/ 目录
3. 用户 ~/.claude/commands/ 目录
4. 插件提供的 skills
5. MCP 服务器 prompts
```

#### Skill 调用流程

通过 `SkillTool` 调用时，skill 在 **forked 子 agent** 中运行（`executeForkedSkill`），使用 `runAgent()` 以 skill 的 prompt 作为初始消息。

Skill frontmatter 可指定：`model`、`tools`、`hooks`、`mcpServers` 等，实现完全自定义的执行环境。

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

#### 传输协议

| 类型 | 说明 |
|------|------|
| `stdio` | 子进程 stdin/stdout |
| `sse` | Server-Sent Events over HTTP（含 OAuth） |
| `http` | StreamableHTTP |
| `ws` / `ws-ide` | WebSocket 双向流 |
| `sse-ide` | IDE 专用 SSE |
| `sdk` | 进程内 SDK 控制 |

#### 连接管理

- `connectToServer()` 使用 `memoize` — 相同 name+config 返回同一连接
- MCP 工具注册为 `mcp__serverName__toolName`，与内置工具共享相同的权限和执行生命周期
- 工具描述截断到 `MAX_MCP_DESCRIPTION_LENGTH = 2048` 字符
- 会话过期检测：HTTP 404 + JSON-RPC -32001，自动清除缓存并重连
- 认证缓存：`~/.claude/mcp-needs-auth-cache.json`，15 分钟 TTL
- 批量连接：本地服务器 batch size 3，远程 20

---

### API 调用与重试

`withRetry()` 是一个 `AsyncGenerator`，处理所有 API 错误恢复：

| 错误 | 策略 |
|------|------|
| 401 | 强制 OAuth token 刷新 |
| 429 | 退避重试，fast mode 可切换到标准速度 |
| 529（过载） | 最多 3 次重试，超过触发 `FallbackTriggeredError` 切换备用模型 |
| 5xx | 标准退避重试 |
| 非前台查询源的 529 | 立即失败，避免放大效应 |

最大重试次数：`DEFAULT_MAX_RETRIES = 10`。

持久重试模式（`CLAUDE_CODE_UNATTENDED_RETRY`）：无限重试 + 心跳 yield。

---

### 优点

- **单一循环驱动一切** — 所有功能都是 query loop 的扩展点，架构简单统一
- **多策略压缩** — 从 tool result budget 到 full compact，6 种策略覆盖不同粒度，优雅降级
- **Forked Agent 共享缓存** — 压缩、Memory 提取等昂贵操作共享 prompt cache，成本低
- **流式工具执行** — 模型生成与工具执行重叠，降低端到端延迟
- **工具自声明并发安全性** — 编排器无需了解工具内部，自动分区
- **27 种 Hook 事件** — 几乎所有生命周期节点都可被外部拦截和修改
- **分层权限** — 规则 → 模式 → Hook → 分类器 → 用户，灵活且安全
- **追加式 JSONL** — 并发安全、支持分支、支持增量恢复
- **Memory 层级 + 自动提取** — 从项目级到自动提取，多层次知识积累
- **编译时特性门控** — 零运行时开销的功能开关

### 局限性

- **query.ts 职责过重** — 单文件 ~1700 行，压缩、恢复、工具编排、状态转换全在一个函数中
- **State 对象非类型安全的转换** — 多种压缩策略通过 mutation 和条件分支交织，难以追踪状态流
- **Hook 系统复杂度** — 27 种事件 + shell 命令 + SDK 回调 + 权限决策，交互路径多
- **工具接口方法过多** — `Tool` 接口约 30 个方法，新工具实现成本高（虽有 `buildTool` 缓解）
- **MCP 连接 memoize** — 基于 name+config 的缓存可能在配置变更时产生陈旧连接
- **子 Agent 清理复杂** — `runAgent` 的 finally 块需要清理 MCP、hooks、cache、todos、bash 等多种资源

---

## 参考资料

- https://github.com/anthropics/claude-code — Claude Code 项目仓库
  - `src/query.ts` — 核心 Agent Loop
  - `src/Tool.ts` — Tool 接口定义
  - `src/services/compact/compact.ts` — 上下文压缩
  - `src/services/tools/toolOrchestration.ts` — 工具并发编排
  - `src/services/tools/toolExecution.ts` — 工具执行生命周期
  - `src/utils/claudemd.ts` — Memory 文件发现与处理
  - `src/services/extractMemories/extractMemories.ts` — 自动 Memory 提取
  - `src/constants/prompts.ts` — System Prompt 构建
  - `src/tools/AgentTool/AgentTool.tsx` — 子 Agent 派生
  - `src/tools/AgentTool/runAgent.ts` — 子 Agent 执行
  - `src/types/permissions.ts` — 权限系统类型
  - `src/utils/hooks/hookEvents.ts` — Hook 事件广播
  - `src/utils/settings/settings.ts` — 配置系统
  - `src/services/mcp/client.ts` — MCP 客户端
  - `src/services/api/withRetry.ts` — API 重试逻辑

