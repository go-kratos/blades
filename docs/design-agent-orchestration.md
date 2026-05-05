---
type: design
title: Agent 组合与编排
parent: design-agent-framework.md
date: 2026-05-01
status: draft
modules: [module-7]
---

# Agent 组合与编排

## 设计结论

AgentOS v1 不把 “SubAgent / BackgroundAgent / WorktreeAgent / Team” 作为核心 Agent 类型。核心只保留通用、可组合、与场景无关的原语：

- `flow.Sequential` / `flow.Parallel` / `flow.Loop`：组合多个 `blades.Agent`。
- `flow.AsTool(agent)`：把 Agent 适配为 `tools.Tool`，让一个 Agent 可被另一个 Agent 调用。
- 应用层 run manager：管理 Agent 运行生命周期、取消、drain、异步 job 和 channel 接入，不作为核心包。
- `event.Notification`：作为内部输入事件，把 worker、后台 job 或应用层接入状态回流到 Agent input channel。

不进入核心的能力：

- `BackgroundAgent`：这是应用层生命周期管理问题，不应改变 Agent 类型。
- `WorktreeAgent`：这是 coding workspace 隔离策略，应放在 coding app 或 contrib。
- `agents.Explore/Plan/General/Verify`：这是 coding preset，不适合通用 AgentOS 核心。
- `team.Coordinator/Swarm`：这是应用级协作协议，可后续放 `orchestrator/` 或 `contrib/orchestrator`。

## 7.1 Agent 组合原语（flow/）

`flow/` 保留原包名，但只承载通用组合能力。它依赖 `blades.Agent` 和 `event/`，根包不依赖 `flow/`，避免根包膨胀。

```go
package flow

func Sequential(agents ...blades.Agent) blades.Agent
func Parallel(agents ...blades.Agent) blades.Agent
func Loop(agent blades.Agent, opts ...LoopOption) blades.Agent
```

### Sequential

`Sequential` 将上一个 Agent 的输出按策略转换为下一个 Agent 的输入。默认策略只在 `event.TurnEnd` 后把本轮最终内容作为下一个 Agent 的 `event.Prompt`。

```go
pipeline := flow.Sequential(researcher, planner, executor)
output, err := pipeline.Run(ctx, input)
```

需要注意：`event.Output` 不等于 `event.Input`。Sequential 必须有明确的 bridge policy：

```go
type Bridge interface {
    NextInput(ctx context.Context, from <-chan event.Output) (<-chan event.Input, error)
}
```

默认 bridge 只处理内容 Part 和错误，不转发 `ToolStart/ToolDelta/ToolEnd`。工具生命周期事件属于用户可见输出，不应默认变成下游 prompt。

### Parallel

`Parallel` fan-out 同一输入流到多个 Agent，再 fan-in 输出流。

```go
search := flow.Parallel(keywordSearch, vectorSearch, webSearch)
```

Parallel 默认只合并多个 Agent 的输出流，不给 `event.Output` 增加 wrapper。需要区分来源时，应由调用方为每个子 Agent 配置不同的 `Name()`，并在 flow 层通过 hook/trace 记录来源；不要为了来源信息把 channel 类型改成包装结构。

### Loop

`Loop` 在一个 Agent 上重复执行，直到策略停止：

```go
type StopPolicy interface {
    ShouldStop(ctx context.Context, turn event.TurnEnd) bool
}

worker := flow.Loop(agent, flow.WithMaxTurns(8), flow.WithStopPolicy(policy))
```

Loop 不读取 `model.Message`，只根据 `event.Output` 和外部策略判断是否继续。需要模型上下文判断时，应通过 Agent Loop 的 hook 或 session state 实现。

## 7.2 Agent-as-Tool（flow.AsTool）

Agent-as-Tool 不单独设计 `agenttool/` 包。它本质是 Agent 组合的一种 bridge，和 Sequential/Parallel/Loop 一样属于 `flow/`：

```go
package flow

type ToolConfig struct {
    Name        string
    Description string
    MaxTurns    int
    Bridge      Bridge
}

func AsTool(agent blades.Agent, opts ...ToolOption) tools.Tool
```

执行流程：

1. Tool 接收 JSON input。
2. Bridge 将 JSON input 转成 `event.Prompt`。
3. 调用 `agent.Run(ctx, input)`.
4. drain `event.Output`。
5. Bridge 将最终输出转成 `tools.Result`。

适配器不引入新的 Agent 接口，不知道 provider、session、policy，也不直接操作 `model.Message`。

不放进 `tools/` 的原因是 `tools/` 是能力叶子包，不应反向依赖 `blades.Agent` 或 `event/`。不放进根包的原因是它是组合适配能力，不是创建普通 Agent 的必需 API。放在 `flow/` 可以保持依赖方向为 `flow -> blades/event/tools`，根包和工具协议都不被污染。

## 7.3 运行生命周期（应用层）

后台执行、取消、drain、超时、队列和资源限制由具体应用管理，不新增核心 `host/` 包。应用可以在 `cmd/<app>/internal` 内定义自己的 run handle，例如：

```go
type Run interface {
    ID() string
    Output() <-chan event.Output
    Cancel(error)
    Done() <-chan struct{}
    Err() error
}

type RunManager interface {
    Start(ctx context.Context, agent blades.Agent, input <-chan event.Input, opts ...RunOption) (Run, error)
}
```

如果需要 fire-and-forget Memory 提取或任务摘要，应用层启动一个后台 run 或异步 job，并负责 drain 输出。结果回流使用 `event.Notification` 注入目标 Agent 的 input channel。

```go
event.Notification{
    Source: "memory.extractor",
    Kind:   "memory",
    ID:     jobID,
    Status: "completed",
    Parts: []event.InputPart{
        event.JSONInput{Value: extracted},
    },
}
```

`Notification` 是 `event.Input`，不是 `event.Output`。这样内部回流和用户输入走同一条 Agent Loop 路径，同时保持 Event 层不依赖 `model/`。

## 7.4 Workspace 隔离

Worktree、容器、远程 sandbox 都属于应用层 workspace strategy，不是 Agent 类型，也不新增核心 `workspace/` 包。Coding 应用可以在自己的 internal 包或 contrib 中定义需要的接口：

```go
type Workspace interface {
    ID() string
    Root() string
    PathPolicy() PathPolicy
    Artifacts() ArtifactStore
}
```

Coding app 可以在 contrib 中提供：

```go
package codingworkspace

func NewWorktree(ctx context.Context, base Workspace, opts ...Option) (Workspace, cleanup func() error, err error)
```

Agent 不直接依赖 workspace 类型。具体路径、安全边界、artifact 存储由应用层和工具实现注入；如需在运行期传递 workspace 标识，应用使用自己的 context key 或 tool config。

## 7.5 Preset Agent 的位置

不提供核心 `agents/` 包。预设 Agent 应由应用或 contrib 提供：

```go
package preset

func Assistant(opts ...blades.Option) blades.Agent
func Researcher(opts ...blades.Option) blades.Agent
```

Coding 预设示例：

```go
package coding

func Explore(opts ...blades.Option) blades.Agent
func Plan(opts ...blades.Option) blades.Agent
func Verify(opts ...blades.Option) blades.Agent
```

这些包可以复用 `blades.New`、`tools.ToolFilter` 和 `policy.Mode`，并在应用层自定义 workspace policy，但不进入 AgentOS core。

## 7.6 多 Agent 编排

复杂多 Agent 协作可以后续新增 `orchestrator/`，而不是 `team/`：

```go
package orchestrator

type Coordinator struct {
    Workers []blades.Agent
    Policy  SchedulePolicy
}

func NewCoordinator(opts ...Option) blades.Agent
```

`orchestrator` 应建立在已有原语之上：

- 用 `flow.Parallel` 并发派发独立任务。
- 用 `flow.AsTool` 暴露 worker。
- 用 `session/` 保存任务状态。
- 用 `event.Notification` 回流 worker 完成状态。
- 用 `policy/` 统一处理权限、安全和预算。

不要让 worker 默认再创建 worker。是否允许嵌套编排应由 orchestrator policy 明确控制。

## 关键设计决策

1. **Agent 接口保持稳定**：所有组合、后台、编排能力都不改变 `Run(context.Context, <-chan event.Input) (<-chan event.Output, error)`。
2. **组合和编排分层**：`flow/` 做轻量组合，`orchestrator/` 做复杂调度，应用层做生命周期。
3. **不把场景塞进核心**：coding worktree、Explore/Plan/Verify、Team/Swarm 都是应用层能力。
4. **Notification 只做输入回流**：worker 和后台 job 的结果作为 `event.Notification` 注入 input，而不是伪造成 output。
5. **Bridge 显式化**：Output 到 Input、JSON 到 Prompt、Agent 到 Tool 都必须有明确 bridge，避免隐式把所有事件拼成文本。
