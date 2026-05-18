package blades

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/event"
	"github.com/go-kratos/blades/hook"
	"github.com/go-kratos/blades/internal/convert"
	"github.com/go-kratos/blades/internal/execute"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/session"
	"github.com/go-kratos/blades/tools"
)

type agentLoop struct {
	agent *llmAgent

	ctx      context.Context
	output   chan<- event.Output
	sess     session.Session
	allTools []tools.Tool
	inputs   *inputQueue

	turnNum int
}

// inputQueue separates current-turn steering from follow-up prompts.
type inputQueue struct {
	ctx       context.Context
	input     <-chan event.Input
	followUps []event.Prompt
	closed    bool
}

type stepBoundaryInputs struct {
	steering []event.Steer
	aborted  bool
}

type stepBoundaryResult struct {
	hasSteering bool
	aborted     bool
}

type toolWaveResult struct {
	index  int
	result execute.Result
}

func (l *agentLoop) run() {
	defer func() {
		l.output <- event.Done{}
		close(l.output)
	}()

	for {
		in, ok, err := l.inputs.nextTurnStart()
		if err != nil {
			l.output <- event.Error{Err: err}
			return
		}
		if !ok {
			return
		}
		if err := l.runTurn(in); err != nil {
			l.output <- event.Error{Err: err}
			return
		}
	}
}

func newInputQueue(ctx context.Context, input <-chan event.Input) *inputQueue {
	return &inputQueue{ctx: ctx, input: input}
}

func (q *inputQueue) nextTurnStart() (event.Input, bool, error) {
	for {
		if prompt, ok := q.popFollowUp(); ok {
			return prompt, true, nil
		}
		if q.closed {
			return nil, false, nil
		}

		select {
		case <-q.ctx.Done():
			return nil, false, q.ctx.Err()
		case in, ok := <-q.input:
			if !ok {
				q.closed = true
				return nil, false, nil
			}
			switch in.(type) {
			case event.Prompt, event.Steer:
				return in, true, nil
			case event.Abort:
				return nil, false, nil
			}
		}
	}
}

func (q *inputQueue) drainStepBoundaryInputs() (stepBoundaryInputs, error) {
	var drained stepBoundaryInputs
	for {
		if q.closed {
			return drained, nil
		}

		select {
		case <-q.ctx.Done():
			return stepBoundaryInputs{}, q.ctx.Err()
		case in, ok := <-q.input:
			if !ok {
				q.closed = true
				return drained, nil
			}
			switch v := in.(type) {
			case event.Steer:
				drained.steering = append(drained.steering, v)
			case event.Prompt:
				q.followUps = append(q.followUps, v)
			case event.Abort:
				drained.aborted = true
				return drained, nil
			}
		default:
			return drained, nil
		}
	}
}

func (q *inputQueue) popFollowUp() (event.Prompt, bool) {
	if len(q.followUps) == 0 {
		return event.Prompt{}, false
	}
	in := q.followUps[0]
	q.followUps = q.followUps[1:]
	return in, true
}

func (l *agentLoop) runTurn(in event.Input) error {
	l.turnNum++
	turn := &hook.Turn{AgentName: l.agent.name, Turn: l.turnNum, Input: in}
	state := newTurnState()

	if err := l.beforeTurn(turn); err != nil {
		state.abort()
		l.endTurn(turn, state, err)
		return abortAsNil(err)
	}

	msg, ok := inputToMessage(turn.Input)
	if !ok {
		err := fmt.Errorf("agent loop: unsupported turn input %T", turn.Input)
		state.abort()
		l.endTurn(turn, state, err)
		return err
	}
	if err := l.sess.Append(l.ctx, msg); err != nil {
		state.abort()
		l.endTurn(turn, state, err)
		return err
	}

	for {
		cont, err := l.runTurnStep(&state)
		if err != nil {
			if hook.IsAbort(err) {
				state.abort()
			}
			l.endTurn(turn, state, err)
			return abortAsNil(err)
		}
		if cont {
			continue
		}
		break
	}

	l.endTurn(turn, state, nil)
	return nil
}

func abortAsNil(err error) error {
	if hook.IsAbort(err) {
		return nil
	}
	return err
}

func (l *agentLoop) runTurnStep(state *turnState) (bool, error) {
	resp, err := l.runStep()
	if err != nil {
		return false, err
	}
	state.recordResponse(resp)

	toolUses := execute.ExtractToolUses(resp.Message)
	if len(toolUses) > 0 {
		return l.handleToolStep(state, resp.Message, toolUses)
	}

	if err := l.commitStep(resp.Message); err != nil {
		return false, err
	}
	state.finish(resp.StopReason)
	return l.continueAfterModelStep(state)
}

func (l *agentLoop) handleToolStep(state *turnState, assistantMsg *model.Message, toolUses []content.ToolUse) (bool, error) {
	toolMsg, action, err := l.executeToolWave(toolUses)
	if err != nil {
		return false, err
	}
	if err := l.commitStep(assistantMsg, toolMsg); err != nil {
		return false, err
	}
	if action != nil {
		state.stopForAction(action)
		return false, nil
	}
	inputs, err := l.consumeStepBoundaryInputs(state)
	if err != nil {
		return false, err
	}
	return !inputs.aborted, nil
}

func (l *agentLoop) continueAfterModelStep(state *turnState) (bool, error) {
	inputs, err := l.consumeStepBoundaryInputs(state)
	if err != nil {
		return false, err
	}
	if inputs.aborted {
		return false, nil
	}
	return inputs.hasSteering, nil
}

func (l *agentLoop) beforeTurn(turn *hook.Turn) error {
	for _, h := range l.agent.hooks {
		if err := h.BeforeTurn(l.ctx, turn); err != nil {
			return err
		}
	}
	return nil
}

func (l *agentLoop) endTurn(turn *hook.Turn, result turnState, err error) {
	l.output <- event.TurnEnd{
		Parts:      result.parts,
		StopReason: result.stopReason,
		Usage:      result.usage,
		Err:        err,
		Action:     result.action,
	}

	summary := &hook.TurnSummary{
		Parts:      result.parts,
		StopReason: model.StopReason(result.stopReason),
		Usage:      &model.Usage{InputTokens: result.usage.InputTokens, OutputTokens: result.usage.OutputTokens},
	}
	for _, h := range l.agent.hooks {
		_ = h.AfterTurn(l.ctx, turn, summary, err)
	}
}

func (l *agentLoop) runStep() (*model.Response, error) {
	req, err := l.buildRequest(l.ctx)
	if err != nil {
		return nil, err
	}

	for _, h := range l.agent.hooks {
		if err := h.BeforeModel(l.ctx, req); err != nil {
			return nil, err
		}
	}

	// Re-check budget after hooks may have mutated the request
	if err := l.enforceBudget(l.ctx, req); err != nil {
		return nil, err
	}

	resp, err := l.streamStep(l.ctx, req)
	if err != nil {
		for _, h := range l.agent.hooks {
			_ = h.AfterModel(l.ctx, req, nil, err)
		}
		return nil, err
	}

	for _, h := range l.agent.hooks {
		if err := h.AfterModel(l.ctx, req, resp, nil); err != nil {
			return nil, err
		}
	}

	return resp, nil
}

func (l *agentLoop) buildRequest(ctx context.Context) (*model.Request, error) {
	return contextBuilder{
		agent:    l.agent,
		sess:     l.sess,
		allTools: l.allTools,
	}.Build(ctx)
}

func (l *agentLoop) enforceBudget(ctx context.Context, req *model.Request) error {
	usage, err := l.agent.tokenCounter.CountTokens(ctx, req)
	if err != nil {
		return err
	}
	return checkBudget(l.agent.contextBudget, normalizeUsage(usage))
}

func (l *agentLoop) streamStep(ctx context.Context, req *model.Request) (*model.Response, error) {
	var (
		parts      []content.Part
		stopReason model.StopReason
		usage      model.Usage
	)

	for chunk, err := range l.agent.provider.Stream(ctx, req) {
		if err != nil {
			return nil, err
		}
		if chunk == nil {
			continue
		}
		for _, o := range convert.ChunkToOutputs(chunk) {
			l.output <- o
		}
		parts = append(parts, chunk.Parts...)
		if chunk.StopReason != "" {
			stopReason = chunk.StopReason
		}
		if chunk.Usage != nil {
			usage.InputTokens += chunk.Usage.InputTokens
			usage.OutputTokens += chunk.Usage.OutputTokens
		}
	}

	return &model.Response{
		Message:    &model.Message{Role: model.RoleAssistant, Parts: parts},
		StopReason: stopReason,
		Usage:      usage,
	}, nil
}

func (l *agentLoop) executeToolWave(calls []content.ToolUse) (*model.Message, event.Action, error) {
	runtime := execute.NewRuntime(l.allTools, l.agent.policy)
	executableCalls, err := l.prepareToolCalls(runtime, calls)
	if err != nil {
		return nil, nil, err
	}
	results, err := l.runToolCalls(runtime, executableCalls)
	if err != nil {
		return nil, nil, err
	}
	toolResults, action := collectToolResults(results)
	return convert.ToolResultToMessage(toolResults), action, nil
}

func (l *agentLoop) prepareToolCalls(runtime execute.Runtime, calls []content.ToolUse) ([]content.ToolUse, error) {
	executableCalls := make([]content.ToolUse, len(calls))
	copy(executableCalls, calls)
	for i := range executableCalls {
		call := &executableCalls[i]
		hookCall := &hook.ToolCall{
			ID:        call.ID,
			AgentName: l.agent.name,
			Turn:      l.turnNum,
			Tool:      runtime.Tool(call.Name),
			Input:     call.Input,
		}
		for _, h := range l.agent.hooks {
			if err := h.BeforeTool(l.ctx, hookCall); err != nil {
				return nil, err
			}
			call.Input = hookCall.Input
		}
	}
	return executableCalls, nil
}

func collectToolResults(results []execute.Result) ([]content.ToolResult, event.Action) {
	toolResults := make([]content.ToolResult, len(results))
	var action event.Action
	for i := range results {
		toolResults[i] = results[i].ToolResult
		if action == nil && results[i].Action != nil {
			action = results[i].Action
		}
	}
	return toolResults, action
}

func (l *agentLoop) runToolCalls(runtime execute.Runtime, calls []content.ToolUse) ([]execute.Result, error) {
	results := make([]execute.Result, len(calls))
	done := make(chan toolWaveResult, len(calls))

	for i, call := range calls {
		l.output <- event.ToolStart{ID: call.ID, Name: call.Name, Input: call.Input}
		go func(i int, call content.ToolUse) {
			done <- toolWaveResult{index: i, result: runtime.Call(l.ctx, call)}
		}(i, call)
	}

	var firstErr error
	for range calls {
		completed := <-done
		result, err := l.finalizeToolResult(runtime, calls[completed.index], completed.result)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		results[completed.index] = result
		l.emitToolEnd(result)
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}

func (l *agentLoop) finalizeToolResult(runtime execute.Runtime, call content.ToolUse, execResult execute.Result) (execute.Result, error) {
	result := &tools.Result{Parts: execResult.Parts}
	hookCall := &hook.ToolCall{
		ID:        call.ID,
		AgentName: l.agent.name,
		Turn:      l.turnNum,
		Tool:      runtime.Tool(call.Name),
		Input:     call.Input,
	}
	for _, h := range l.agent.hooks {
		if err := h.AfterTool(l.ctx, hookCall, result, execResult.Err); err != nil {
			return execute.Result{}, err
		}
	}
	execResult.Parts = result.Parts
	return execResult, nil
}

func (l *agentLoop) emitToolEnd(result execute.Result) {
	l.output <- event.ToolEnd{ID: result.ID, Name: result.Name, Parts: result.Parts, IsError: result.IsError}
}

func (l *agentLoop) commitStep(msgs ...*model.Message) error {
	filtered := make([]*model.Message, 0, len(msgs))
	for _, msg := range msgs {
		if msg != nil {
			filtered = append(filtered, msg)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return l.sess.Append(l.ctx, filtered...)
}

func (l *agentLoop) consumeStepBoundaryInputs(state *turnState) (stepBoundaryResult, error) {
	drained, err := l.inputs.drainStepBoundaryInputs()
	if err != nil {
		return stepBoundaryResult{}, err
	}
	if len(drained.steering) > 0 {
		msgs := make([]*model.Message, 0, len(drained.steering))
		for _, steer := range drained.steering {
			msgs = append(msgs, convert.SteerToMessage(steer))
		}
		if err := l.sess.Append(l.ctx, msgs...); err != nil {
			return stepBoundaryResult{}, err
		}
	}
	if drained.aborted {
		state.abort()
	}
	return stepBoundaryResult{
		hasSteering: len(drained.steering) > 0,
		aborted:     drained.aborted,
	}, nil
}

func inputToMessage(in event.Input) (*model.Message, bool) {
	switch v := in.(type) {
	case event.Prompt:
		return convert.PromptToMessage(v), true
	case event.Steer:
		return convert.SteerToMessage(v), true
	default:
		return nil, false
	}
}
