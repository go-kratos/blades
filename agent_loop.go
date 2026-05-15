package blades

import (
	"context"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/event"
	"github.com/go-kratos/blades/hook"
	"github.com/go-kratos/blades/internal/convert"
	"github.com/go-kratos/blades/internal/execute"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/prompt"
	"github.com/go-kratos/blades/session"
	"github.com/go-kratos/blades/tools"
)

type agentLoop struct {
	agent *llmAgent

	ctx      context.Context
	output   chan<- event.Output
	sess     session.Session
	allTools []tools.Tool
	inputs   *loopInput

	turnNum int
}

type loopInput struct {
	ctx     context.Context
	input   <-chan event.Input
	pending []event.Input
	closed  bool
}

type turnInputs struct {
	steering []event.Steer
	aborted  bool
}

type turnResult struct {
	parts      []content.Part
	stopReason event.StopReason
	usage      event.Usage
	action     event.Action
}

type steeringResult struct {
	consumed bool
	aborted  bool
}

type stepResult struct {
	response *model.Response
	usage    event.Usage
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
		in, ok, err := l.inputs.nextFollowUp()
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

func newLoopInput(ctx context.Context, input <-chan event.Input) *loopInput {
	return &loopInput{ctx: ctx, input: input}
}

func (q *loopInput) nextFollowUp() (event.Input, bool, error) {
	for {
		if in, ok := q.popPendingStart(); ok {
			return in, true, nil
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
			if startsTurn(in) {
				return in, true, nil
			}
			if _, ok := in.(event.Abort); ok {
				return nil, false, nil
			}
		}
	}
}

func (q *loopInput) drainDuringTurn() (turnInputs, error) {
	var drained turnInputs
	for {
		if q.closed {
			return drained, nil
		}

		select {
		case <-q.ctx.Done():
			return turnInputs{}, q.ctx.Err()
		case in, ok := <-q.input:
			if !ok {
				q.closed = true
				return drained, nil
			}
			switch v := in.(type) {
			case event.Steer:
				drained.steering = append(drained.steering, v)
			case event.Prompt:
				q.pending = append(q.pending, v)
			case event.Abort:
				drained.aborted = true
				return drained, nil
			}
		default:
			return drained, nil
		}
	}
}

func (q *loopInput) popPendingStart() (event.Input, bool) {
	for len(q.pending) > 0 {
		in := q.pending[0]
		q.pending = q.pending[1:]
		if startsTurn(in) {
			return in, true
		}
		if _, ok := in.(event.Abort); ok {
			return nil, false
		}
	}
	return nil, false
}

func startsTurn(in event.Input) bool {
	switch in.(type) {
	case event.Prompt, event.Steer:
		return true
	default:
		return false
	}
}

func (l *agentLoop) runTurn(in event.Input) error {
	l.turnNum++
	turn := &hook.Turn{AgentName: l.agent.name, Turn: l.turnNum, Input: in}
	result := turnResult{stopReason: event.StopEnd}

	if err := l.beforeTurn(turn); err != nil {
		if hook.IsAbort(err) {
			result.stopReason = event.StopAbort
			l.endTurn(turn, result, err)
			return nil
		}
		l.endTurn(turn, turnResult{stopReason: event.StopAbort}, err)
		return err
	}

	msg, ok := inputToMessage(turn.Input)
	if !ok {
		return nil
	}
	if err := l.sess.Append(l.ctx, msg); err != nil {
		l.endTurn(turn, turnResult{stopReason: event.StopAbort}, err)
		return err
	}

	for {
		step, err := l.runStep()
		if err != nil {
			if hook.IsAbort(err) {
				result.stopReason = event.StopAbort
				l.endTurn(turn, result, err)
				return nil
			}
			l.endTurn(turn, result, err)
			return err
		}

		result.usage.InputTokens += step.usage.InputTokens
		result.usage.OutputTokens += step.usage.OutputTokens
		if step.response != nil && step.response.Message != nil {
			result.parts = step.response.Message.Parts
		}

		toolUses := execute.ExtractToolUses(step.response.Message)
		if len(toolUses) > 0 {
			toolMsg, action, err := l.executeToolWave(toolUses)
			if err != nil {
				if hook.IsAbort(err) {
					result.stopReason = event.StopAbort
					l.endTurn(turn, result, err)
					return nil
				}
				l.endTurn(turn, result, err)
				return err
			}
			if err := l.commitStep(step.response.Message, toolMsg); err != nil {
				l.endTurn(turn, result, err)
				return err
			}
			if action != nil {
				result.stopReason = event.StopToolUse
				result.action = action
				break
			}
			steering, err := l.consumeTurnInputs()
			if err != nil {
				l.endTurn(turn, result, err)
				return err
			}
			if steering.aborted {
				result.stopReason = event.StopAbort
				break
			}
			continue
		}

		if err := l.commitStep(step.response.Message, nil); err != nil {
			l.endTurn(turn, result, err)
			return err
		}
		result.stopReason = outputStopReason(step.response.StopReason)

		steering, err := l.consumeTurnInputs()
		if err != nil {
			l.endTurn(turn, result, err)
			return err
		}
		if steering.aborted {
			result.stopReason = event.StopAbort
			break
		}
		if steering.consumed {
			continue
		}
		break
	}

	l.endTurn(turn, result, nil)
	return nil
}

func (l *agentLoop) beforeTurn(turn *hook.Turn) error {
	for _, h := range l.agent.hooks {
		if err := h.BeforeTurn(l.ctx, turn); err != nil {
			return err
		}
	}
	return nil
}

func (l *agentLoop) endTurn(turn *hook.Turn, result turnResult, err error) {
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

func (l *agentLoop) runStep() (stepResult, error) {
	req, err := l.buildRequest()
	if err != nil {
		return stepResult{}, err
	}

	for _, h := range l.agent.hooks {
		if err := h.BeforeModel(l.ctx, req); err != nil {
			return stepResult{}, err
		}
	}

	resp, usage, err := l.streamStep(req)
	if err != nil {
		for _, h := range l.agent.hooks {
			_ = h.AfterModel(l.ctx, req, nil, err)
		}
		return stepResult{}, err
	}

	for _, h := range l.agent.hooks {
		if err := h.AfterModel(l.ctx, req, resp, nil); err != nil {
			return stepResult{}, err
		}
	}

	return stepResult{response: resp, usage: usage}, nil
}

func (l *agentLoop) buildRequest() (*model.Request, error) {
	msgs, err := l.sess.Messages(l.ctx)
	if err != nil {
		return nil, err
	}
	if l.agent.compactor != nil {
		msgs, err = l.agent.compactor.Compact(l.ctx, msgs)
		if err != nil {
			return nil, err
		}
	}
	var system string
	if l.agent.promptBuilder != nil {
		parts, err := l.agent.promptBuilder.Build(l.ctx)
		if err != nil {
			return nil, err
		}
		system = prompt.SystemText(parts)
	}
	toolSpecs := make([]tools.ToolSpec, 0, len(l.allTools))
	for _, t := range l.allTools {
		toolSpecs = append(toolSpecs, t.Spec())
	}
	return &model.Request{
		Model:    l.agent.provider.Name(),
		System:   system,
		Messages: msgs,
		Tools:    toolSpecs,
	}, nil
}

func (l *agentLoop) streamStep(req *model.Request) (*model.Response, event.Usage, error) {
	var (
		parts      []content.Part
		stopReason model.StopReason
		usage      model.Usage
	)

	for chunk, err := range l.agent.provider.Stream(l.ctx, req) {
		if err != nil {
			return nil, event.Usage{}, err
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
	}, event.Usage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens}, nil
}

func (l *agentLoop) executeToolWave(calls []content.ToolUse) (*model.Message, event.Action, error) {
	executableCalls := make([]content.ToolUse, len(calls))
	copy(executableCalls, calls)
	runtime := execute.NewRuntime(l.allTools, l.agent.policy)

	for i := range executableCalls {
		call := &executableCalls[i]
		tool := runtime.Tool(call.Name)
		hookCall := &hook.ToolCall{ID: call.ID, AgentName: l.agent.name, Turn: l.turnNum, Tool: tool, Input: call.Input}
		for _, h := range l.agent.hooks {
			if err := h.BeforeTool(l.ctx, hookCall); err != nil {
				return nil, nil, err
			}
			call.Input = hookCall.Input
		}
	}

	results, err := l.executeToolWaveParallel(runtime, executableCalls)
	if err != nil {
		return nil, nil, err
	}

	toolResults := make([]content.ToolResult, len(results))
	var action event.Action
	for _, result := range results {
		if action == nil && result.Action != nil {
			action = result.Action
		}
	}
	for i := range results {
		toolResults[i] = results[i].ToolResult
	}

	return convert.ToolResultToMessage(toolResults), action, nil
}

func (l *agentLoop) executeToolWaveParallel(runtime execute.Runtime, calls []content.ToolUse) ([]execute.Result, error) {
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
	for _, h := range l.agent.hooks {
		if err := h.AfterTool(l.ctx, &hook.ToolCall{ID: call.ID, AgentName: l.agent.name, Turn: l.turnNum, Tool: runtime.Tool(call.Name), Input: call.Input}, result, execResult.Err); err != nil {
			return execute.Result{}, err
		}
	}
	execResult.Parts = result.Parts
	return execResult, nil
}

func (l *agentLoop) emitToolEnd(result execute.Result) {
	l.output <- event.ToolEnd{ID: result.ID, Name: result.Name, Parts: result.Parts, IsError: result.IsError}
}

func (l *agentLoop) commitStep(assistantMsg *model.Message, toolMsg *model.Message) error {
	switch {
	case assistantMsg == nil && toolMsg == nil:
		return nil
	case assistantMsg == nil:
		return l.sess.Append(l.ctx, toolMsg)
	case toolMsg == nil:
		return l.sess.Append(l.ctx, assistantMsg)
	default:
		return l.sess.Append(l.ctx, assistantMsg, toolMsg)
	}
}

func (l *agentLoop) consumeTurnInputs() (steeringResult, error) {
	drained, err := l.inputs.drainDuringTurn()
	if err != nil {
		return steeringResult{}, err
	}
	if len(drained.steering) > 0 {
		msgs := make([]*model.Message, 0, len(drained.steering))
		for _, steer := range drained.steering {
			msgs = append(msgs, convert.SteerToMessage(steer))
		}
		if err := l.sess.Append(l.ctx, msgs...); err != nil {
			return steeringResult{}, err
		}
	}
	return steeringResult{
		consumed: len(drained.steering) > 0,
		aborted:  drained.aborted,
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

func outputStopReason(reason model.StopReason) event.StopReason {
	if reason == "" {
		return event.StopEnd
	}
	return event.StopReason(reason)
}
