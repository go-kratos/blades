package blades

import (
	"context"

	"github.com/go-kratos/blades/compact"
	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/event"
	"github.com/go-kratos/blades/hook"
	"github.com/go-kratos/blades/internal/convert"
	"github.com/go-kratos/blades/internal/execute"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/policy"
	"github.com/go-kratos/blades/prompt"
	"github.com/go-kratos/blades/session"
	"github.com/go-kratos/blades/tools"
)

const outputBufferSize = 64

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

func (a *llmAgent) Name() string        { return a.name }
func (a *llmAgent) Description() string { return a.description }

// Run implements the Agent interface.
func (a *llmAgent) Run(ctx context.Context, input <-chan event.Input) (<-chan event.Output, error) {
	allTools, err := a.resolveTools(ctx)
	if err != nil {
		return nil, err
	}
	sess := session.Ensure(ctx)
	output := make(chan event.Output, outputBufferSize)
	go a.loop(ctx, input, output, sess, allTools)
	return output, nil
}

func (a *llmAgent) loop(ctx context.Context, input <-chan event.Input, output chan<- event.Output, sess session.Session, allTools []tools.Tool) {
	defer func() {
		output <- event.Done{}
		close(output)
	}()

	var turnNum int

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
			turnAction event.Action
		)

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
				if err := sess.Append(ctx, resp.Message); err != nil {
					output <- event.Error{Err: err}
					return
				}
				finalParts = resp.Message.Parts
			}

			hasMore = false
			if toolUses := execute.ExtractToolUses(resp.Message); len(toolUses) > 0 {
				action, err := a.executeToolWave(ctx, toolUses, allTools, output, sess, turnNum)
				if err != nil {
					output <- event.Error{Err: err}
					return
				}
				if action != nil {
					stopReason = event.StopToolUse
					turnAction = action
					break
				}
				hasMore = true
			} else {
				stopReason = event.StopReason(resp.StopReason)
			}

			consumed, aborted := a.consumeSteering(ctx, input, sess)
			if aborted {
				stopReason = event.StopAbort
				hasMore = false
				break
			}
			if consumed {
				hasMore = true
			}
		}

		output <- event.TurnEnd{
			Parts:      finalParts,
			StopReason: stopReason,
			Usage:      totalUsage,
			Action:     turnAction,
		}

		summary := &hook.TurnSummary{
			Parts:      finalParts,
			StopReason: model.StopReason(stopReason),
			Usage:      &model.Usage{InputTokens: totalUsage.InputTokens, OutputTokens: totalUsage.OutputTokens},
		}
		for _, h := range a.hooks {
			_ = h.AfterTurn(ctx, &hook.Turn{AgentName: a.name, Turn: turnNum}, summary, nil)
		}

		if !a.awaitFollowUp(ctx, input, sess, output) {
			return
		}
	}
}

func (a *llmAgent) executeToolWave(ctx context.Context, calls []content.ToolUse, allTools []tools.Tool, output chan<- event.Output, sess session.Session, turnNum int) (event.Action, error) {
	for _, call := range calls {
		for _, h := range a.hooks {
			if err := h.BeforeTool(ctx, &hook.ToolCall{AgentName: a.name, Turn: turnNum, Input: call.Input}); err != nil {
				return nil, err
			}
		}
	}

	results, action := execute.Wave(ctx, calls, allTools, a.policy, output)

	if err := sess.Append(ctx, convert.ToolResultToMessage(results)); err != nil {
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

	return action, nil
}

// consumeSteering non-blocking drains Steer events from input into session.
func (a *llmAgent) consumeSteering(ctx context.Context, input <-chan event.Input, sess session.Session) (consumed bool, aborted bool) {
	for {
		select {
		case in, ok := <-input:
			if !ok {
				return consumed, true
			}
			switch v := in.(type) {
			case event.Steer:
				_ = sess.Append(ctx, convert.SteerToMessage(v))
				consumed = true
			case event.Abort:
				return consumed, true
			}
		default:
			return consumed, false
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

func (a *llmAgent) streamStep(ctx context.Context, req *model.Request, output chan<- event.Output) (*model.Response, event.Usage, error) {
	var (
		parts      []content.Part
		stopReason model.StopReason
		usage      model.Usage
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
	msgs, err := sess.Messages(ctx)
	if err != nil {
		return nil, err
	}
	if a.compactor != nil {
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
		system = prompt.SystemText(parts)
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
