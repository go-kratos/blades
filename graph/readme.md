# Graph 架构文档

## 一、核心概念

### 1.1 基础组件

#### State (state.go)

```go
type State map[string]any
```

- 图中流动的数据载体
- 浅拷贝设计:每次传递都克隆,避免并发冲突
- Handler 间通过 State 传递信息

#### Handler (middleware.go:5-8)

```go
type Handler func(ctx context.Context, state State) (State, error)
```

- 节点的处理函数
- 接收 State,返回新的 State
- 不可变性原则:不修改输入,返回新实例

#### Edge (graph.go:47-52)

```go
type conditionalEdge struct {
    to        string          // 目标节点
    condition EdgeCondition   // 条件函数(nil = 无条件)
    group     string          // 依赖组名
}
```

- 有向边,支持条件分支
- 通过 group 实现复杂依赖关系

### 1.2 三层架构

```
Graph (定义)
  ↓ Compile()
Executor (编译)
  ↓ Execute()
Task (运行时)
```

## 二、Graph 层:图定义

### 2.1 图结构 (graph.go:54-62)

```go
type Graph struct {
    nodes       map[string]Handler           // 节点 -> 处理函数
    edges       map[string][]conditionalEdge // 节点 -> 出边列表
    entryPoint  string                       // 入口节点
    finishPoint string                       // 结束节点
    parallel    bool                         // 是否并行执行
    middlewares []Middleware                 // 全局中间件
}
```

### 2.2 构建 API

```go
// 创建图
g := NewGraph(WithParallel(true), WithMiddleware(logger))

// 添加节点
g.AddNode("start", startHandler)
g.AddNode("processA", processAHandler)
g.AddNode("processB", processBHandler)
g.AddNode("end", endHandler)

// 添加边
g.AddEdge("start", "processA")  // 无条件边
g.AddEdge("start", "processB",  // 条件边
    WithEdgeCondition(func(ctx, state) bool {
        return state["type"] == "B"
    }),
    WithEdgeGroup("branch_b"),
)
g.AddEdge("processA", "end")
g.AddEdge("processB", "end")

// 设置入口和出口
g.SetEntryPoint("start")
g.SetFinishPoint("end")
```

### 2.3 编译校验 (graph.go:226-237)

```go
func (g *Graph) Compile() (*Executor, error) {
    // 1. 基础校验:节点存在性、入口出口设置
    if err := g.validate(); err != nil {
        return nil, err
    }
    // 2. 拓扑检查:禁止环
    if err := g.ensureAcyclic(); err != nil {
        return nil, err
    }
    // 3. 可达性检查:确保能从入口到出口
    if err := g.ensureReachable(); err != nil {
        return nil, err
    }
    return NewExecutor(g), nil
}
```

## 三、Executor 层:依赖编译

### 3.1 预计算依赖关系 (executor.go:16-38)

```go
type Executor struct {
    graph        *Graph
    predecessors map[string][]string         // 每个节点的前驱列表(排序)
    dependencies map[string]map[string]int   // 每个节点的依赖计数
}

func NewExecutor(g *Graph) *Executor {
    predecessors := make(map[string][]string)
    dependencies := make(map[string]map[string]int)

    // 遍历所有边,统计依赖
    for from, edges := range g.edges {
        for _, edge := range edges {
            // 记录前驱
            predecessors[edge.to] = append(predecessors[edge.to], from)

            // 按组统计依赖数
            if dependencies[edge.to] == nil {
                dependencies[edge.to] = make(map[string]int)
            }
            dependencies[edge.to][edge.group]++  // 关键:按组计数
        }
    }

    // 排序前驱列表(保证聚合顺序稳定)
    for node := range predecessors {
        sort.Strings(predecessors[node])
    }

    return &Executor{graph: g, predecessors: predecessors, dependencies: dependencies}
}
```

### 3.2 依赖组示例

假设有这样的图:

```
A -> C (group="g1")
B -> C (group="g1")
D -> C (group="g2")
```

编译后:

```go
dependencies["C"] = {
    "g1": 2,  // 需要等待 A 和 B
    "g2": 1,  // 需要等待 D
}
predecessors["C"] = ["A", "B", "D"]  // 已排序
```

**语义**: C 必须等待 g1 组的 2 条边都完成 AND g2 组的 1 条边完成

## 四、Task 层:运行时执行

### 4.1 Task 状态 (task.go:10-30)

```go
type Task struct {
    executor *Executor

    // 上下文管理
    ctx    context.Context
    cancel context.CancelFunc
    wg     sync.WaitGroup  // 等待所有节点完成

    // 核心状态(需要加锁)
    mu            sync.Mutex
    contributions map[string]map[string]State  // 每个节点收到的 state 贡献
    remainingDeps map[string]map[string]int    // 剩余依赖计数(运行时拷贝)
    inFlight      map[string]bool              // 正在执行的节点
    visited       map[string]bool              // 已完成的节点
    skippedCnt    map[string]int               // 被跳过的次数
    skippedFrom   map[string]map[string]bool   // 被哪些前驱跳过(去重)

    // 结果
    finished    bool
    finishState State
    err         error
    errOnce     sync.Once
}
```

### 4.2 执行流程 (task.go:46-69)

```go
func (t *Task) run(ctx context.Context, initial State) (State, error) {
    // 1. 初始化上下文
    taskCtx, cancel := context.WithCancel(ctx)
    t.ctx = taskCtx
    t.cancel = cancel
    defer cancel()

    // 2. 注入初始 state 到入口节点
    t.mu.Lock()
    t.addContributionLocked(t.executor.graph.entryPoint, seedParent, initial)
    t.mu.Unlock()

    // 3. 调度入口节点
    t.trySchedule(t.executor.graph.entryPoint)

    // 4. 等待所有节点完成(关键同步点)
    t.wg.Wait()

    // 5. 检查结果
    t.mu.Lock()
    defer t.mu.Unlock()

    if t.err != nil {
        return nil, t.err
    }
    if !t.finished {
        return nil, fmt.Errorf("graph: finish node not reachable")
    }
    return t.finishState.Clone(), nil
}
```

### 4.3 节点调度逻辑 (task.go:71-102)

```go
func (t *Task) trySchedule(node string) {
    t.mu.Lock()

    // 1. 提前退出检查
    if t.ctx.Err() != nil || t.err != nil || t.finished {
        t.mu.Unlock()
        return
    }

    // 2. 防止重复调度
    if t.visited[node] || t.inFlight[node] {
        t.mu.Unlock()
        return
    }

    // 3. 检查依赖是否满足
    if !t.dependenciesSatisfiedLocked(node) {
        t.mu.Unlock()
        return
    }

    // 4. 聚合所有前驱的 state
    state := t.buildAggregateLocked(node)

    // 5. 标记为执行中
    t.inFlight[node] = true
    t.wg.Add(1)
    parallel := t.executor.graph.parallel
    t.mu.Unlock()

    // 6. 执行(并行或串行)
    run := func() {
        defer t.nodeDone(node)
        t.executeNode(node, state)
    }

    if parallel {
        go run()  // 并行:启动 goroutine
    } else {
        run()     // 串行:直接调用
    }
}
```

### 4.4 依赖检查 (task.go:288-299)

```go
func (t *Task) dependenciesSatisfiedLocked(node string) bool {
    groups := t.remainingDeps[node]
    if len(groups) == 0 {
        return true  // 无依赖
    }
    // 所有组的计数都必须归零
    for _, count := range groups {
        if count > 0 {
            return false
        }
    }
    return true
}
```

### 4.5 State 聚合 (task.go:266-286)

```go
func (t *Task) buildAggregateLocked(node string) State {
    var state State
    if contribs, ok := t.contributions[node]; ok {
        // 1. 按前驱顺序聚合(保证稳定性)
        order := t.executor.predecessors[node]
        for _, parent := range order {
            if contribution, exists := contribs[parent]; exists {
                state = mergeStates(state, contribution)
                delete(contribs, parent)
            }
        }

        // 2. 处理剩余的(理论上不应该有)
        for parent, contribution := range contribs {
            state = mergeStates(state, contribution)
            delete(contribs, parent)
        }

        delete(t.contributions, node)
    }

    if state == nil {
        return State{}
    }
    return state.Clone()
}

// mergeStates: 后面的覆盖前面的
func mergeStates(base State, updates ...State) State {
    merged := State{}
    if base != nil {
        merged = base.Clone()
    }
    for _, update := range updates {
        if update == nil {
            continue
        }
        for k, v := range update {
            merged[k] = v  // 覆盖
        }
    }
    return merged
}
```

### 4.6 节点执行 (task.go:104-139)

```go
func (t *Task) executeNode(node string, state State) {
    if state == nil {
        state = State{}
    }

    // 1. 检查是否已取消
    if err := t.ctx.Err(); err != nil {
        return
    }

    // 2. 获取处理函数
    handler := t.executor.graph.nodes[node]
    if handler == nil {
        t.fail(fmt.Errorf("graph: node %s handler missing", node))
        return
    }

    // 3. 应用中间件
    if len(t.executor.graph.middlewares) > 0 {
        handler = ChainMiddlewares(t.executor.graph.middlewares...)(handler)
    }

    // 4. 注入节点名到 context
    ctx := context.WithValue(t.ctx, NodeNameKey, node)

    // 5. 执行 handler
    nextState, err := handler(ctx, state)
    if err != nil {
        t.fail(fmt.Errorf("graph: node %s: %w", node, err))
        return
    }

    nextState = nextState.Clone()

    // 6. 标记访问
    if t.markVisited(node, nextState) {
        return  // 如果是 finish 节点,提前返回
    }

    // 7. 处理出边
    t.processOutgoing(node, nextState)
}
```

### 4.7 条件边解析 (task.go:333-392)

```go
func resolveEdgeSelection(ctx context.Context, node string, edges []conditionalEdge, state State) (matched, skipped []conditionalEdge, error) {
    if len(edges) == 0 {
        return nil, nil, fmt.Errorf("graph: no outgoing edges from node %s", node)
    }

    // 1. 快速路径:所有边都无条件 -> 全部匹配
    allUnconditional := true
    for _, edge := range edges {
        if edge.condition != nil {
            allUnconditional = false
            break
        }
    }
    if allUnconditional {
        return cloneEdges(edges), nil, nil
    }

    // 2. 按顺序评估条件
    var matched []conditionalEdge
    var skipped []conditionalEdge
    hasUnconditional := false

    for i, edge := range edges {
        if edge.condition == nil {
            // 无条件边:匹配,并跳过后续所有边
            matched = append(matched, edge)
            hasUnconditional = true
            skipped = append(skipped, edges[i+1:]...)
            break
        }

        if edge.condition(ctx, state) {
            // 条件满足
            matched = append(matched, edge)

            // 检查后续是否有无条件边
            if i+1 < len(edges) {
                hasTrailing := false
                for _, trailing := range edges[i+1:] {
                    if trailing.condition == nil {
                        hasTrailing = true
                        break
                    }
                }
                if hasTrailing {
                    // 有无条件边,跳过后续
                    skipped = append(skipped, edges[i+1:]...)
                    hasUnconditional = true
                    break
                }
            }
        } else {
            // 条件不满足:跳过
            skipped = append(skipped, edge)
        }
    }

    // 3. 必须至少匹配一条边
    if len(matched) == 0 {
        return nil, nil, fmt.Errorf("graph: no condition matched for edges from node %s", node)
    }

    // 4. 如果有无条件边,只保留第一个匹配
    if hasUnconditional && len(matched) > 1 {
        matched = matched[:1]
    }

    return cloneEdges(matched), skipped, nil
}
```

### 4.8 出边处理 (task.go:159-182)

```go
func (t *Task) processOutgoing(node string, state State) {
    edges := t.executor.graph.edges[node]
    if len(edges) == 0 {
        t.fail(fmt.Errorf("graph: no outgoing edges from node %s", node))
        return
    }

    // 1. 解析条件边
    matched, skipped, err := resolveEdgeSelection(t.ctx, node, edges, state)
    if err != nil {
        t.fail(err)
        return
    }

    // 2. 注册跳过的边(关键:消费依赖)
    for _, edge := range skipped {
        t.registerSkip(node, edge)
    }

    // 3. 处理匹配的边
    for _, edge := range matched {
        ready := t.consumeAndAggregate(node, edge.to, edge.group, state.Clone())
        if ready {
            t.trySchedule(edge.to)  // 依赖满足,尝试调度
        }
    }
}
```

### 4.9 跳过机制 (task.go:192-230)

```go
func (t *Task) registerSkip(parent string, edge conditionalEdge) {
    target := edge.to

    t.mu.Lock()
    if t.visited[target] {
        t.mu.Unlock()
        return
    }

    // 1. 消费依赖(关键:即使跳过也减计数)
    t.consumeDependencyLocked(target, edge.group)

    // 2. 记录跳过(去重)
    preds := t.executor.predecessors[target]
    if len(preds) == 0 {
        t.mu.Unlock()
        return
    }
    if t.skippedFrom[target] == nil {
        t.skippedFrom[target] = make(map[string]bool)
    }
    if t.skippedFrom[target][parent] {
        t.mu.Unlock()
        return  // 已记录
    }
    t.skippedFrom[target][parent] = true
    t.skippedCnt[target]++

    // 3. 判断是否所有前驱都跳过
    allSkipped := t.skippedCnt[target] >= len(preds)
    hasState := t.hasStateLocked(target)
    ready := t.dependenciesSatisfiedLocked(target)
    t.mu.Unlock()

    if allSkipped {
        // 所有前驱都跳过 -> 递归标记跳过
        t.markNodeSkipped(target)
        return
    }
    if ready && hasState {
        // 有其他前驱提供了 state -> 可以调度
        t.trySchedule(target)
    }
}

func (t *Task) markNodeSkipped(node string) {
    t.mu.Lock()
    if t.visited[node] {
        t.mu.Unlock()
        return
    }
    t.visited[node] = true
    edges := cloneEdges(t.executor.graph.edges[node])
    t.mu.Unlock()

    // 递归跳过所有出边
    for _, edge := range edges {
        t.registerSkip(node, edge)
    }
}
```

### 4.10 依赖消费 (task.go:184-190, 301-316)

```go
func (t *Task) consumeAndAggregate(parent, target, group string, contribution State) bool {
    t.mu.Lock()
    t.addContributionLocked(target, parent, contribution)  // 保存 state
    ready := t.consumeDependencyLocked(target, group) && !t.visited[target]
    t.mu.Unlock()
    return ready
}

func (t *Task) consumeDependencyLocked(node, group string) bool {
    if group == "" {
        group = node  // 默认组名 = 节点名
    }

    groups := t.remainingDeps[node]
    if len(groups) == 0 {
        return true
    }

    if count, ok := groups[group]; ok {
        if count > 0 {
            groups[group] = count - 1  // 减计数
        }
    }

    return t.dependenciesSatisfiedLocked(node)  // 检查所有组
}
```

## 五、完整执行示例

### 5.1 复杂图结构

```
       start
      /     \
  condA     condB  (条件互斥)
      \     /
       merge
         |
        end
```

```go
g := NewGraph()
g.AddNode("start", func(ctx context.Context, s State) (State, error) {
    return State{"value": 10}, nil
})

g.AddNode("condA", func(ctx context.Context, s State) (State, error) {
    s["path"] = "A"
    return s, nil
})

g.AddNode("condB", func(ctx context.Context, s State) (State, error) {
    s["path"] = "B"
    return s, nil
})

g.AddNode("merge", func(ctx context.Context, s State) (State, error) {
    return s, nil
})

g.AddNode("end", func(ctx context.Context, s State) (State, error) {
    return s, nil
})

// 条件边
g.AddEdge("start", "condA",
    WithEdgeCondition(func(ctx, s State) bool {
        return s["choice"] == "A"
    }),
    WithEdgeGroup("branch_a"),
)

g.AddEdge("start", "condB",
    WithEdgeCondition(func(ctx, s State) bool {
        return s["choice"] == "B"
    }),
    WithEdgeGroup("branch_b"),
)

g.AddEdge("condA", "merge", WithEdgeGroup("from_a"))
g.AddEdge("condB", "merge", WithEdgeGroup("from_b"))
g.AddEdge("merge", "end")

g.SetEntryPoint("start")
g.SetFinishPoint("end")

executor, _ := g.Compile()
```

### 5.2 执行过程(假设 choice="A")

**编译后的依赖:**

```go
dependencies = {
    "condA": {"branch_a": 1},
    "condB": {"branch_b": 1},
    "merge": {"from_a": 1, "from_b": 1},
    "end": {"end": 1},
}
```

**执行流程:**

1. **初始化**
   ```
   contributions["start"]["__seed__"] = {choice: "A"}
   remainingDeps = (克隆 dependencies)
   ```

2. **执行 start**
   ```
   state = {choice: "A"}
   handler 返回: {value: 10, choice: "A"}
   ```

3. **解析 start 的出边**
   ```
   condA: choice=="A" ✓ matched
   condB: choice=="B" ✗ skipped
   ```

4. **处理 matched: start->condA**
   ```
   contributions["condA"]["start"] = {value: 10, choice: "A"}
   remainingDeps["condA"]["branch_a"]--  → 0
   依赖满足 ✓ → trySchedule("condA")
   ```

5. **处理 skipped: start->condB**
   ```
   registerSkip("start", edge(start->condB))
   └─ consumeDependencyLocked("condB", "branch_b")
      └─ remainingDeps["condB"]["branch_b"]--  → 0
   └─ allSkipped("condB") == true
      └─ markNodeSkipped("condB")
         └─ visited["condB"] = true
         └─ registerSkip("condB", edge(condB->merge))
            └─ consumeDependencyLocked("merge", "from_b")
               └─ remainingDeps["merge"]["from_b"]--  → 0
   ```

6. **执行 condA**
   ```
   state = {value: 10, choice: "A"}
   handler 返回: {value: 10, choice: "A", path: "A"}
   ```

7. **处理 condA->merge**
   ```
   contributions["merge"]["condA"] = {value: 10, choice: "A", path: "A"}
   remainingDeps["merge"]["from_a"]--  → 0
   检查依赖: from_a=0 ✓, from_b=0 ✓
   依赖满足 ✓ → trySchedule("merge")
   ```

8. **执行 merge**
   ```
   state = {value: 10, choice: "A", path: "A"}
   继续执行...
   ```

9. **最终**
   ```
   visited = {start, condA, condB(skipped), merge, end}
   finished = true
   finishState = {value: 10, choice: "A", path: "A", ...}
   ```

## 六、核心设计原则

### 6.1 并发安全

- 所有共享状态用 `mu sync.Mutex` 保护
- State 每次传递都 `Clone()`,避免数据竞争
- `wg.Wait()` 确保所有 goroutine 完成后才返回

### 6.2 不可变性

- Handler 不修改输入 State,返回新实例
- `buildAggregateLocked` 返回克隆的 State
- 避免副作用,易于推理

### 6.3 依赖组机制

- 支持复杂的 join 语义(AND/部分依赖)
- 默认 group = 目标节点名(简单场景)
- 自定义 group 实现高级模式

### 6.4 跳过传播

- 跳过的边也消费依赖(防止死锁)
- 递归标记跳过(传播到下游)
- 区分"无 state"和"所有前驱跳过"

### 6.5 错误处理

- `errOnce` 确保只记录第一个错误
- 错误时 `cancel()` 取消所有 goroutine
- `trySchedule` 检查 `t.err` 提前退出

### 6.6 中间件支持

- 全局中间件应用到所有节点
- 支持链式组合(洋葱模型)
- 可访问节点名(通过 context)

## 七、关键优化

1. **依赖预计算**: 编译时计算依赖,运行时只需克隆
2. **前驱排序**: 保证 state 聚合顺序稳定
3. **快速路径**: 无条件边直接返回,跳过评估
4. **去重机制**: skippedFrom 防止重复注册
5. **计数优化**: skippedCnt 快速判断是否全跳过

## 八、使用场景

### 8.1 简单流水线

```
A -> B -> C -> D
```

### 8.2 条件分支

```
A -> (if X then B else C) -> D
```

### 8.3 并行扇出

```
      A
    / | \
   B  C  D
    \ | /
      E
```

### 8.4 复杂依赖

```
A -> D (group="g1")
B -> D (group="g1")  // D 需要 A 和 B 都完成
C -> D (group="g2")  // 且 C 也完成
```
