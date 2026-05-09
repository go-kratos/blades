package blades

import (
	"context"
	"sync"

	"github.com/go-kratos/blades/compact"
	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/event"
	"github.com/go-kratos/blades/hook"
	"github.com/go-kratos/blades/internal/convert"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/policy"
	"github.com/go-kratos/blades/prompt"
	"github.com/go-kratos/blades/session"
	"github.com/go-kratos/blades/tools"
)

// Agent is the core interface for all agents in the system.
type Agent interface {
	Name() string
	Description() string
	Run(ctx context.Context, input <-chan event.Input) (<-chan event.Output, error)
}

// llmAgent is the default Agent implementation backed by an LLM provider.
type llmAgent struct {
	name          string
	description   string
	provider      model.Provider
	tools         []tools.Tool
	resolver      tools.Resolver
	policy        policy.Policy
	hooks         []hook.Hook
	compactor     compact.Compactor
	promptBuilder prompt.Builder
}

// NewAgent creates a new default LLM-backed Agent.
func NewAgent(name string, opts ...AgentOption) (Agent, error) {
	a := &llmAgent{name: name}
	for _, opt := range opts {
		opt(a)
	}
	if a.provider == nil {
		return nil, ErrModelProviderRequired
	}
	return a, nil
}

func (a *llmAgent) Name() string       { return a.name }
func (a *llmAgent) Description() string { return a.description }

// Run implements the Agent interface.
func (a *llmAgent) Run(ctx context.Context, input <-chan event.Input) (<-chan event.Output, error) {
	allTools, err := a.resolveTools(ctx)
	if err != nil {
		return nil, err
	}
	sess := session.Ensure(ctx)
	output := make(chan event.Output, 64)
	go a.loop(ctx, input, output, sess, allTools)
	return output, nil
}

// loop is the agent's main execution loop, structured after pi-agent's runLoop:
//   - Outer for: follow-up loop (re-enters when new input arrives after turn completes)
//   - Inner for: step loop (model call → tool wave → repeat while hasMore)
//   - consumeSteering: non-blocking drain (= pi-agent's getSteeringMessages)
//   - awaitFollowUp: blocking wait (= pi-agent's getFollowUpMessages)
func (a *llmAgent) loop(ctx context.Context, input <-chan event.Input, output chan<- event.Output, sess session.Session, allTools []tools.Tool) {
	defer func() {
		output <- event.Done{}
		close(output)
	}()

	var turnNum int

	// Wait for the first Prompt to start
	if !a.awaitFollowUp(ctx, input, sess, output) {
		return
	}

	for {
		turnNum++
		hasMore := true
		var (
			totalUsage event.Usage
			finalParts []content.Part
			stopReason event.StopReason
		)

		// BeforeTurn hooks
		for _, h := range a.hooks {
			if err := h.BeforeTurn(ctx, &hook.Turn{AgentName: a.name, Turn: turnNum}); err != nil {
				if hook.IsAbort(err) {
					output <- event.TurnEnd{StopReason: event.StopAbort}
					return
				}
				output <- event.Error{Err: err}
				return
			}
		}

		// Step loop (= pi-agent inner while)
		for hasMore {
			req, err := a.buildRequest(ctx, sess, allTools)
			if err != nil {
				output <- event.Error{Err: err}
				return
			}

			for _, h := range a.hooks {
				if err := h.BeforeModel(ctx, req); err != nil {
					if hook.IsAbort(err) {
						output <- event.TurnEnd{StopReason: event.StopAbort}
						return
					}
					output <- event.Error{Err: err}
					return
				}
			}

			resp, stepUsage, err := a.streamStep(ctx, req, output)
			if err != nil {
				for _, h := range a.hooks {
					_ = h.AfterModel(ctx, req, nil, err)
				}
				output <- event.Error{Err: err}
				return
			}

			for _, h := range a.hooks {
				if err := h.AfterModel(ctx, req, resp, nil); err != nil {
					if hook.IsAbort(err) {
						output <- event.TurnEnd{StopReason: event.StopAbort}
						return
					}
					output <- event.Error{Err: err}
					return
				}
			}

			totalUsage.InputTokens += stepUsage.InputTokens
			totalUsage.OutputTokens += stepUsage.OutputTokens

			if resp.Message != nil {
				_ = sess.Append(ctx, resp.Message)
				finalParts = resp.Message.Parts
			}

			output <- event.StepEnd{
				StopReason: event.StopReason(resp.StopReason),
				Usage:      stepUsage,
			}

			// Check tool calls
			hasMore = false
			if toolUses := extractToolUses(resp.Message); len(toolUses) > 0 {
				control, err := a.executeToolWave(ctx, toolUses, allTools, output, sess, turnNum)
				if err != nil {
					output <- event.Error{Err: err}
					return
				}
				if control != nil {
					output <- control
					stopReason = event.StopToolUse
					break
				}
				hasMore = true
			} else {
				stopReason = event.StopReason(resp.StopReason)
			}

			// Poll steering (= pi-agent's getSteeringMessages at end of each step)
			if a.consumeSteering(ctx, input, sess) {
				hasMore = true
			}
		}

		// Emit TurnEnd
		output <- event.TurnEnd{
			Parts:      finalParts,
			StopReason: stopReason,
			Usage:      totalUsage,
		}

		// AfterTurn hooks
		summary := &hook.TurnSummary{
			Parts:      finalParts,
			StopReason: model.StopReason(stopReason),
			Usage:      &model.Usage{InputTokens: totalUsage.InputTokens, OutputTokens: totalUsage.OutputTokens},
		}
		for _, h := range a.hooks {
			_ = h.AfterTurn(ctx, &hook.Turn{AgentName: a.name, Turn: turnNum}, summary, nil)
		}

		// Wait for follow-up (= pi-agent's getFollowUpMessages)
		if !a.awaitFollowUp(ctx, input, sess, output) {
			return
		}
	}
}

// consumeSteering non-blocking drains Steer events from input into session.
// Returns true if any messages were consumed (step loop should continue).
func (a *llmAgent) consumeSteering(ctx context.Context, input <-chan event.Input, sess session.Session) bool {
	consumed := false
	for {
		select {
		case in, ok := <-input:
			if !ok {
				return consumed
			}
			switch v := in.(type) {
			case event.Steer:
				_ = sess.Append(ctx, convert.SteerToMessage(v))
				consumed = true
			case event.Abort:
				return false
			}
		default:
			return consumed
		}
	}
}

// awaitFollowUp blocks until a Prompt or Steer arrives. Returns false to exit.
func (a *llmAgent) awaitFollowUp(ctx context.Context, input <-chan event.Input, sess session.Session, output chan<- event.Output) bool {
	select {
	case <-ctx.Done():
		output <- event.Error{Err: ctx.Err()}
		return false
	case in, ok := <-input:
		if !ok {
			return false
		}
		switch v := in.(type) {
		case event.Prompt:
			_ = sess.Append(ctx, convert.PromptToMessage(v))
			return true
		case event.Steer:
			_ = sess.Append(ctx, convert.SteerToMessage(v))
			return true
		case event.Abort:
			return false
		}
		return false
	}
}

// streamStep calls the provider and streams chunks to the output channel.
func (a *llmAgent) streamStep(ctx context.Context, req *model.Request, output chan<- event.Output) (*model.Response, event.Usage, error) {
	var (
		parts      []content.Part
		stopReason model.StopReason
		usage      model.Usage
		mu         sync.Mutex
	)

	for chunk, err := range a.provider.Stream(ctx, req) {
		if err != nil {
			return nil, event.Usage{}, err
		}
		if chunk == nil {
			continue
		}
		for _, o := range convert.ChunkToOutputs(chunk) {
			output <- o
		}
		mu.Lock()
		parts = append(parts, chunk.Parts...)
		if chunk.StopReason != "" {
			stopReason = chunk.StopReason
		}
		if chunk.Usage != nil {
			usage.InputTokens += chunk.Usage.InputTokens
			usage.OutputTokens += chunk.Usage.OutputTokens
		}
		mu.Unlock()
	}

	return &model.Response{
		Message:    &model.Message{Role: model.RoleAssistant, Parts: parts},
		StopReason: stopReason,
		Usage:      usage,
	}, event.Usage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens}, nil
}

// executeToolWave runs a batch of tool calls, emitting lifecycle events.
func (a *llmAgent) executeToolWave(ctx context.Context, calls []content.ToolUse, allTools []tools.Tool, output chan<- event.Output, sess session.Session, turnNum int) (event.Output, error) {
	for _, call := range calls {
		output <- event.ToolStart{ID: call.ID, Name: call.Name, Input: call.Input}
	}

	for _, call := range calls {
		for _, h := range a.hooks {
			if err := h.BeforeTool(ctx, &hook.ToolCall{AgentName: a.name, Turn: turnNum, Input: call.Input}); err != nil {
				if hook.IsAbort(err) {
					return nil, err
				}
			}
		}
	}

	results := a.executeTools(ctx, calls, allTools)

	var (
		toolResults []content.ToolResult
		controlOut  event.Output
	)
	for _, r := range results {
		output <- event.ToolEnd{ID: r.ID, Name: r.Name, Parts: r.Parts, IsError: r.IsError}
		toolResults = append(toolResults, r.ToolResult)
		if r.Control != nil {
			controlOut = r.Control
		}
	}

	if err := sess.Append(ctx, convert.ToolResultToMessage(toolResults)); err != nil {
		return nil, err
	}

	for _, h := range a.hooks {
		for i, call := range calls {
			var result *tools.Result
			if i < len(results) {
				result = &tools.Result{Parts: results[i].Parts}
			}
			_ = h.AfterTool(ctx, &hook.ToolCall{AgentName: a.name, Turn: turnNum, Input: call.Input}, result, nil)
		}
	}

	return controlOut, nil
}

// toolExecResult pairs a tool result with an optional control signal.
type toolExecResult struct {
	content.ToolResult
	Control event.Output
}

func (a *llmAgent) executeTools(ctx context.Context, calls []content.ToolUse, allTools []tools.Tool) []toolExecResult {
	results := make([]toolExecResult, len(calls))
	toolMap := make(map[string]tools.Tool, len(allTools))
	for _, t := range allTools {
		toolMap[t.Spec().Name] = t
	}
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = a.executeSingleTool(ctx, call, toolMap)
		}()
	}
	wg.Wait()
	return results
}

func (a *llmAgent) executeSingleTool(ctx context.Context, call content.ToolUse, toolMap map[string]tools.Tool) toolExecResult {
	tool, ok := toolMap[call.Name]
	if !ok {
		return toolExecResult{
			ToolResult: content.ToolResult{ID: call.ID, Name: call.Name, Parts: []content.Part{content.Text{Text: "tool not found: " + call.Name}}, IsError: true},
		}
	}

	input := call.Input
	if a.policy != nil {
		decision, err := a.policy.Check(ctx, policy.ToolRequest{Tool: tool, Input: input})
		if err != nil || decision.Action == policy.Deny {
			reason := "denied by policy"
			if err != nil {
				reason = err.Error()
			} else if decision.Reason != "" {
				reason = decision.Reason
			}
			return toolExecResult{
				ToolResult: content.ToolResult{ID: call.ID, Name: call.Name, Parts: []content.Part{content.Text{Text: reason}}, IsError: true},
			}
		}
		if decision.Action == policy.Modify && decision.Modified != nil {
			input = decision.Modified.Input
		}
	}

	tc := &toolCtx{id: call.ID, spec: tool.Spec()}
	res, err := tool.Handle(tools.NewContext(ctx, tc), input)
	if err != nil {
		if le, ok := tools.IsLoopExit(err); ok {
			return toolExecResult{
				ToolResult: content.ToolResult{ID: call.ID, Name: call.Name, Parts: []content.Part{content.Text{Text: "loop exit"}}},
				Control:    event.LoopExit{ToolID: call.ID, ToolName: call.Name, Escalate: le.Escalate},
			}
		}
		if h, ok := tools.IsHandoff(err); ok {
			return toolExecResult{
				ToolResult: content.ToolResult{ID: call.ID, Name: call.Name, Parts: []content.Part{content.Text{Text: "handoff to " + h.Agent}}},
				Control:    event.Handoff{ToolID: call.ID, ToolName: call.Name, Agent: h.Agent},
			}
		}
		return toolExecResult{
			ToolResult: content.ToolResult{ID: call.ID, Name: call.Name, Parts: []content.Part{content.Text{Text: "tool error: " + err.Error()}}, IsError: true},
		}
	}

	var parts []content.Part
	if res != nil {
		parts = res.Parts
	}
	return toolExecResult{
		ToolResult: content.ToolResult{ID: call.ID, Name: call.Name, Parts: parts},
	}
}

func extractToolUses(msg *model.Message) []content.ToolUse {
	if msg == nil {
		return nil
	}
	var calls []content.ToolUse
	for _, p := range msg.Parts {
		if tu, ok := p.(content.ToolUse); ok {
			calls = append(calls, tu)
		}
	}
	return calls
}

func (a *llmAgent) resolveTools(ctx context.Context) ([]tools.Tool, error) {
	allTools := make([]tools.Tool, 0, len(a.tools))
	allTools = append(allTools, a.tools...)
	if a.resolver != nil {
		resolved, err := a.resolver.List(ctx)
		if err != nil {
			return nil, err
		}
		allTools = append(allTools, resolved...)
	}
	return allTools, nil
}

func (a *llmAgent) buildRequest(ctx context.Context, sess session.Session, allTools []tools.Tool) (*model.Request, error) {
	var msgs []*model.Message
	if sess != nil {
		var err error
		msgs, err = sess.Messages(ctx)
		if err != nil {
			return nil, err
		}
	}
	if a.compactor != nil {
		var err error
		msgs, err = a.compactor.Compact(ctx, msgs)
		if err != nil {
			return nil, err
		}
	}
	var system string
	if a.promptBuilder != nil {
		parts, err := a.promptBuilder.Build(ctx)
		if err != nil {
			return nil, err
		}
		system = partsToSystemText(parts)
	}
	var toolSpecs []tools.ToolSpec
	for _, t := range allTools {
		toolSpecs = append(toolSpecs, t.Spec())
	}
	return &model.Request{
		Model:    a.provider.Name(),
		System:   system,
		Messages: msgs,
		Tools:    toolSpecs,
	}, nil
}

func partsToSystemText(parts []content.Part) string {
	var text string
	for _, p := range parts {
		if t, ok := p.(content.Text); ok {
			if text != "" {
				text += "\n\n"
			}
			text += t.Text
		}
	}
	return text
}

type toolCtx struct {
	id   string
	spec tools.ToolSpec
}

func (t *toolCtx) ID() string         { return t.id }
func (t *toolCtx) Spec() tools.ToolSpec { return t.spec }
