package blades

import (
	"context"
	"encoding/json"
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
	maxSteps      int
}

// NewAgent creates a new default LLM-backed Agent.
func NewAgent(name string, opts ...AgentOption) (Agent, error) {
	a := &llmAgent{
		name:     name,
		maxSteps: 10,
	}
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

	go a.runLoop(ctx, input, output, sess, allTools)
	return output, nil
}

func (a *llmAgent) runLoop(ctx context.Context, input <-chan event.Input, output chan<- event.Output, sess session.Session, allTools []tools.Tool) {
	defer func() {
		output <- event.Done{}
		close(output)
	}()

	var turnCount int
	for {
		select {
		case <-ctx.Done():
			output <- event.Error{Err: ctx.Err()}
			return
		case in, ok := <-input:
			if !ok {
				return
			}
			switch v := in.(type) {
			case event.Prompt:
				turnCount++
				err := a.executeTurn(ctx, v, output, sess, allTools, turnCount)
				if err != nil {
					output <- event.Error{Err: err}
					return
				}
			case event.Abort:
				return
			default:
			}
		}
	}
}

func (a *llmAgent) executeTurn(ctx context.Context, p event.Prompt, output chan<- event.Output, sess session.Session, allTools []tools.Tool, turnNum int) error {
	for _, h := range a.hooks {
		if err := h.BeforeTurn(ctx, &hook.Turn{AgentName: a.name, Turn: turnNum, Input: p}); err != nil {
			if hook.IsAbort(err) {
				output <- event.TurnEnd{StopReason: event.StopAbort}
				return nil
			}
			return err
		}
	}

	userMsg := convert.PromptToMessage(p)
	if err := sess.Append(ctx, userMsg); err != nil {
		return err
	}

	var (
		totalUsage event.Usage
		finalParts []content.Part
		stopReason event.StopReason
	)

	for step := 0; step < a.maxSteps; step++ {
		req, err := a.buildRequest(ctx, sess, allTools)
		if err != nil {
			return err
		}

		for _, h := range a.hooks {
			if err := h.BeforeModel(ctx, req); err != nil {
				if hook.IsAbort(err) {
					output <- event.TurnEnd{StopReason: event.StopAbort}
					return nil
				}
				return err
			}
		}

		resp, stepUsage, err := a.streamStep(ctx, req, output)
		if err != nil {
			for _, h := range a.hooks {
				_ = h.AfterModel(ctx, req, nil, err)
			}
			return err
		}

		for _, h := range a.hooks {
			if err := h.AfterModel(ctx, req, resp, nil); err != nil {
				if hook.IsAbort(err) {
					output <- event.TurnEnd{StopReason: event.StopAbort}
					return nil
				}
				return err
			}
		}

		totalUsage.InputTokens += stepUsage.InputTokens
		totalUsage.OutputTokens += stepUsage.OutputTokens

		if resp.Message != nil {
			if err := sess.Append(ctx, resp.Message); err != nil {
				return err
			}
			finalParts = resp.Message.Parts
		}

		output <- event.StepEnd{
			Index:      step,
			StopReason: event.StopReason(resp.StopReason),
			Usage:      stepUsage,
		}

		toolCalls := extractToolCalls(resp.Message)
		if len(toolCalls) == 0 {
			stopReason = event.StopReason(resp.StopReason)
			break
		}

		controlEvent, err := a.executeToolWave(ctx, toolCalls, allTools, output, sess, turnNum)
		if err != nil {
			return err
		}
		if controlEvent != nil {
			output <- controlEvent
			stopReason = event.StopToolUse
			break
		}
		stopReason = event.StopToolUse
	}

	output <- event.TurnEnd{
		Parts:      finalParts,
		StopReason: stopReason,
		Usage:      totalUsage,
	}

	summary := &hook.TurnSummary{
		Parts:      finalParts,
		StopReason: model.StopReason(stopReason),
		Usage:      &model.Usage{InputTokens: totalUsage.InputTokens, OutputTokens: totalUsage.OutputTokens},
	}
	for _, h := range a.hooks {
		_ = h.AfterTurn(ctx, &hook.Turn{AgentName: a.name, Turn: turnNum, Input: p}, summary, nil)
	}

	return nil
}

func (a *llmAgent) streamStep(ctx context.Context, req *model.Request, output chan<- event.Output) (*model.Response, event.Usage, error) {
	var (
		parts      []content.Part
		stopReason model.StopReason
		usage      model.Usage
		mu         sync.Mutex
	)

	stream := a.provider.Stream(ctx, req)
	for chunk, err := range stream {
		if err != nil {
			return nil, event.Usage{}, err
		}
		if chunk == nil {
			continue
		}

		outputs := convert.ChunkToOutputs(chunk)
		for _, o := range outputs {
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

	resp := &model.Response{
		Message:    &model.Message{Role: model.RoleAssistant, Parts: parts},
		StopReason: stopReason,
		Usage:      usage,
	}
	return resp, event.Usage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens}, nil
}

func (a *llmAgent) executeToolWave(ctx context.Context, calls []toolCall, allTools []tools.Tool, output chan<- event.Output, sess session.Session, turnNum int) (event.Output, error) {
	for _, call := range calls {
		output <- event.ToolStart{ID: call.ID, Name: call.Name, Input: call.Input}
	}

	for _, call := range calls {
		for _, h := range a.hooks {
			tc := &hook.ToolCall{AgentName: a.name, Turn: turnNum, Input: call.Input}
			if err := h.BeforeTool(ctx, tc); err != nil {
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

		toolResults = append(toolResults, content.ToolResult{
			ID:      r.ID,
			Name:    r.Name,
			Parts:   r.Parts,
			IsError: r.IsError,
		})

		if r.Control != nil {
			controlOut = r.Control
		}
	}

	toolMsg := convert.ToolResultToMessage(toolResults)
	if err := sess.Append(ctx, toolMsg); err != nil {
		return nil, err
	}

	for _, h := range a.hooks {
		for i, call := range calls {
			var toolResult *tools.Result
			if i < len(results) {
				toolResult = &tools.Result{Parts: results[i].Parts}
			}
			_ = h.AfterTool(ctx, &hook.ToolCall{AgentName: a.name, Turn: turnNum, Input: call.Input}, toolResult, nil)
		}
	}

	return controlOut, nil
}

func extractToolCalls(msg *model.Message) []toolCall {
	if msg == nil {
		return nil
	}
	var calls []toolCall
	for _, p := range msg.Parts {
		if tu, ok := p.(content.ToolUse); ok {
			calls = append(calls, toolCall{ID: tu.ID, Name: tu.Name, Input: tu.Input})
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

// toolCall represents a single tool invocation request.
type toolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// toolExecResult is the outcome of a single tool execution.
type toolExecResult struct {
	ID      string
	Name    string
	Parts   []content.Part
	IsError bool
	Control event.Output
}

func (a *llmAgent) executeTools(ctx context.Context, calls []toolCall, allTools []tools.Tool) []toolExecResult {
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

func (a *llmAgent) executeSingleTool(ctx context.Context, call toolCall, toolMap map[string]tools.Tool) toolExecResult {
	tool, ok := toolMap[call.Name]
	if !ok {
		return toolExecResult{
			ID:      call.ID,
			Name:    call.Name,
			Parts:   []content.Part{content.Text{Text: "tool not found: " + call.Name}},
			IsError: true,
		}
	}

	if a.policy != nil {
		decision, err := a.policy.Check(ctx, policy.ToolRequest{Tool: tool, Input: call.Input})
		if err != nil || decision.Action == policy.Deny {
			reason := "denied by policy"
			if err != nil {
				reason = err.Error()
			} else if decision.Reason != "" {
				reason = decision.Reason
			}
			return toolExecResult{
				ID:      call.ID,
				Name:    call.Name,
				Parts:   []content.Part{content.Text{Text: reason}},
				IsError: true,
			}
		}
		if decision.Action == policy.Modify && decision.Modified != nil {
			call.Input = decision.Modified.Input
		}
	}

	tc := &toolCtx{id: call.ID, spec: tool.Spec()}
	res, err := tool.Handle(tools.NewContext(ctx, tc), call.Input)
	if err != nil {
		if le, ok := tools.IsLoopExit(err); ok {
			return toolExecResult{
				ID:      call.ID,
				Name:    call.Name,
				Parts:   []content.Part{content.Text{Text: "loop exit"}},
				Control: event.LoopExit{ToolID: call.ID, ToolName: call.Name, Escalate: le.Escalate},
			}
		}
		if h, ok := tools.IsHandoff(err); ok {
			return toolExecResult{
				ID:      call.ID,
				Name:    call.Name,
				Parts:   []content.Part{content.Text{Text: "handoff to " + h.Agent}},
				Control: event.Handoff{ToolID: call.ID, ToolName: call.Name, Agent: h.Agent},
			}
		}
		return toolExecResult{
			ID:      call.ID,
			Name:    call.Name,
			Parts:   []content.Part{content.Text{Text: "tool error: " + err.Error()}},
			IsError: true,
		}
	}

	var parts []content.Part
	if res != nil {
		parts = res.Parts
	}
	return toolExecResult{
		ID:    call.ID,
		Name:  call.Name,
		Parts: parts,
	}
}

type toolCtx struct {
	id   string
	spec tools.ToolSpec
}

func (t *toolCtx) ID() string         { return t.id }
func (t *toolCtx) Spec() tools.ToolSpec { return t.spec }
