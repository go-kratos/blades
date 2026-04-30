---
type: reference
title: pi-agent Agent Framework 参考设计
date: 2026-04-30
status: draft
author: chenzhihui
related: []
tags: [agent, framework, architecture, context-management, memory, tools, extension]
source:
  project: pi-mono
  url: https://github.com/mariozechner/pi-mono
  version: local
---

# pi-agent Agent Framework 参考设计

## 参考来源

- **项目**: pi-mono（mariozechner/pi-mono）
- **链接**: https://github.com/mariozechner/pi-mono
- **版本/提交**: local（/Users/chenzhihui/Workspace/AgentOS/pi-mono）

pi-mono 是一个 TypeScript 实现的 AI Coding Agent 框架，包含完整的 LLM 提供商抽象、Agent Loop、上下文管理、会话持久化、工具系统和扩展系统。其分层架构和关注点分离设计值得深入研究。

---

## 原始设计分析

### 三层架构

pi-mono 将 agent 框架拆分为三个严格单向依赖的包：

```
pi-coding-agent   ← 应用层：工具、会话、扩展、压缩、UI
      ↓
pi-agent-core     ← 核心层：Agent Loop、有状态 Agent、事件
      ↓
pi-ai             ← 提供商层：LLM 抽象、流式协议、模型注册
```

每层只依赖下层，上层不向下层泄漏。

### Agent Loop 设计

Loop 被拆分为两个独立部分：

**纯函数 `runLoop()`**（`agent-loop.ts`）：无副作用，接收上下文快照和事件回调，可独立测试。

```
runAgentLoop(prompts, context, config, emit, signal, streamFn)
  └─ runLoop()
       ├─ while(true)                    // follow-up 消息循环
       │    ├─ while(hasToolCalls)       // 工具调用循环
       │    │    ├─ streamAssistantResponse()   // LLM 调用
       │    │    └─ executeToolCalls()          // 并发/串行执行工具
       │    └─ getFollowUpMessages()
       └─ emit agent_end
```

**有状态 `Agent` 类**：持有 `messages`、`model`、`tools`、`isStreaming` 等可变状态，管理 `steeringQueue`（运行中注入）和 `followUpQueue`（运行后续接），提供 `prompt()`、`steer()`、`followUp()`、`abort()` 等公开 API。

### 上下文管理

每次 LLM 调用前经过两阶段转换：

1. **`transformContext(messages: AgentMessage[])`** — 在 Agent 消息层面操作，用于裁剪、注入、压缩。由扩展的 `context` 事件处理器链式调用。
2. **`convertToLlm(messages: AgentMessage[])`** — 将 Agent 消息（含自定义类型）转换为 LLM 可理解的标准 `Message[]`。例如 `compactionSummary` → `<summary>` XML 用户消息，`bashExecution` → 命令+输出用户消息。

**Compaction（上下文压缩）**：当 token 用量超过阈值时，`AgentSession` 触发压缩：
1. 确定保留的最近 N 轮消息
2. 调用 LLM 对历史消息生成摘要
3. 将 `CompactionEntry`（含摘要和截断点）写入 JSONL 会话文件
4. 重新加载会话，历史消息替换为摘要

### Memory 系统

pi-mono 的 memory 是**文件型**的，分三个层次：

| 层次 | 实现 | 作用域 |
|------|------|--------|
| 项目上下文 | `AGENTS.md` / `CLAUDE.md`（从 cwd 向上遍历） | 项目级静态知识 |
| 会话记忆 | JSONL 会话文件（每条消息一行） | 单次会话完整历史 |
| 压缩记忆 | `CompactionEntry`（摘要 + 截断点） | 超出上下文窗口的历史摘要 |

会话文件条目类型：`SessionHeader`、`SessionMessageEntry`、`CompactionEntry`、`BranchSummaryEntry`、`ModelChangeEntry`、`CustomEntry`（扩展自定义状态）。

### Tool 系统

工具定义分两层：

**`AgentTool`（核心层）**：
```typescript
interface AgentTool<TParameters> extends Tool<TParameters> {
  label: string;
  executionMode?: "sequential" | "parallel";
  prepareArguments?(args: unknown): Static<TParameters>;
  execute(toolCallId, params, signal?, onUpdate?): Promise<AgentToolResult>;
}
```

**`ToolDefinition`（应用层）**：在 `AgentTool` 基础上增加：
- `promptSnippet` / `promptGuidelines` — 注入 system prompt 的文本片段
- `renderCall` / `renderResult` — UI 渲染组件
- `execute` 接收 `ExtensionContext`（访问 cwd、session、model 等）

**工具执行流程**：
```
prepareToolCall()          // 参数校验、beforeToolCall hook
  → executePreparedToolCall()  // tool.execute()，onUpdate 触发流式更新
  → finalizeExecutedToolCall() // afterToolCall hook，可覆盖结果
  → createToolResultMessage()
```

并发策略：`executionMode: "parallel"` 的工具在同一批次中 `Promise.all()` 并发执行；`sequential` 的工具串行执行。

### Extension 系统

扩展采用**工厂函数 + 注册 API** 模式：

```typescript
// 扩展定义
export default function(pi: ExtensionAPI) {
  pi.registerTool({ name: "my_tool", execute: async (id, params, signal, onUpdate, ctx) => ... });
  pi.on("agent_end", async (event, ctx) => { ... });
  pi.registerCommand("my-cmd", { handler: async (args, ctx) => ... });
}
```

`ExtensionAPI` 提供：工具注册、命令注册、快捷键注册、模型提供商注册、消息注入、事件订阅。

`ExtensionContext`（运行时上下文）提供：UI 操作、cwd、sessionManager、modelRegistry、abort、compact 等。

**两阶段 Runtime**：加载阶段（工厂函数执行，注册入队）→ 绑定阶段（`runner.initialize()` 后，注册生效）。

### AgentSession（应用层顶层类）

`AgentSession` 是 `pi-coding-agent` 的中心类，包装 `Agent` 并叠加应用层关注点：

- **会话持久化**：订阅 `message_end` 事件，调用 `sessionManager.appendMessage()`
- **扩展系统**：持有 `ExtensionRunner`，将所有 agent 事件路由到扩展
- **工具注册表**：维护 `_toolRegistry`（name → AgentTool）和 `_toolDefinitions`（name → ToolDefinition，含 UI 元数据）
- **自动压缩**：每次 `agent_end` 后检查 token 用量，超阈值触发压缩
- **自动重试**：检测可重试错误（过载、限流、服务器错误），退避等待后调用 `agent.continue()`
- **上下文溢出恢复**：检测 `isContextOverflow()` 错误，触发压缩后重试
- **工具 Hook**：安装 `agent.beforeToolCall` 和 `agent.afterToolCall`，路由到 `ExtensionRunner`
- **会话分支**：`newSession()`、`fork()`、`navigateTree()`、`switchSession()`

`AgentSessionEvent` 扩展 `AgentEvent`，新增：`queue_update`、`compaction_start/end`、`session_info_changed`、`auto_retry_start/end`。

#### createAgentSession 工厂

`createAgentSession()` 是主工厂函数，创建所有服务并组装：

```
1. 解析 cwd、agentDir、authStorage、modelRegistry、settingsManager、sessionManager
2. 创建 DefaultResourceLoader 并 reload()（发现扩展、技能、提示、主题、上下文文件）
3. 从 session 恢复或查找初始模型
4. 创建 Agent（含 convertToLlm、streamFn、onPayload/onResponse hooks、transformContext）
5. 从 session 恢复消息（如果是继续会话）
6. 创建 AgentSession，注入所有服务
```

### LLM 提供商层（pi-ai）

#### 核心类型

```typescript
// 消息联合
type Message = UserMessage | AssistantMessage | ToolResultMessage;

// LLM 调用上下文
interface Context {
  systemPrompt?: string;
  messages: Message[];
  tools?: Tool[];
}

// 工具 schema（TypeBox）
interface Tool<TParameters extends TSchema = TSchema> {
  name: string;
  description: string;
  parameters: TParameters;
}

// 模型描述符
interface Model<TApi extends Api> {
  id: string; api: TApi; provider: Provider; baseUrl: string;
  reasoning: boolean; input: ("text"|"image")[];
  cost: { input, output, cacheRead, cacheWrite };
  contextWindow: number; maxTokens: number;
}
```

#### 提供商注册模式

```typescript
// 模块级 Map
const apiProviderRegistry = new Map<string, RegisteredApiProvider>();

// 每个提供商文件在 import 时注册
registerApiProvider({ api: "anthropic-messages", stream, streamSimple });

// stream.ts 导入 register-builtins.js（副作用导入）注册所有内置提供商
// 然后通过注册表分发
export function streamSimple(model, context, options): AssistantMessageEventStream {
  const provider = getApiProvider(model.api);
  return provider.streamSimple(model, context, options);
}
```

#### 流式事件协议

```
start → text_start → text_delta* → text_end
      → thinking_start → thinking_delta* → thinking_end
      → toolcall_start → toolcall_delta* → toolcall_end
      → done | error
```

所有事件携带 `partial: AssistantMessage` 快照。流包装在 `EventStream<AssistantMessageEvent, AssistantMessage>` 中，`done`/`error` 时终止，暴露 `.result()`。

### Flow 模式（flow/）

pi-mono 提供四种复合 Agent 模式：

| 模式 | 说明 |
|------|------|
| **SequentialAgent** | 按顺序运行子 agent，每个接收同一 invocation 的克隆 |
| **RoutingAgent** | 使用 LLM 通过 `handoff` 工具选择目标子 agent，然后委派 |
| **LoopAgent** | 重复运行子 agent，直到 `ExitTool` 触发或 `LoopCondition` 返回 false。支持 `ErrLoopEscalated` 向外层升级 |
| **DeepAgent** | 层级任务管理器，内置 `WriteTodosTool` 和 `TaskTool`，分解复杂任务并委派给子 agent |

### Graph 引擎（graph/）

独立于 Agent 模型的 DAG 工作流引擎。节点是操作 `State`（map）的 `Handler` 函数：

- 编译时循环检测、可达性检查、结构验证
- 并行扇出（默认启用），使用 goroutine
- 条件边（`WithEdgeCondition`）
- 检查点（`Checkpointer` 接口），支持中断后恢复

### Recipe 系统（recipe/）

声明式 YAML Agent 构建器。`AgentSpec` YAML 描述 model、instruction、parameters、sub-agents、执行模式（`sequential`/`parallel`/`loop`/`tool`）、上下文策略和中间件。`recipe.Build()` 通过注册表解析一切，返回 `blades.Agent`。

### Skills 系统（skills/）

Skills 是自包含的包，从目录或 `embed.FS` 加载。每个 skill 有一个 `SKILL.md`，含 YAML frontmatter（`name`、`description`、`allowed-tools`、`compatibility`）和 markdown 正文（指令）。可选子目录：`references/`、`assets/`、`scripts/`。Skills 组合为 `Toolset`，合并工具和指令到 agent。

### 资源发现（ResourceLoader）

`DefaultResourceLoader` 从文件系统发现资源：

- 扩展：`~/.pi/agent/extensions/`、项目 `.pi/extensions/`
- 技能：`~/.pi/agent/skills/`、项目 `.pi/skills/`
- 提示：`~/.pi/agent/prompts/`、项目 `.pi/prompts/`
- 主题：`~/.pi/agent/themes/`、项目 `.pi/themes/`
- 上下文文件：从 cwd 向上遍历 `AGENTS.md` / `CLAUDE.md`

扩展可通过 `resources_discover` 事件扩展资源路径。

### 优点

- 三层严格分离，每层可独立测试和替换
- Loop 纯函数化，无副作用，易于单元测试
- 两阶段上下文转换，扩展点清晰
- JSONL 会话文件天然支持追加、分支、回放
- 工具并发策略由工具自身声明，loop 无需感知
- 扩展系统通过工厂函数隔离，避免全局状态污染
- `onUpdate` 回调支持工具执行过程中的流式进度更新

### 局限性

- TypeScript 声明合并（`CustomAgentMessages`）是语言特有能力，其他语言需要用不同方式实现消息扩展
- TypeBox schema 是运行时 JSON Schema 生成，与静态类型语言的 schema 生成方式不同
- 扩展通过 `jiti` 动态加载 TypeScript 模块，依赖 Node.js 生态的动态 import 能力
- `AgentSession` 职责过重（压缩、重试、分支、扩展路由全在一个类），违反单一职责原则

---

### 关键接口汇总

| 接口 | 所在包 | 职责 |
|------|--------|------|
| `Model<TApi>` | `pi-ai` | 模型描述符：成本、能力、兼容性设置 |
| `Context` | `pi-ai` | LLM 调用输入：systemPrompt + messages + tools |
| `Tool<TParameters>` | `pi-ai` | LLM 工具 schema（name, description, TypeBox parameters） |
| `AssistantMessageEvent` | `pi-ai` | 流式事件协议 |
| `StreamFunction` | `pi-ai` | `(model, context, options) => AssistantMessageEventStream` |
| `ApiProvider` | `pi-ai` | 注册的提供商：`stream` + `streamSimple` |
| `AgentTool<TParameters>` | `pi-agent-core` | 可执行工具：`execute()` + `executionMode` |
| `AgentContext` | `pi-agent-core` | Loop 级快照：systemPrompt + messages + tools |
| `AgentLoopConfig` | `pi-agent-core` | Loop 配置：model、hooks、converters、queue callbacks |
| `AgentEvent` | `pi-agent-core` | Loop 生命周期事件 |
| `AgentState` | `pi-agent-core` | 可变 agent 状态 |
| `AgentMessage` | `pi-agent-core` | 可扩展消息联合（LLM 消息 + 自定义类型） |
| `ToolDefinition` | `pi-coding-agent` | 完整工具定义：含 UI 渲染和 system prompt 元数据 |
| `ExtensionAPI` | `pi-coding-agent` | 扩展注册表面 |
| `ExtensionContext` | `pi-coding-agent` | 运行时上下文 |
| `ExtensionFactory` | `pi-coding-agent` | `(pi: ExtensionAPI) => void \| Promise<void>` |
| `ResourceLoader` | `pi-coding-agent` | 文件系统资源发现 |
| `AgentSessionServices` | `pi-coding-agent` | 分组的 cwd 绑定服务 |

---

### 架构模式总结

**关注点跨层分离**：
- `pi-ai`：纯提供商适配器，无 agent 逻辑
- `pi-agent-core`：纯 loop 逻辑，无文件系统、无 UI、无会话持久化
- `pi-coding-agent`：所有应用关注点（工具、会话、扩展、压缩、UI）

**声明合并实现消息扩展**：
```typescript
// pi-agent-core/types.ts — 空接口
interface CustomAgentMessages {}

// coding-agent/messages.ts — 扩展
declare module "@mariozechner/pi-agent-core" {
  interface CustomAgentMessages {
    bashExecution: BashExecutionMessage;
    custom: CustomMessage;
    branchSummary: BranchSummaryMessage;
    compactionSummary: CompactionSummaryMessage;
  }
}
```

**事件驱动架构**：所有 agent 生命周期事件通过单一 `AgentEvent` 联合类型流转。`AgentSession` 订阅一次，扇出到：会话持久化、扩展运行器、用户监听器。

**可插拔操作后端**：工具接受 `Operations` 接口（如 `BashOperations`、`EditOperations`），执行后端可替换（本地、SSH、Docker），不改变工具逻辑。

**两阶段扩展运行时**：扩展在加载阶段注册（同步），运行时绑定 action 实现后生效。允许提供商注册排队并原子应用。

**代理流函数**：`streamProxy()` 实现 `StreamFn` 接口，通过 HTTP 服务器路由而非直接调用提供商。服务端剥离 delta 事件的 `partial` 字段以减少带宽；客户端本地重建。

---

## 参考资料

- https://github.com/mariozechner/pi-mono — pi-mono 项目仓库
  - `packages/agent/src/agent-loop.ts` — Loop 纯函数实现
  - `packages/agent/src/agent.ts` — 有状态 Agent 类
  - `packages/agent/src/types.ts` — 核心类型定义
  - `packages/ai/src/types.ts` — LLM 提供商类型
  - `packages/coding-agent/src/core/agent-session.ts` — 应用层 Session
  - `packages/coding-agent/src/core/sdk.ts` — createAgentSession 工厂
  - `packages/coding-agent/src/core/extensions/runner.ts` — 扩展运行器
  - `packages/coding-agent/src/core/compaction/` — 压缩实现
  - `packages/coding-agent/src/core/tools/` — 内置工具
