package execute

import (
	"context"
	"sync"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/event"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/policy"
	"github.com/go-kratos/blades/tools"
)

// Result pairs a tool result with an optional control signal.
type Result struct {
	content.ToolResult
	Control event.Output
}

// Wave executes a batch of tool calls concurrently with policy checks.
// It emits ToolStart/ToolEnd events to the output channel.
func Wave(ctx context.Context, calls []content.ToolUse, allTools []tools.Tool, p policy.Policy, output chan<- event.Output) []Result {
	for _, call := range calls {
		output <- event.ToolStart{ID: call.ID, Name: call.Name, Input: call.Input}
	}

	results := run(ctx, calls, allTools, p)

	for _, r := range results {
		output <- event.ToolEnd{ID: r.ID, Name: r.Name, Parts: r.Parts, IsError: r.IsError}
	}

	return results
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

func run(ctx context.Context, calls []content.ToolUse, allTools []tools.Tool, p policy.Policy) []Result {
	results := make([]Result, len(calls))
	toolMap := make(map[string]tools.Tool, len(allTools))
	for _, t := range allTools {
		toolMap[t.Spec().Name] = t
	}
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = executeSingle(ctx, call, toolMap, p)
		}()
	}
	wg.Wait()
	return results
}

func executeSingle(ctx context.Context, call content.ToolUse, toolMap map[string]tools.Tool, p policy.Policy) Result {
	tool, ok := toolMap[call.Name]
	if !ok {
		return Result{
			ToolResult: content.ToolResult{ID: call.ID, Name: call.Name, Parts: []content.Part{content.Text{Text: "tool not found: " + call.Name}}, IsError: true},
		}
	}

	input := call.Input
	if p != nil {
		decision, err := p.Check(ctx, policy.ToolRequest{Tool: tool, Input: input})
		if err != nil || decision.Action == policy.Deny {
			reason := "denied by policy"
			if err != nil {
				reason = err.Error()
			} else if decision.Reason != "" {
				reason = decision.Reason
			}
			return Result{
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
			return Result{
				ToolResult: content.ToolResult{ID: call.ID, Name: call.Name, Parts: []content.Part{content.Text{Text: "loop exit"}}},
				Control:    event.LoopExit{ToolID: call.ID, ToolName: call.Name, Escalate: le.Escalate},
			}
		}
		if h, ok := tools.IsHandoff(err); ok {
			return Result{
				ToolResult: content.ToolResult{ID: call.ID, Name: call.Name, Parts: []content.Part{content.Text{Text: "handoff to " + h.Agent}}},
				Control:    event.Handoff{ToolID: call.ID, ToolName: call.Name, Agent: h.Agent},
			}
		}
		return Result{
			ToolResult: content.ToolResult{ID: call.ID, Name: call.Name, Parts: []content.Part{content.Text{Text: "tool error: " + err.Error()}}, IsError: true},
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

type toolCtx struct {
	id   string
	spec tools.ToolSpec
}

func (t *toolCtx) ID() string           { return t.id }
func (t *toolCtx) Spec() tools.ToolSpec { return t.spec }
