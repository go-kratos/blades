---
type: design
title: 权限与交互模式系统
parent: design-agent-framework.md
date: 2026-05-01
status: draft
modules: [module-6, module-14]
---

# 权限与交互模式系统

## 权限系统

### 现状对比

| 维度 | 当前 Blades | 新设计 |
|------|------------|--------|
| 权限控制 | 仅 `Confirm` 中间件 | 分层决策链（7 层） |
| 权限模式 | 无 | 7 种模式（default/accept_edits/plan/auto/bubble/dont_ask/bypass_permissions） |
| 规则配置 | 无 | 多来源规则（CLI/session/project/user/policy） |
| 安全检查 | 无 | bypass-immune SafetyChecker |
| 模式管理 | 无 | ModeManager 状态机（含 plan 暂存/恢复） |

### 6.1 权限决策类型

```go
type PermissionDecision string
const (
    PermissionAllow       PermissionDecision = "allow"
    PermissionDeny        PermissionDecision = "deny"
    PermissionAsk         PermissionDecision = "ask"
    PermissionPassthrough PermissionDecision = "passthrough"
)

// Mode 控制整体权限行为。
// 当前实现 4 种核心模式，其余预留枚举值供后续扩展。
type Mode string
const (
    // ModeDefault 是标准交互模式。
    // 只读工具自动批准，破坏性操作需用户确认。
    ModeDefault Mode = "default"

    // ModeAcceptEdits 自动批准工作目录内的文件写入工具。
    // Bash、MCP 等其他工具仍需确认。
    // 与 Claude Code 的 acceptEdits 模式对应。
    ModeAcceptEdits Mode = "accept_edits"

    // ModePlan 是只读探索模式。
    // 非只读工具被拒绝，工具列表被过滤（只发送只读工具给模型）。
    // 通过 EnterPlanModeTool/ExitPlanModeTool 进入/退出。
    // 完整生命周期设计见模块 14。
    ModePlan Mode = "plan"

    // ModeAuto 使用可插拔 AI 分类器自主决策。
    // 有 acceptEdits 快速路径（跳过分类器）和熔断器（连续拒绝后降级到 default）。
    // 分类器是纯接口，框架不提供内置实现。
    // 详细设计见模块 14。
    ModeAuto Mode = "auto"

    // --- 预留模式（后续按需实现）---
    // ModeDenyAll Mode = "deny_all" // 拒绝所有，仅规则放行

    // ModeBubble 将权限决策冒泡到父 agent。
    // 用于 fork 子 agent，不应自主做权限决策。
    // 父 agent 的 PermissionChain 处理实际决策。
    ModeBubble Mode = "bubble"

    // ModeDontAsk 将所有 ASK 决策转为 DENY。
    // 用于 headless/CI/SDK 场景，无人交互。
    // SafetyChecker（第 1 层）仍然生效——SeverityBlock 绝对拒绝。
    ModeDontAsk Mode = "dont_ask"

    // ModeBypassPermissions 将所有 ASK 决策转为 ALLOW。
    // 仅用于受信任环境。
    // SafetyChecker（第 1 层）仍然生效——SeverityBlock 绝对拒绝。
    ModeBypassPermissions Mode = "bypass_permissions"
)
```

### 6.2 权限规则

```go
// PermissionRule 是配置的 allow/deny 规则。
type PermissionRule struct {
    Source   PermissionRuleSource
    Behavior PermissionDecision // allow 或 deny
    ToolName string             // 工具名，支持 glob（如 "bash*"）
    Pattern  string             // 输入匹配模式（glob/正则）
}

type PermissionRuleSource string
const (
    SourceCLI     PermissionRuleSource = "cli"      // CLI 参数
    SourceSession PermissionRuleSource = "session"   // 会话内授权
    SourceProject PermissionRuleSource = "project"   // .blades/settings.json
    SourceUser    PermissionRuleSource = "user"      // ~/.blades/settings.json
    SourcePolicy  PermissionRuleSource = "policy"    // 组织策略
)
```

### 6.3 权限决策链

```go
// PermissionChain 通过分层链式判断评估权限。
// 7 层决策管线，每层可短路返回 allow/deny，或 passthrough 到下一层。
type PermissionChain struct {
    rules       []PermissionRule
    modeManager *ModeManager
    safety      SafetyChecker
    acceptEdits *AcceptEditsChecker
    autoCtrl    *AutoModeController
    hooks       *HookRegistry
    promptUser  UserPromptFunc
}

func NewPermissionChain(opts ...PermissionOption) *PermissionChain

// Check 评估工具调用的权限。
func (c *PermissionChain) Check(
    ctx context.Context, toolName string, input string,
) (PermissionDecision, error)
```

#### BubbleEscalator

```go
// BubbleEscalator 桥接子 agent 权限请求到父 agent。
type BubbleEscalator struct {
    parentChain *PermissionChain
    childID     string
}

func NewBubbleEscalator(parentChain *PermissionChain, childID string) *BubbleEscalator

// Escalate 将权限检查转发到父 agent 的权限链。
// 父链运行完整的 7 层决策管线。
// 如果 parentChain 为 nil（根 Agent），返回 DENY 作为安全默认值。
// 根 Agent 不应进入 Bubble 模式——状态转换规则已禁止此路径。
func (b *BubbleEscalator) Escalate(ctx context.Context, toolName, input string) (PermissionDecision, error) {
    if b.parentChain == nil {
        return Decision{Action: ActionDeny, Reason: "root agent has no parent chain to bubble to"}, nil
    }
    // ... 转发到 parentChain
}
```

决策流程（7 层）：

```
1. 安全检查（bypass-immune，SafetyChecker）
   → SeverityBlock 违规: DENY（绝对，任何模式都无法绕过）
   → SeverityConfirm 违规: ASK（跳过第 2-6 层，直接进入第 7 层后处理）
   → 无违规: 继续

2. 规则匹配（首次匹配生效）
   → allow/deny: 短路返回
   → 无匹配: passthrough

3. 模式决策
   → plan: 非只读工具 → DENY
   → accept_edits: 工作目录内文件写入 → ALLOW，否则 passthrough
   → bubble: 委托给 BubbleEscalator，转发到父 agent 的权限链
   → default/auto: passthrough

4. Hook 拦截（HookPreToolUse）
   → Hook 返回 allow/deny: 短路返回
   → 无 Hook 或 passthrough: 继续

5. 工具自声明检查
   → ReadOnlyTool: ALLOW
   → DestructiveTool(input)=true: ASK
   → 其他: passthrough

6. 默认决策 → ASK

7. 后处理（ASK 的最终处理）
   → auto: 运行 Classifier（见模块 14.3）
   → dont_ask: ASK → DENY（无人交互，headless/CI/SDK 场景）
   → bypass_permissions: ASK → ALLOW（受信任环境，SafetyChecker 仍生效）
   → default/accept_edits: 提示用户
```

### 6.4 权限中间件集成

```go
// PermissionMiddleware 将权限链集成到 Agent 的工具执行流程中。
// 替代当前的 Confirm 中间件，提供更细粒度的控制。
func PermissionMiddleware(chain *PermissionChain) ToolMiddleware {
    return func(next ToolHandler) ToolHandler {
        return ToolHandlerFunc(func(ctx context.Context, input string) (string, error) {
            toolCtx := tools.FromContext(ctx)
            decision, err := chain.Check(ctx, toolCtx.Name(), input)
            if err != nil {
                return "", err
            }
            switch decision {
            case PermissionDeny:
                return "", ErrPermissionDenied
            case PermissionAsk:
                return "", ErrPermissionAsk
            default:
                return next.Handle(ctx, input)
            }
        })
    }
}
```

### 关键设计决策

1. **7 层决策链而非 4 层** — 在原有 4 层（规则 → 模式 → Hook → 用户确认）基础上，新增安全检查（最前面，bypass-immune）、工具自声明检查（ReadOnlyTool/DestructiveTool）、后处理层（auto 模式分类器）。每层职责单一，可独立测试。

2. **规则优先于模式** — 规则在决策链第 2 层（仅次于安全检查），可以精确覆盖特定工具的权限。例如 `allow bash "git *"` 允许所有 git 命令，即使在 default 模式下 bash 通常需要确认。

3. **7 种模式分层实现** — 4 种核心交互模式（default/accept_edits/plan/auto）处理日常开发场景。3 种扩展模式（bubble/dont_ask/bypass_permissions）覆盖子 agent 委托、headless/CI/SDK 无人交互、受信任环境等特殊场景。SafetyChecker（第 1 层）在所有模式下保持 bypass-immune——SeverityBlock 绝对拒绝，任何模式都无法绕过。

4. **安全检查双级别** — SafetyChecker 在决策链最前面执行，区分 `SeverityBlock`（绝对禁止）和 `SeverityConfirm`（需确认但可覆盖）。路径遍历是绝对禁止的，敏感文件写入需要确认但不绝对禁止（用户可能需要配置 git hooks）。

5. **后处理层分离** — ASK 决策的最终处理（auto 模式运行分类器 vs default 模式提示用户）从决策链主流程中分离到第 7 层，使主流程（1-6 层）保持模式无关。

---

## 交互模式系统

### 背景与动机

模块 6 定义了权限决策链的基础架构，但交互模式（Plan Mode、Auto Mode、Accept Edits Mode）需要超越权限判断的完整生命周期管理：模式转换状态机、system prompt 注入、工具列表过滤、AI 分类器集成等。本模块设计这些能力。

### 现状对比

| 维度 | 当前 Blades | 新设计 |
|------|------------|--------|
| 模式管理 | 无 | ModeManager 状态机 + 转换规则 |
| Plan Mode | 权限标志 | 完整生命周期：进入/退出工具 + prompt 注入 + 工具过滤 |
| Auto Mode | 无 | 可插拔 Classifier 接口 + AutoModeController |
| Accept Edits | 无 | AcceptEditsChecker 工作目录边界检查 |
| 安全检查 | 无 | SafetyChecker bypass-immune 检查 |
| 工具过滤 | 无 | FilterToolsForMode（plan 模式隐藏写入工具） |

### 14.1 模式转换状态机

```go
package permission

// ModeState 持有当前模式和暂存状态。
// Plan 模式进入时暂存当前模式，退出时恢复。
type ModeState struct {
    Current     Mode `json:"current"`
    PrePlanMode Mode `json:"prePlanMode,omitempty"`
}

// ModeTransition 表示一次模式变更请求。
type ModeTransition struct {
    From   Mode
    To     Mode
    Source TransitionSource
}

// TransitionSource 标识模式变更的发起方。
type TransitionSource string
const (
    TransitionUser   TransitionSource = "user"   // 用户命令或 CLI 参数
    TransitionTool   TransitionSource = "tool"   // EnterPlanModeTool / ExitPlanModeTool
    TransitionSystem TransitionSource = "system" // 熔断器降级、安全回退
)

// ModeManager 管理模式状态和转换验证。
// 模式切换语义：切换立即生效于 ModeState，但对 Agent Loop 的影响
// 在下一轮 ContextBuilder.Build() 时体现（工具过滤、prompt 注入）。
// 当前轮次已在执行的工具调用不受影响，权限链在每次工具执行时
// 读取 ModeManager.Current()，因此当前轮次的后续工具调用会立即受影响。
type ModeManager struct {
    mu       sync.RWMutex
    state    ModeState
    hooks    *hook.Registry
    onChange []func(from, to Mode)
}

func NewModeManager(initial Mode) *ModeManager

func (m *ModeManager) Current() Mode
func (m *ModeManager) State() ModeState
func (m *ModeManager) Transition(t ModeTransition) error
func (m *ModeManager) OnChange(fn func(from, to Mode))
```

#### 转换规则

```go
// validTransitions 定义合法的模式转换。
// Key: (from, source) → 允许的目标模式列表。
// 不在此表中的转换被拒绝。
//
// 额外约束（不在表中，由 ModeManager.Transition() 运行时检查）：
//   - 根 Agent（parentChain == nil）不能转换到 ModeBubble（没有父链可委托）
//   - ModeAuto 只能通过 TransitionSystem: ModeDefault 降级（熔断器触发），不能被用户直接绕过
var validTransitions = map[Mode]map[TransitionSource][]Mode{
    ModeDefault: {
        TransitionUser:   {ModeAcceptEdits, ModePlan, ModeAuto, ModeBubble, ModeDontAsk, ModeBypassPermissions},
        TransitionTool:   {ModePlan},       // EnterPlanModeTool
        TransitionSystem: {},               // 无系统触发的转换
    },
    ModeAcceptEdits: {
        TransitionUser:   {ModeDefault, ModePlan, ModeAuto, ModeBubble, ModeDontAsk, ModeBypassPermissions},
        TransitionTool:   {ModePlan},
        TransitionSystem: {},
    },
    ModePlan: {
        TransitionUser: {ModeDefault, ModeAcceptEdits, ModeAuto, ModeBubble, ModeDontAsk, ModeBypassPermissions},
        TransitionTool: {ModeDefault, ModeAcceptEdits, ModeAuto}, // ExitPlanModeTool 恢复暂存模式
    },
    ModeAuto: {
        TransitionUser:   {ModeDefault, ModeAcceptEdits, ModePlan, ModeBubble, ModeDontAsk, ModeBypassPermissions},
        TransitionTool:   {ModePlan},
        TransitionSystem: {ModeDefault}, // 熔断器：连续拒绝 → 降级到 default
    },
    ModeBubble: {
        TransitionUser:   {ModeDefault, ModeAcceptEdits, ModePlan, ModeAuto, ModeDontAsk, ModeBypassPermissions},
        TransitionSystem: {ModeDefault}, // 父 agent 断开时回退到 default
        // 约束：根 Agent（无父链）不能进入 ModeBubble。
        // ModeManager 在 Transition 时检查：如果 parentChain == nil 且目标为 ModeBubble，拒绝转换。
    },
    ModeDontAsk: {
        TransitionUser:   {ModeDefault, ModeAcceptEdits, ModePlan, ModeAuto, ModeBubble, ModeBypassPermissions},
        TransitionSystem: {},
    },
    ModeBypassPermissions: {
        TransitionUser:   {ModeDefault, ModeAcceptEdits, ModePlan, ModeAuto, ModeBubble, ModeDontAsk},
        TransitionSystem: {},
    },
}
```

#### Plan 模式暂存/恢复

```go
// enterPlan 暂存当前模式并切换到 plan。
func (m *ModeManager) enterPlan() error {
    m.mu.Lock()
    defer m.mu.Unlock()
    if m.state.Current == ModePlan {
        return nil
    }
    m.state.PrePlanMode = m.state.Current
    old := m.state.Current
    m.state.Current = ModePlan
    m.notifyChange(old, ModePlan)
    return nil
}

// exitPlan 恢复暂存的模式。PrePlanMode 为空时回退到 ModeDefault。
func (m *ModeManager) exitPlan() (Mode, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    if m.state.Current != ModePlan {
        return m.state.Current, fmt.Errorf("not in plan mode, current: %s", m.state.Current)
    }
    restore := m.state.PrePlanMode
    if restore == "" {
        restore = ModeDefault
    }
    m.state.PrePlanMode = ""
    old := m.state.Current
    m.state.Current = restore
    m.notifyChange(old, restore)
    return restore, nil
}
```

### 14.2 Plan Mode 完整生命周期

Plan Mode 不仅是权限标志，而是一个完整的工作流：模型通过工具进入 plan 模式 → 使用只读工具探索代码 → 写计划文件 → 用户审批 → 退出 plan 模式并恢复之前的模式。

#### EnterPlanModeTool

```go
package planmode

// EnterPlanModeTool 切换 Agent 到 plan 模式。
// 模型在判断任务需要先探索和规划时调用此工具。
type EnterPlanModeTool struct {
    modeManager *permission.ModeManager
    planDir     string // 计划文件目录，如 ~/.blades/plans/
}

func NewEnterPlanModeTool(mm *permission.ModeManager, planDir string) tools.Tool

func (t *EnterPlanModeTool) Handle(ctx context.Context, input string) (string, error) {
    if err := t.modeManager.Transition(permission.ModeTransition{
        From:   t.modeManager.Current(),
        To:     permission.ModePlan,
        Source: permission.TransitionTool,
    }); err != nil {
        return "", err
    }
    planPath := t.getPlanFilePath(ctx)
    return fmt.Sprintf("已进入 plan 模式。请使用只读工具探索代码，将计划写入 %s，"+
        "完成后调用 ExitPlanMode 提交审批。", planPath), nil
}

func (t *EnterPlanModeTool) IsReadOnly() bool { return true }
```

#### ExitPlanModeTool

```go
// ExitPlanModeInput 是 ExitPlanModeTool 的输入。
type ExitPlanModeInput struct {
    // AllowedPrompts 是计划需要的预批准动作描述。
    // 用户审批计划时同时审批这些动作，批准后转为 session 级 allow 规则。
    AllowedPrompts []AllowedPrompt `json:"allowedPrompts,omitempty"`
}

type AllowedPrompt struct {
    Tool   string `json:"tool"`   // 工具名，如 "Bash"
    Prompt string `json:"prompt"` // 语义描述，如 "run tests"
}

// ExitPlanModeTool 读取计划文件，提交用户审批，退出 plan 模式。
type ExitPlanModeTool struct {
    modeManager *permission.ModeManager
    planDir     string
    promptUser  permission.UserPromptFunc
}

func NewExitPlanModeTool(mm *permission.ModeManager, planDir string, promptUser permission.UserPromptFunc) tools.Tool

func (t *ExitPlanModeTool) Handle(ctx context.Context, input string) (string, error) {
    if t.modeManager.Current() != permission.ModePlan {
        return "", fmt.Errorf("not in plan mode")
    }

    var params ExitPlanModeInput
    if err := json.Unmarshal([]byte(input), &params); err != nil {
        return "", fmt.Errorf("invalid input: %w", err)
    }

    planPath := t.getPlanFilePath(ctx)
    plan, err := os.ReadFile(planPath)
    if err != nil {
        return "", fmt.Errorf("计划文件不存在: %s，请先写入计划再调用 ExitPlanMode", planPath)
    }

    approved, err := t.promptUser(ctx, fmt.Sprintf("退出 plan 模式？\n\n计划:\n%s", string(plan)))
    if err != nil {
        return "", err
    }
    if !approved {
        return "计划被拒绝。请继续在 plan 模式中完善计划。", nil
    }

    restoredMode, err := t.modeManager.exitPlan()
    if err != nil {
        return "", err
    }

    // AllowedPrompts 转为 session 级 allow 规则
    for _, ap := range params.AllowedPrompts {
        t.addSessionRule(ctx, PermissionRule{
            Source:   SourceSession,
            Behavior: PermissionAllow,
            ToolName: ap.Tool,
            Pattern:  ap.Prompt, // 语义描述作为 pattern，由规则匹配器解释
        })
    }

    return fmt.Sprintf("计划已批准。模式恢复为 %s。开始实现。", restoredMode), nil
}

func (t *ExitPlanModeTool) IsReadOnly() bool { return false }
```

#### PlanModePromptSection

```go
// PlanModePromptSection 是 blades.PromptBuilder 的动态 section。
// Plan 模式激活时注入只读指令和计划文件路径。
type PlanModePromptSection struct {
    modeManager *permission.ModeManager
    planDir     string
}

func (s *PlanModePromptSection) Name() string     { return "plan_mode" }
func (s *PlanModePromptSection) Priority() int     { return 50 }

func (s *PlanModePromptSection) Build(ctx context.Context) (string, error) {
    if s.modeManager.Current() != permission.ModePlan {
        return "", nil
    }
    sessionID := session.IDFromContext(ctx)
    planPath := filepath.Join(s.planDir, sessionID+"-*.md") // 支持多计划：<sessionID>-<planName>.md
    return fmt.Sprintf(`=== PLAN MODE ===
当前处于只读计划模式。禁止执行任何写入操作。

工作流程：
1. 使用只读工具探索代码，理解现有模式
2. 设计实现方案，考虑多种方案的权衡
3. 使用 WritePlanTool 将计划写入 %s （多次调用可写多个计划，每次传入不同 plan_name）
4. 调用 ExitPlanMode 提交审批
`, planPath), nil
}
```

#### FilterToolsForMode

```go
// FilterToolsForMode 根据当前模式过滤工具列表。
// Plan 模式下只保留只读工具 + plan 工具 + PlanModeTool 声明的工具。
// 其他模式返回完整工具列表。
func FilterToolsForMode(allTools []tools.Tool, mode permission.Mode) []tools.Tool {
    if mode != permission.ModePlan {
        return allTools
    }
    filtered := make([]tools.Tool, 0, len(allTools))
    for _, t := range allTools {
        if t.Name() == "EnterPlanMode" || t.Name() == "ExitPlanMode" || t.Name() == "WritePlan" {
            filtered = append(filtered, t)
            continue
        }
        if rt, ok := t.(tools.ReadOnlyTool); ok && rt.IsReadOnly() {
            filtered = append(filtered, t)
            continue
        }
        if pt, ok := t.(PlanModeTool); ok && pt.AvailableInPlanMode() {
            filtered = append(filtered, t)
            continue
        }
    }
    return filtered
}

// PlanModeTool 是可选接口，声明非只读工具在 plan 模式下仍可用。
// 例如 AskUserQuestion 需要用户交互但不修改状态。
type PlanModeTool interface {
    AvailableInPlanMode() bool
}

// WritePlanTool 是 plan 模式专用的写入工具。
// 只能写入 planDir 下的计划文件，不能写入其他路径。
// 这解决了 plan 模式过滤掉所有写入工具后模型无法写计划文件的矛盾。
type WritePlanTool struct {
    planDir string
}

func NewWritePlanTool(planDir string) tools.Tool

func (t *WritePlanTool) Handle(ctx context.Context, input string) (string, error) {
    var params struct {
        PlanName string `json:"plan_name"` // 计划名称，支持多计划共存
        Content  string `json:"content"`
    }
    if err := json.Unmarshal([]byte(input), &params); err != nil {
        return "", err
    }
    if params.PlanName == "" {
        params.PlanName = "plan"
    }
    sessionID := session.IDFromContext(ctx)
    // 防御路径遍历：sessionID 和 planName 只能包含字母数字和连字符
    if !isSafeFilename(sessionID) || !isSafeFilename(params.PlanName) {
        return "", fmt.Errorf("invalid session ID or plan name")
    }
    planPath := filepath.Join(t.planDir, sessionID+"-"+params.PlanName+".md")
    // 二次校验：确保最终路径在 planDir 内
    rel, err := filepath.Rel(t.planDir, planPath)
    if err != nil || strings.HasPrefix(rel, "..") {
        return "", fmt.Errorf("invalid plan path")
    }
    if err := os.WriteFile(planPath, []byte(params.Content), 0644); err != nil {
        return "", err
    }
    return fmt.Sprintf("计划已写入 %s", planPath), nil
}

func (t *WritePlanTool) IsReadOnly() bool { return false }
```

### 14.3 Auto Mode（纯接口设计）

Auto Mode 使用可插拔 AI 分类器自主决策工具调用的权限。框架只定义接口和控制器，不提供内置分类器实现。

#### Classifier 接口

```go
package permission

// Classifier 评估工具调用是否应被批准或拒绝，无需用户交互。
// 实现可以是 LLM 调用、规则引擎、本地模型等。
type Classifier interface {
    Classify(ctx context.Context, req *ClassifyRequest) (*ClassifyResult, error)
}

// ClassifyRequest 包含分类器决策所需的全部上下文。
type ClassifyRequest struct {
    ToolName     string           `json:"toolName"`
    ToolInput    string           `json:"toolInput"`
    Messages     []*model.Message `json:"messages"`
    SystemPrompt string           `json:"systemPrompt"`
    Rules        ClassifierRules  `json:"rules"`
}

// ClassifierRules 是用户可配置的规则，注入分类器的决策上下文。
type ClassifierRules struct {
    Allow       []string `json:"allow"`       // 应批准的动作描述
    SoftDeny    []string `json:"softDeny"`    // 应拒绝的动作描述
    Environment []string `json:"environment"` // 环境上下文提示
}

// ClassifyResult 是分类器的决策结果。
type ClassifyResult struct {
    ShouldBlock bool              `json:"shouldBlock"`
    Reason      string            `json:"reason"`
    Thinking    string            `json:"thinking,omitempty"`
    Unavailable bool              `json:"unavailable,omitempty"` // 分类器不可用（API 错误等）
    Usage       *model.TokenUsage `json:"usage,omitempty"`
}
```

#### AutoModeController

```go
// AutoModeController 管理 auto 模式的决策流程。
// 包含 acceptEdits 快速路径、熔断器和分类器调用。
type AutoModeController struct {
    classifier    Classifier
    modeManager   *ModeManager
    denialTracker *DenialTracker
    acceptEdits   *AcceptEditsChecker // 快速路径：acceptEdits 安全的工具跳过分类器
    rules         ClassifierRules     // 用户配置的分类器规则
}

func NewAutoModeController(classifier Classifier, mm *ModeManager, opts ...AutoModeOption) *AutoModeController

// Evaluate 运行 auto 模式决策管线。
// 返回 PermissionAllow/PermissionDeny/PermissionAsk（fallback 到用户提示）。
func (c *AutoModeController) Evaluate(
    ctx context.Context, toolName, input string, messages []*model.Message, systemPrompt string,
) (PermissionDecision, error) {
    // 1. 快速路径：acceptEdits 安全的工具直接批准，跳过分类器
    if c.acceptEdits != nil {
        if allowed, _ := c.acceptEdits.Check(ctx, toolName, input); allowed {
            return PermissionAllow, nil
        }
    }

    // 2. 熔断器检查：连续拒绝过多 → 降级到用户提示
    if c.denialTracker.ShouldFallback() {
        // 触发系统降级：auto → default
        _ = c.modeManager.Transition(ModeTransition{
            From: ModeAuto, To: ModeDefault, Source: TransitionSystem,
        })
        return PermissionAsk, nil
    }

    // 3. 运行分类器
    result, err := c.classifier.Classify(ctx, &ClassifyRequest{
        ToolName:     toolName,
        ToolInput:    input,
        Messages:     messages,
        SystemPrompt: systemPrompt,
        Rules:        c.rules,
    })
    if err != nil || result.Unavailable {
        return PermissionAsk, nil // 分类器不可用 → 提示用户，不是拒绝
    }

    // 4. 更新熔断器状态
    if result.ShouldBlock {
        c.denialTracker.RecordDenial()
        return PermissionDeny, nil
    }
    c.denialTracker.RecordSuccess()
    return PermissionAllow, nil
}
```

#### DenialTracker 熔断器

```go
// DenialTracker 跟踪连续和总拒绝次数。
// 超过阈值时触发降级，防止 Agent 陷入拒绝循环。
type DenialTracker struct {
    mu                 sync.Mutex
    consecutiveDenials int
    totalDenials       int
    maxConsecutive     int // 默认 3
    maxTotal           int // 默认 20
}

func NewDenialTracker(maxConsecutive, maxTotal int) *DenialTracker

func (d *DenialTracker) RecordDenial() {
    d.mu.Lock()
    defer d.mu.Unlock()
    d.consecutiveDenials++
    d.totalDenials++
}

func (d *DenialTracker) RecordSuccess() {
    d.mu.Lock()
    defer d.mu.Unlock()
    d.consecutiveDenials = 0
}

func (d *DenialTracker) ShouldFallback() bool {
    d.mu.Lock()
    defer d.mu.Unlock()
    return d.consecutiveDenials >= d.maxConsecutive || d.totalDenials >= d.maxTotal
}

// Reset 清除所有计数。用户手动批准操作后调用，表示继续使用 auto 模式。
func (d *DenialTracker) Reset() {
    d.mu.Lock()
    defer d.mu.Unlock()
    d.consecutiveDenials = 0
    d.totalDenials = 0
}
```

### 14.4 Accept Edits Mode

Accept Edits 模式自动批准工作目录内的文件写入工具，其他工具（Bash、MCP 等）仍需确认。

```go
// AcceptEditsChecker 检查工具调用是否在 acceptEdits 模式下可自动批准。
type AcceptEditsChecker struct {
    workingDir         string
    additionalDirs     []string
    fileWriteToolNames map[string]bool // 可自动批准的工具名，如 {"FileWrite": true, "FileEdit": true}
}

func NewAcceptEditsChecker(workingDir string, fileWriteTools []string, opts ...AcceptEditsOption) *AcceptEditsChecker

// Check 返回 true 表示该工具调用可在 acceptEdits 模式下自动批准。
func (c *AcceptEditsChecker) Check(ctx context.Context, toolName, input string) (bool, error) {
    if !c.fileWriteToolNames[toolName] {
        return false, nil
    }
    filePath, err := extractFilePath(toolName, input)
    if err != nil {
        return false, nil // 无法确定路径，不自动批准
    }
    absPath, err := filepath.Abs(filePath)
    if err != nil {
        return false, nil
    }
    // 解析符号链接，防止通过 symlink 逃逸工作目录
    // 例如：workdir/link -> /etc/passwd，filepath.Rel 会认为在目录内
    realPath, err := filepath.EvalSymlinks(absPath)
    if err != nil {
        return false, nil // 无法解析，不自动批准
    }
    return c.isInAllowedDirectory(realPath), nil
}

// isInAllowedDirectory 检查路径是否在允许的目录内。
// 使用 filepath.Rel 防止路径遍历攻击（../../../etc/passwd）。
func (c *AcceptEditsChecker) isInAllowedDirectory(absPath string) bool {
    dirs := append([]string{c.workingDir}, c.additionalDirs...)
    for _, dir := range dirs {
        rel, err := filepath.Rel(dir, absPath)
        if err != nil {
            continue
        }
        if !strings.HasPrefix(rel, "..") {
            return true
        }
    }
    return false
}

type AcceptEditsOption func(*AcceptEditsChecker)

func WithAdditionalDirectories(dirs ...string) AcceptEditsOption {
    return func(c *AcceptEditsChecker) {
        c.additionalDirs = append(c.additionalDirs, dirs...)
    }
}
```

### 14.5 安全不可绕过检查（Safety Invariants）

SafetyChecker 在权限决策链最前面执行，任何模式都无法绕过。

```go
// SafetyChecker 执行 bypass-immune 安全检查。
// 在权限链第 1 层运行，任何模式（包括未来的 bypass_permissions）都无法绕过。
type SafetyChecker interface {
    Check(ctx context.Context, toolName, input string) *SafetyViolation
}

// SafetyViolation 描述安全违规。
type SafetyViolation struct {
    Reason   string        `json:"reason"`
    Severity SafetySeverity `json:"severity"`
}

// SafetySeverity 区分安全违规的严重程度。
type SafetySeverity int
const (
    // SeverityBlock 绝对禁止，任何机制都无法覆盖。
    // 用于路径遍历、跨机器攻击等。
    SeverityBlock SafetySeverity = iota

    // SeverityConfirm 需要用户确认，但不绝对禁止。
    // 用于敏感文件写入（.git/hooks、.blades/settings.json 等），
    // 用户可能有合理理由需要写入这些文件。
    // 在 auto 模式下，分类器可以覆盖此级别。
    SeverityConfirm
)

// DefaultSafetyChecker 实现常见安全检查。
type DefaultSafetyChecker struct {
    workingDir     string
    sensitiveGlobs []string // 敏感文件模式，如 ".git/**", ".blades/**", "~/.ssh/**"
}

func NewDefaultSafetyChecker(workingDir string) *DefaultSafetyChecker

func (c *DefaultSafetyChecker) Check(ctx context.Context, toolName, input string) *SafetyViolation {
    // 检查 1：路径遍历检测（SeverityBlock）
    // 拒绝通过符号链接解析或编码路径组件逃逸工作目录的输入。
    if c.isPathTraversal(toolName, input) {
        return &SafetyViolation{Reason: "path traversal attempt detected", Severity: SeverityBlock}
    }

    // 检查 2：敏感文件保护（SeverityConfirm）
    // 标记对 .git/、.blades/、shell 配置等敏感路径的写入。
    // 使用 SeverityConfirm 而非 SeverityBlock，因为用户可能有合理理由
    // 写入这些文件（如配置 git hooks、更新 .blades/settings.json）。
    if c.isSensitiveFilePath(toolName, input) {
        return &SafetyViolation{Reason: "write to sensitive file path", Severity: SeverityConfirm}
    }

    return nil
}
```

### 14.6 模式与 Agent Loop 集成

交互模式系统通过以下方式与 Agent Loop（模块 1）集成：

**ContextBuilder 集成：**

```go
// ContextBuilder.Build 在构建 model.Request 时调用 FilterToolsForMode，
// 根据当前模式过滤发送给模型的工具列表。
func (b *ContextBuilder) Build(ctx context.Context, sess session.Session, allTools []tools.Tool) (*model.Request, error) {
    mode := b.modeManager.Current()
    tools := FilterToolsForMode(allTools, mode)
    // ... 其余构建逻辑
}
```

**PromptBuilder 集成：**

```go
// Agent 构造时注册 PlanModePromptSection 为动态 section。
// Plan 模式激活时自动注入只读指令。
func NewAgent(opts ...AgentOption) Agent {
    // ...
    a.promptBuilder.AddDynamic(planmode.NewPlanModePromptSection(a.modeManager, a.planDir))
    // ...
}
```

**AgentOption 注入：**

```go
func WithModeManager(mm *permission.ModeManager) AgentOption
func WithClassifier(c permission.Classifier) AgentOption
func WithAcceptEditsChecker(c *permission.AcceptEditsChecker) AgentOption
func WithSafetyChecker(c permission.SafetyChecker) AgentOption
```

**ModeManager.OnChange 事件：**

```go
// 模式变更时发射 Hook 事件，供可观测性和 UI 使用。
modeManager.OnChange(func(from, to permission.Mode) {
    hookRegistry.Emit(ctx, &hook.HookModeChange{From: from, To: to})
})
```

**数据流：**

```
用户切换模式（CLI/命令）
  │
  ├─→ ModeManager.Transition()
  │     ├─→ 验证转换规则
  │     ├─→ 更新 ModeState
  │     └─→ 触发 OnChange → HookModeChange
  │
  ├─→ 下一轮 ContextBuilder.Build()
  │     ├─→ FilterToolsForMode（plan 模式过滤写入工具）
  │     └─→ PlanModePromptSection（plan 模式注入指令）
  │
  └─→ 工具执行时 PermissionChain.Check()
        ├─→ 第 3 层：模式决策（bubble → BubbleEscalator 委托父 agent）
        └─→ 第 7 层：后处理（auto → Classifier / dont_ask → DENY / bypass_permissions → ALLOW）
```

### 14.7 关键设计决策

1. **7 种模式覆盖完整场景** — 4 种核心交互模式（default/accept_edits/plan/auto）处理日常开发场景。新增 3 种扩展模式：bubble 模式实现子 agent 权限委托，避免重复配置父 agent 的权限规则；dont_ask 和 bypass_permissions 是 SDK/headless 场景的必要模式，前者将 ASK 转为 DENY（安全优先），后者将 ASK 转为 ALLOW（效率优先）。deny_all 预留枚举值，后续按需实现。

2. **纯接口 Classifier** — 框架不内置 LLM 分类器实现，只提供 `Classifier` 接口和 `AutoModeController`（快速路径 + 熔断器）。原因：分类器质量高度依赖具体模型和 prompt 工程，框架不应承担这个责任。用户可以注入基于 Claude/GPT 的 LLM 分类器、基于规则的分类器、或本地小模型分类器。

3. **Plan 模式过滤工具列表 + WritePlanTool** — Plan 模式下从发送给模型的工具列表中移除写入工具，但保留专用的 `WritePlanTool`（只能写入 planDir）。这解决了「过滤写入工具」和「模型需要写计划文件」之间的矛盾。权限链仍作为双重保障。

4. **安全检查双级别** — SafetyViolation 区分 `SeverityBlock`（绝对禁止，如路径遍历）和 `SeverityConfirm`（需确认，如敏感文件写入）。敏感文件不是绝对禁止——用户可能需要配置 git hooks 或更新 .blades/settings.json。SeverityConfirm 跳过模式决策直接进入后处理层（auto 模式下分类器可覆盖）。

5. **模式转换状态机** — 显式转换规则表防止非法转换。例如 auto 模式只能通过 system 来源降级到 default（熔断器触发），不能通过 tool 来源直接跳到其他模式。Plan 模式的暂存/恢复机制确保退出 plan 后恢复用户之前的模式选择。

6. **模式切换时序** — 模式切换立即生效于 ModeState。权限链在每次工具执行时读取当前模式，因此当前轮次的后续工具调用立即受影响。工具列表过滤和 prompt 注入在下一轮 ContextBuilder.Build() 时体现。

7. **AllowedPrompts 闭环** — ExitPlanModeTool 的 AllowedPrompts 在用户审批后直接转为 session 级 PermissionRule，由 PermissionChain 的规则匹配层处理。语义描述作为 pattern 字段，规则匹配器负责解释。

8. **Bubble 模式实现子 agent 权限委托** — 子 agent 使用 ModeBubble 时，权限决策通过 BubbleEscalator 转发到父 agent 的 PermissionChain，父链运行完整的 7 层决策管线。这避免了在子 agent 中重复配置父 agent 的权限规则（规则、分类器、用户交互等），同时保证父 agent 对子 agent 的工具调用拥有完整的权限控制。Bubble 模式下子 agent 不做任何自主权限决策。

9. **dontAsk 和 bypassPermissions 面向 SDK/headless 场景** — 在 SDK 集成、CI 流水线、后台任务等无人交互场景中，ASK 决策无法提示用户。ModeDontAsk 将 ASK 转为 DENY，适用于安全优先的场景（CI 流水线、不受信任的输入）；ModeBypassPermissions 将 ASK 转为 ALLOW，适用于受信任环境（内部工具链、预审批的自动化任务）。两种模式都在第 7 层后处理中生效，SafetyChecker（第 1 层）仍然 bypass-immune——SeverityBlock 绝对拒绝，确保即使在 bypass_permissions 模式下也无法执行路径遍历等危险操作。
