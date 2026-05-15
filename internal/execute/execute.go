package execute

import (
	"context"
	"errors"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/event"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/policy"
	"github.com/go-kratos/blades/tools"
)

// Result is the normalized outcome of one tool invocation.
type Result struct {
	content.ToolResult
	Err    error
	Action event.Action
}

// Runtime normalizes tool invocation details without owning Agent Loop events.
type Runtime struct {
	tools  map[string]tools.Tool
	policy policy.Policy
}

// NewRuntime creates a tool execution runtime for a resolved tool set.
func NewRuntime(allTools []tools.Tool, p policy.Policy) Runtime {
	return Runtime{tools: toolsByName(allTools), policy: p}
}

// Tool returns a resolved tool by name.
func (r Runtime) Tool(name string) tools.Tool {
	return r.tools[name]
}

// Call executes one tool call with policy checks and normalized errors.
func (r Runtime) Call(ctx context.Context, call content.ToolUse) Result {
	return executeSingle(ctx, call, r.tools, r.policy)
}

// ExtractToolUses extracts ToolUse parts from an assistant message.
func ExtractToolUses(msg *model.Message) []content.ToolUse {
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

func toolsByName(allTools []tools.Tool) map[string]tools.Tool {
	toolMap := make(map[string]tools.Tool, len(allTools))
	for _, t := range allTools {
		toolMap[t.Spec().Name] = t
	}
	return toolMap
}

func executeSingle(ctx context.Context, call content.ToolUse, toolMap map[string]tools.Tool, p policy.Policy) Result {
	tool, ok := toolMap[call.Name]
	if !ok {
		err := errors.New("tool not found: " + call.Name)
		return Result{
			ToolResult: content.ToolResult{ID: call.ID, Name: call.Name, Parts: []content.Part{content.Text{Text: "tool not found: " + call.Name}}, IsError: true},
			Err:        err,
		}
	}

	input := call.Input
	if p != nil {
		decision, err := p.Check(ctx, policy.ToolRequest{Tool: tool, Input: input})
		if err != nil {
			return toolErrorResult(call, err.Error(), err)
		}
		switch decision.Action {
		case policy.Allow:
		case policy.Deny:
			reason := decision.Reason
			if reason == "" {
				reason = "denied by policy"
			}
			return toolErrorResult(call, reason, errors.New(reason))
		case policy.Ask:
			reason := decision.Reason
			if reason == "" {
				reason = "requires approval by policy"
			}
			return toolErrorResult(call, reason, errors.New(reason))
		case policy.Modify:
			if decision.Modified == nil {
				return toolErrorResult(call, "policy modify missing request", errors.New("policy modify missing request"))
			}
			if decision.Modified.Tool == nil || decision.Modified.Tool.Spec().Name != tool.Spec().Name {
				return toolErrorResult(call, "policy modify changed tool", errors.New("policy modify changed tool"))
			}
			input = decision.Modified.Input
		default:
			return toolErrorResult(call, "unknown policy action: "+string(decision.Action), errors.New("unknown policy action: "+string(decision.Action)))
		}
	}

	tc := &toolCtx{id: call.ID, spec: tool.Spec()}
	res, err := tool.Handle(tools.NewContext(ctx, tc), input)
	if err != nil {
		if le, ok := tools.IsLoopExit(err); ok {
			return Result{
				ToolResult: content.ToolResult{ID: call.ID, Name: call.Name, Parts: []content.Part{content.Text{Text: "loop exit"}}},
				Action:     event.LoopExit{Escalate: le.Escalate},
			}
		}
		if h, ok := tools.IsHandoff(err); ok {
			return Result{
				ToolResult: content.ToolResult{ID: call.ID, Name: call.Name, Parts: []content.Part{content.Text{Text: "handoff to " + h.Agent}}},
				Action:     event.Handoff{Agent: h.Agent},
			}
		}
		return Result{
			ToolResult: content.ToolResult{ID: call.ID, Name: call.Name, Parts: []content.Part{content.Text{Text: "tool error: " + err.Error()}}, IsError: true},
			Err:        err,
		}
	}

	var parts []content.Part
	if res != nil {
		parts = res.Parts
	}
	return Result{
		ToolResult: content.ToolResult{ID: call.ID, Name: call.Name, Parts: parts},
	}
}

func toolErrorResult(call content.ToolUse, text string, err error) Result {
	return Result{
		ToolResult: content.ToolResult{ID: call.ID, Name: call.Name, Parts: []content.Part{content.Text{Text: text}}, IsError: true},
		Err:        err,
	}
}

type toolCtx struct {
	id   string
	spec tools.ToolSpec
}

func (t *toolCtx) ID() string           { return t.id }
func (t *toolCtx) Spec() tools.ToolSpec { return t.spec }
