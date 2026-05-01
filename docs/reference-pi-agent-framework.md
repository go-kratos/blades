---
type: reference
title: pi-agent Framework 参考设计
date: 2026-05-01
status: draft
author: chenzhihui
related: [reference-claude-code-agent.md]
tags: [agent, framework, architecture, streaming, tools, extensions, session, tui]
source:
  project: pi-mono
  url: https://github.com/badlogic/pi-mono
  version: "0.70.6"
---

# pi-agent Framework 参考设计

## 参考来源

- **项目**: pi-mono（Mario Zechner）
- **链接**: https://github.com/badlogic/pi-mono
- **版本/提交**: 0.70.6

pi-mono 是一个 TypeScript monorepo，包含 7 个包，从 LLM 抽象层到终端 UI 构建了一个完整的 Coding Agent 框架。核心产品是 `pi`——一个极简的终端编程助手。项目的核心哲学是 **"aggressively extensible so it doesn't have to dictate your workflow"**，通过保持核心极简、将所有可选功能推到扩展层来实现。

---

## 原始设计分析

### 核心设计思路

pi-mono 的架构围绕以下设计原则构建：

1. **极简核心，最大扩展性** — 核心不内置 MCP、子 Agent、权限弹窗、Plan 模式、内置 TODO。所有这些功能留给扩展系统实现
2. **Context 是纯 JSON** — `Context` 对象完全 JSON 可序列化，便于持久化、跨 Provider 传递和调试
3. **TypeBox Schema 定义工具** — 工具参数使用 TypeBox schema，编译时类型安全，运行时自动校验
4. **声明合并扩展消息类型** — 通过 TypeScript declaration merging 扩展 `AgentMessage`，无需修改核心代码
5. **并行工具执行为默认** — 工具默认并发执行，单个工具可声明 `executionMode: "sequential"` 覆盖
6. **事件流协议** — 统一的 push-based `EventStream<T, R>` 异步可迭代流，贯穿 LLM 流和 Agent 事件
7. **四层扩展系统** — 从简单 Prompt 模板到完整 TypeScript 模块，渐进式复杂度

---

### 整体架构

```
┌─────────────────────────────────────────────────────────────┐
│  应用层                                                      │
│    ├── pi-mom        Slack Bot（委托给 coding-agent）        │
│    ├── pi-web-ui     Web 组件（Lit + Tailwind CSS v4）      │
│    └── pi-pods       GPU Pod 部署管理 CLI                    │
├─────────────────────────────────────────────────────────────┤
│  pi-coding-agent     编程 Agent CLI / SDK                    │
│    ├── 四种运行模式（Interactive / Print / RPC / SDK）       │
│    ├── 内置工具（read, write, edit, bash, find, grep, ls）  │
│    ├── 扩展系统（Prompts / Skills / Extensions / Packages） │
│    └── 会话管理（JSONL 树形持久化 + Compaction）            │
├─────────────────────────────────────────────────────────────┤
│  pi-agent-core       有状态 Agent 运行时                     │
│    ├── Agent 类（消息历史 + 工具执行循环）                   │
│    ├── Agent Loop（双循环：steering + follow-up）            │
│    └── 事件系统（AgentEvent 判别联合）                       │
├─────────────────────────────────────────────────────────────┤
│  pi-ai               统一 LLM 抽象层                         │
│    ├── 25+ Provider 统一流式 API                             │
│    ├── 流式事件协议（AssistantMessageEvent）                 │
│    ├── EventStream<T, R> 通用异步流                          │
│    └── Provider 注册表（懒加载）                             │
├─────────────────────────────────────────────────────────────┤
│  pi-tui              终端 UI 框架（独立，无 Agent 依赖）     │
│    ├── 差分渲染引擎                                          │
│    ├── CSI 2026 同步输出（无闪烁）                           │
│    └── 组件系统（Text, Input, Editor, Markdown, Image...）  │
└─────────────────────────────────────────────────────────────┘
```

#### 包依赖关系

| 包 | npm 名称 | 依赖 | 说明 |
|---|---|---|---|
| pi-tui | `@mariozechner/pi-tui` | 无内部依赖 | 独立 TUI 库 |
| pi-ai | `@mariozechner/pi-ai` | 无内部依赖 | LLM 抽象层 |
| pi-agent-core | `@mariozechner/pi-agent-core` | pi-ai | Agent 运行时 |
| pi-coding-agent | `@mariozechner/pi-coding-agent` | pi-agent-core, pi-ai, pi-tui | 编程 Agent |
| pi-web-ui | `@mariozechner/pi-web-ui` | pi-ai, pi-tui | Web 聊天 UI |
| pi-mom | `@mariozechner/pi-mom` | pi-coding-agent, pi-agent-core, pi-ai | Slack Bot |
| pi-pods | `@mariozechner/pi` | pi-agent-core | GPU Pod 管理 |

---

### LLM 抽象层（pi-ai）

pi-ai 是整个框架的基础，提供跨 25+ Provider 的统一流式 API。只收录支持 **tool calling** 的模型。

#### 核心类型

```typescript
// 三种消息角色
interface UserMessage {
  role: "user"
  content: string | (TextContent | ImageContent)[]
  timestamp: number
}

interface AssistantMessage {
  role: "assistant"
  content: (TextContent | ThinkingContent | ToolCall)[]
  api: Api; provider: Provider; model: string
  usage: Usage; stopReason: StopReason
  errorMessage?: string; timestamp: number
}

interface ToolResultMessage<TDetails = any> {
  role: "toolResult"
  toolCallId: string; toolName: string
  content: (TextContent | ImageContent)[]
  details?: TDetails; isError: boolean; timestamp: number
}

type Message = UserMessage | AssistantMessage | ToolResultMessage

// LLM 上下文 — 纯 JSON 可序列化
interface Context {
  systemPrompt?: string
  messages: Message[]
  tools?: Tool[]
}

// 工具定义 — TypeBox schema
interface Tool<TParameters extends TSchema = TSchema> {
  name: string
  description: string
  parameters: TParameters
}
```

#### 流式事件协议

所有 Provider 的流式输出统一为 `AssistantMessageEvent` 判别联合类型：

```typescript
type AssistantMessageEvent =
  | { type: "start"; partial: AssistantMessage }
  | { type: "text_start"; contentIndex: number; partial: AssistantMessage }
  | { type: "text_delta"; contentIndex: number; delta: string; partial: AssistantMessage }
  | { type: "text_end"; contentIndex: number; content: string; partial: AssistantMessage }
  | { type: "thinking_start"; contentIndex: number; partial: AssistantMessage }
  | { type: "thinking_delta"; contentIndex: number; delta: string; partial: AssistantMessage }
  | { type: "thinking_end"; contentIndex: number; content: string; partial: AssistantMessage }
  | { type: "toolcall_start"; contentIndex: number; partial: AssistantMessage }
  | { type: "toolcall_delta"; contentIndex: number; delta: string; partial: AssistantMessage }
  | { type: "toolcall_end"; contentIndex: number; toolCall: ToolCall; partial: AssistantMessage }
  | { type: "done"; reason: "stop" | "length" | "toolUse"; message: AssistantMessage }
  | { type: "error"; reason: "aborted" | "error"; error: AssistantMessage }
```

`EventStream<T, R>` 是通用的 push-based 异步可迭代流，`AssistantMessageEventStream` 基于它实现，在 `done` 或 `error` 事件时终止，`result()` 返回最终的 `AssistantMessage`。

#### 核心 API

```typescript
stream(model, context, options?)        // 原始流式调用
streamSimple(model, context, options?)  // 统一推理接口（自动处理 thinking level）
complete(model, context, options?)      // 非流式调用
getModel(provider, modelId)             // 类型化模型查找
getProviders() / getModels(provider)    // 注册表查询
validateToolCall(tools, toolCall)       // 校验工具参数
```

#### Provider 注册表

Provider 通过 `registerApiProvider()` 注册到全局 `Map<string, ApiProvider>`。所有内置 Provider 使用**懒加载**（动态 `import()`），在 `register-builtins.ts` 中注册。扩展可通过 `pi.registerProvider()` 添加自定义 Provider。

支持的 Provider 包括：OpenAI、Anthropic、Google（Generative AI / Gemini CLI / Vertex AI）、Mistral、Amazon Bedrock、DeepSeek、GitHub Copilot、xAI、Groq、Cerebras、OpenRouter、Vercel AI Gateway、MiniMax、Cloudflare Workers AI、Fireworks、Kimi 等。

#### 跨 Provider 上下文传递

`Context` 是纯 JSON，可以在不同 Provider 之间传递。跨 Provider 时，thinking blocks 会自动转换为 `<thinking>` 标签文本，保持上下文连续性。

#### 测试支持

`registerFauxProvider()` 提供确定性的内存 Provider，可队列化脚本响应、模拟流式输出、估算 token 用量。用于 CI 环境下无需真实 API key 的完整 Agent 测试。

---

### Agent 运行时（pi-agent-core）

pi-agent-core 在 pi-ai 之上构建有状态的 Agent 运行时，处理工具调用循环、事件流和消息状态管理。

#### 可扩展消息类型

通过 TypeScript 声明合并，应用层可以类型安全地添加自定义消息类型：

```typescript
// 空接口 — 应用通过声明合并扩展
interface CustomAgentMessages {}

// AgentMessage = LLM 消息 + 自定义消息的联合
type AgentMessage = Message | CustomAgentMessages[keyof CustomAgentMessages]

// 应用层扩展示例（pi-coding-agent 中的实际用法）
declare module "@mariozechner/agent" {
  interface CustomAgentMessages {
    bashExecution: BashExecutionMessage
    custom: CustomMessage
    branchSummary: BranchSummaryMessage
    compactionSummary: CompactionSummaryMessage
  }
}
```

#### AgentTool 接口

在 pi-ai 的 `Tool`（纯 schema）基础上，添加执行能力：

```typescript
interface AgentTool<TParameters extends TSchema, TDetails = any> extends Tool<TParameters> {
  label: string
  prepareArguments?: (args: unknown) => Static<TParameters>
  execute: (
    toolCallId: string,
    params: Static<TParameters>,
    signal?: AbortSignal,
    onUpdate?: AgentToolUpdateCallback<TDetails>,
  ) => Promise<AgentToolResult<TDetails>>
  executionMode?: "sequential" | "parallel"
}

interface AgentToolResult<T> {
  content: (TextContent | ImageContent)[]
  details: T
  terminate?: boolean  // 提示 Agent 在当前批次后停止
}
```

#### AgentLoopConfig

Agent Loop 的配置接口，定义了所有可插拔的行为：

```typescript
interface AgentLoopConfig extends SimpleStreamOptions {
  model: Model<any>

  // 消息转换边界：AgentMessage[] → Message[]
  convertToLlm: (messages: AgentMessage[]) => Message[] | Promise<Message[]>

  // 上下文变换：在 convertToLlm 之前应用
  transformContext?: (messages: AgentMessage[], signal?: AbortSignal) => Promise<AgentMessage[]>

  // 动态 API key 解析（支持短期 OAuth token）
  getApiKey?: (provider: string) => Promise<string | undefined> | string | undefined

  // 中途注入消息（Agent 工作时"转向"）
  getSteeringMessages?: () => Promise<AgentMessage[]>

  // 后续消息（Agent 停止后追加）
  getFollowUpMessages?: () => Promise<AgentMessage[]>

  // 工具执行模式，默认 "parallel"
  toolExecution?: "sequential" | "parallel"

  // 工具执行前后 Hook
  beforeToolCall?: (context: BeforeToolCallContext, signal?: AbortSignal)
    => Promise<BeforeToolCallResult | undefined>
  afterToolCall?: (context: AfterToolCallContext, signal?: AbortSignal)
    => Promise<AfterToolCallResult | undefined>
}
```

`convertToLlm` 是核心设计点：它是 AgentMessage（应用层）和 Message（LLM 层）之间的唯一边界。自定义消息在这里被转换为 LLM 可理解的格式，或被过滤掉。

#### Agent 类

`Agent` 是有状态的封装，持有消息历史、工具集和两个消息队列：

```typescript
class Agent {
  readonly state: AgentState

  subscribe(listener: (event: AgentEvent) => void | Promise<void>): () => void
  prompt(message: UserMessage | string): Promise<void>
  continue(): Promise<void>
  steer(message: AgentMessage): void     // 注入中途消息
  followUp(message: AgentMessage): void  // 注入后续消息
  abort(): void
  waitForIdle(): Promise<void>
}

interface AgentState {
  systemPrompt: string
  model: Model<any>
  thinkingLevel: ThinkingLevel
  tools: AgentTool<any>[]
  messages: AgentMessage[]
  readonly isStreaming: boolean
  readonly streamingMessage?: AgentMessage
  readonly pendingToolCalls: ReadonlySet<string>
  readonly errorMessage?: string
}
```

#### 事件系统

Agent 通过 `AgentEvent` 判别联合类型发射生命周期事件：

```typescript
type AgentEvent =
  | { type: "agent_start" }
  | { type: "agent_end"; messages: AgentMessage[] }
  | { type: "turn_start" }
  | { type: "turn_end"; message: AgentMessage; toolResults: ToolResultMessage[] }
  | { type: "message_start"; message: AgentMessage }
  | { type: "message_update"; message: AgentMessage; assistantMessageEvent: AssistantMessageEvent }
  | { type: "message_end"; message: AgentMessage }
  | { type: "tool_execution_start"; toolCallId: string; toolName: string; args: any }
  | { type: "tool_execution_update"; toolCallId: string; toolName: string; args: any; partialResult: any }
  | { type: "tool_execution_end"; toolCallId: string; toolName: string; result: any; isError: boolean }
```

事件序列：

```
prompt() → agent_start
  → [turn_start → message_start → message_update* → message_end
     → tool_execution_start → tool_execution_update* → tool_execution_end
     → turn_end]*
  → agent_end
```

#### 双循环模型

Agent Loop（`agent-loop.ts`，约 680 行）采用双循环结构：

- **外循环**：处理 follow-up 消息。当 Agent 本应停止时，检查 `getFollowUpMessages()`，有消息则继续
- **内循环**：处理 steering 消息和工具调用。每轮结束后检查 `getSteeringMessages()`，有消息则注入并继续

`StreamFn` 的契约：不得抛出异常，失败必须编码在返回的流中（通过 `error` 事件和 `stopReason: "error"`）。

---

### 工具系统

pi-mono 采用三层工具抽象，每层在上一层基础上添加能力：

#### 三层抽象

```
Tool<TParameters>                    ← pi-ai：纯 schema（name, description, parameters）
  └── AgentTool<TParameters, TDetails>  ← pi-agent-core：+ execute(), executionMode, label
        └── ToolDefinition<TParams, TDetails, TState>  ← pi-coding-agent：+ UI 渲染, prompt 贡献
```

`ToolDefinition`（pi-coding-agent 层）在 `AgentTool` 基础上添加：

- `renderCall(args)` / `renderResult(result)` — TUI 渲染
- `promptSnippet` — 注入 system prompt 的工具描述
- `promptGuidelines` — 工具使用指南
- `execute(toolCallId, params, ctx)` — 接收 `ExtensionContext` 参数

#### 内置工具

| 工具 | 执行模式 | 说明 |
|------|---------|------|
| read | parallel | 读取文件内容 |
| write | sequential | 写入文件 |
| edit | sequential | 编辑文件（差异替换） |
| bash | sequential | 执行 shell 命令 |
| find | parallel | 查找文件 |
| grep | parallel | 搜索文件内容 |
| ls | parallel | 列出目录内容 |

#### 工具执行生命周期

```
prepareArguments()     ← 参数预处理（兼容性 shim）
  → validateToolCall() ← TypeBox schema 校验
  → beforeToolCall()   ← Hook：可阻止执行（返回 { block: true }）
  → execute()          ← 实际执行，支持 onUpdate 流式进度
  → afterToolCall()    ← Hook：可修改结果（content, details, isError, terminate）
  → emit events        ← 发射 tool_execution_end + tool result message
```

#### 并行/串行分区

默认 `toolExecution: "parallel"`。执行流程：

1. 所有工具调用**串行预处理**（prepareArguments + beforeToolCall）
2. 标记为 `parallel` 的工具**并发执行**
3. 标记为 `sequential` 的工具**串行执行**
4. `tool_execution_end` 按完成顺序发射
5. tool result message 按 assistant 消息中的原始顺序发射

`terminate` 提示：只有当批次中**所有**工具结果都设置 `terminate: true` 时，Agent 才会提前停止。

---

### 扩展系统

pi-coding-agent 提供四层渐进式扩展机制，从简单到复杂：

#### 四层扩展

| 层级 | 形式 | 位置 | 能力 |
|------|------|------|------|
| Prompt Templates | Markdown 文件 | `.pi/prompts/` | 可作为 `/name` 斜杠命令调用的提示模板 |
| Skills | Markdown + YAML frontmatter | `.pi/skills/`, `.agents/skills/` | 按需加载的可复用提示，支持 `disable-model-invocation` |
| Extensions | TypeScript 模块 | `.pi/extensions/` | 完整 API 访问：工具、命令、快捷键、Provider、事件 |
| Pi Packages | npm / git 包 | `pi install npm:@foo/bar` | 打包分发 extensions/skills/prompts/themes |

#### Skill 格式

```markdown
---
name: my-skill
description: What this skill does
disable-model-invocation: false
---

Skill content here...
```

Skills 被格式化为 XML 注入 system prompt：

```xml
<available_skills>
  <skill>
    <name>my-skill</name>
    <description>...</description>
    <location>/path/to/SKILL.md</location>
  </skill>
</available_skills>
```

Agent 通过 `read` 工具按需读取 skill 文件内容。

#### Extension 工厂

Extension 是导出工厂函数的 TypeScript 模块，通过 `jiti`（无需编译）运行时加载：

```typescript
type ExtensionFactory = (pi: ExtensionAPI) => void | Promise<void>

// 示例
export default function(pi: ExtensionAPI) {
  pi.on("agent_end", async (ctx) => {
    // 在每次 Agent 运行结束后执行
  })

  pi.registerTool({
    name: "my-tool",
    description: "...",
    parameters: Type.Object({ ... }),
    execute: async (toolCallId, params, ctx) => { ... }
  })

  pi.registerCommand("my-cmd", {
    description: "...",
    execute: async (args, ctx) => { ... }
  })
}
```

#### ExtensionAPI 核心方法

```typescript
interface ExtensionAPI {
  // 事件订阅（28 种事件）
  on(event: ExtensionEventType, handler): void

  // 注册能力
  registerTool(definition: ToolDefinition): void
  registerCommand(name: string, options): void
  registerShortcut(key: string, options): void
  registerProvider(name: string, config: ProviderConfig): void
  registerFlag(name: string, options): void

  // 消息注入
  sendMessage(msg, opts): void
  sendUserMessage(content, opts): void

  // 会话状态持久化
  appendEntry(type: string, data: unknown): void

  // Shell 执行
  exec(cmd: string, args: string[]): Promise<ExecResult>

  // 跨扩展通信
  events: EventBus  // { emit(channel, data), on(channel, handler) }
}
```

#### 生命周期事件

Extension 可订阅 28 种事件，覆盖从会话到工具执行的完整生命周期：

```
session_start → resources_discover →
  [input → before_agent_start → agent_start →
    turn_start → context → before_provider_request →
      [tool_execution_start → tool_call → tool_result → tool_execution_end] →
    turn_end → agent_end] →
session_shutdown
```

关键事件能力：
- `tool_call` — 可阻止或修改工具参数
- `tool_result` — 可修改工具结果
- `context` — 可在 LLM 调用前修改消息
- `before_provider_request` — 可拦截/修改 Provider 请求

#### EventBus

简单的 `emit/on` 模式，用于扩展间通信：

```typescript
interface EventBus {
  emit(channel: string, data: unknown): void
  on(channel: string, handler: (data: unknown) => void): () => void
}
```

---

### 会话与持久化

#### JSONL 树形结构

会话以 JSONL 格式存储，每行是一个 `SessionEntry`。通过 `id` + `parentId` 构成树形结构，支持原地分支而无需创建新文件：

```typescript
interface SessionHeader {
  version: number
  id: string
  createdAt: number
  cwd: string
  title?: string
  leaf: string  // 当前位置指针
}

type SessionEntry =
  | { type: "message"; id: string; parentId: string; message: AgentMessage }
  | { type: "thinking_level_change"; ... }
  | { type: "model_change"; ... }
  | { type: "compaction"; summary: AgentMessage[]; firstKeptEntryId: string; tokensBefore: number }
  | { type: "branch_summary"; ... }
  | { type: "custom"; ... }  // 扩展自定义状态（不发送给 LLM）
```

#### 分支与导航

- `branch()` — 移动 leaf 指针，不修改历史
- `branchWithSummary()` — 创建 branch_summary 条目，保留上下文
- `createBranchedSession()` — 提取单条路径到新文件
- `/tree` 命令可回溯到任意历史节点

#### Compaction

当 `contextTokens > contextWindow - reserveTokens`（默认 16384 reserve）时触发。使用 LLM 摘要压缩旧消息，完整历史保留在 JSONL 文件中。

#### SDK 用法

```typescript
import {
  AuthStorage, createAgentSession, ModelRegistry, SessionManager
} from "@mariozechner/pi-coding-agent"

const { session } = await createAgentSession({
  sessionManager: SessionManager.inMemory(),
  authStorage: AuthStorage.create(),
  modelRegistry: ModelRegistry.create(authStorage),
})

session.subscribe((event) => { /* 处理事件 */ })
await session.prompt("What files are in the current directory?")
```

---

### TUI 框架（pi-tui）

pi-tui 是独立的终端 UI 库，不依赖 pi-ai 或 pi-agent-core。

#### Component 接口

```typescript
interface Component {
  render(width: number): string[]
  handleInput?(data: string): boolean
  invalidate(): void
}

interface Focusable extends Component {
  focus(): void
  blur(): void
  isFocused(): boolean
}
```

#### 渲染策略

- **差分渲染** — 只重绘变化的行
- **CSI 2026 同步输出** — 批量写入终端，消除闪烁
- **Kitty 键盘协议** — 支持按键释放事件

#### 内置组件

| 组件 | 说明 |
|------|------|
| Text / TruncatedText | 文本显示 |
| Input | 单行输入 |
| Editor | 多行编辑器 |
| Markdown | Markdown 渲染 |
| Image | 内联图片（Kitty/iTerm2 协议） |
| SelectList | 选择列表 |
| SettingsList | 设置面板 |
| Loader | 加载动画 |
| Box / Container | 布局容器 |
| Overlay | 浮层（锚点定位） |

---

### 优点

- **清晰的分层架构** — pi-ai → pi-agent-core → pi-coding-agent 三层职责分明，每层可独立使用和测试
- **极简 Agent Loop** — agent-loop.ts 约 680 行，双循环结构清晰（外层 follow-up，内层 steering + tool）
- **声明合并扩展消息** — `CustomAgentMessages` 接口允许类型安全地添加自定义消息，无需修改核心代码
- **convertToLlm 边界** — AgentMessage[] 到 Message[] 的转换集中在一个点，干净地解耦应用层和 LLM 层
- **EventStream 通用流** — 统一的 push-based async-iterable 模式，可用于 LLM 流和 Agent 事件
- **四层扩展系统** — 从简单 Prompt 模板到完整 TypeScript 模块，渐进式复杂度适配不同需求
- **28 种扩展事件** — 覆盖从 session 生命周期到工具执行的所有节点，扩展能力极强
- **树形会话结构** — 支持分支、导航、摘要，比线性 JSONL 更灵活
- **Faux Provider** — 内置测试 Provider，无需真实 API key 即可测试完整 Agent 流程
- **TUI 独立包** — 可复用的终端 UI 库，不绑定 Agent 逻辑，可用于其他 CLI 项目

### 局限性

- **无内置上下文压缩策略** — compaction 逻辑在 coding-agent 层，agent-core 仅提供 `transformContext` hook，不如 Claude Code 的多策略分层（snip / microcompact / context collapse / autocompact）
- **无内置权限系统** — 所有权限控制留给扩展实现，核心不提供安全保障
- **无 MCP 协议支持** — 有意不支持，但限制了与外部工具生态（如 IDE 插件、外部服务）的互操作
- **单一 Compaction 策略** — 只有 LLM 摘要一种压缩方式，缺少 tool result budget、snip、microcompact 等轻量策略
- **扩展 API 表面积大** — `ExtensionAPI` + `ExtensionUIContext` 接口方法众多，学习曲线较陡
- **无 Prompt Cache 感知** — 不像 Claude Code 那样区分静态/动态 system prompt 以优化缓存命中率
- **会话文件可能膨胀** — append-only + 树结构意味着废弃分支永远保留在文件中，无 GC 机制

---

## 参考资料

- [pi-mono 项目仓库](https://github.com/badlogic/pi-mono)
  - `packages/ai/src/types.ts` — 核心类型（Message, Tool, Model, Context, AssistantMessageEvent）
  - `packages/ai/src/stream.ts` — stream / streamSimple / complete API
  - `packages/ai/src/api-registry.ts` — Provider 注册表
  - `packages/ai/src/utils/event-stream.ts` — EventStream 通用流
  - `packages/agent/src/types.ts` — AgentTool, AgentEvent, AgentMessage, AgentLoopConfig
  - `packages/agent/src/agent.ts` — Agent 类
  - `packages/agent/src/agent-loop.ts` — Agent Loop 实现
  - `packages/coding-agent/src/core/extensions/types.ts` — ExtensionAPI, ToolDefinition, 28 种事件
  - `packages/coding-agent/src/core/session-manager.ts` — SessionManager, 树形会话
  - `packages/coding-agent/src/core/compaction/compaction.ts` — 上下文压缩
  - `packages/coding-agent/src/core/system-prompt.ts` — System Prompt 构建
  - `packages/coding-agent/src/core/event-bus.ts` — EventBus
  - `packages/coding-agent/src/core/sdk.ts` — SDK 入口
  - `packages/coding-agent/src/core/skills.ts` — Skill 加载与格式化
  - `packages/tui/src/tui.ts` — TUI Component 接口
