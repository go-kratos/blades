package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"sync"
	"testing"
	"time"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/compact"
	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/event"
	"github.com/go-kratos/blades/hook"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/policy"
	"github.com/go-kratos/blades/prompt"
	"github.com/go-kratos/blades/session"
	"github.com/go-kratos/blades/tests/dummyprovider"
	"github.com/go-kratos/blades/tests/testtools"
	"github.com/go-kratos/blades/tools"
	"github.com/stretchr/testify/assert"
)

func TestLLMAgentExecutesCalculateTool(t *testing.T) {
	provider := dummyprovider.NewProvider(
		dummyprovider.AssistantResponse(
			[]content.Part{
				dummyprovider.Text("Let me calculate that."),
				dummyprovider.ToolUse("calc-1", "calculate", json.RawMessage(`{"expression":"123 * 456"}`)),
			},
			dummyprovider.WithStopReason(model.StopToolUse),
		),
		dummyprovider.TextResponse("The result is 56088."),
	)
	agent, err := blades.NewAgent(
		"calculator",
		blades.WithModel(provider),
		blades.WithTools(testtools.NewCalculateTool()),
	)
	assert.NoError(t, err)

	sess := newRecordingSession()
	ctx, cancel := context.WithCancel(session.NewContext(context.Background(), sess))
	defer cancel()
	inputs := make(chan event.Input, 1)
	inputs <- event.NewPromptText("What is 123 * 456?")

	outputs, err := collectAgentOutputs(ctx, agent, inputs)
	cancel()
	assert.NoError(t, err)

	toolEnd, ok := findToolEnd(outputs, "calc-1")
	assert.True(t, ok)
	assert.Equal(t, "calculate", toolEnd.Name)
	assert.False(t, toolEnd.IsError)
	assert.Equal(t, "123 * 456 = 56088", textFromParts(toolEnd.Parts))
	assert.Equal(t, 1, countToolStarts(outputs, "calc-1"))

	turnEnd, ok := lastTurnEnd(outputs)
	assert.True(t, ok)
	assert.Equal(t, event.StopEnd, turnEnd.StopReason)
	assert.Equal(t, "The result is 56088.", textFromParts(turnEnd.Parts))
	assert.Equal(t, 2, provider.CallCount())

	messages, err := sess.Messages(ctx)
	assert.NoError(t, err)
	if assert.Len(t, messages, 4) {
		assert.Equal(t, model.RoleUser, messages[0].Role)
		assert.Equal(t, model.RoleAssistant, messages[1].Role)
		assert.Equal(t, model.RoleTool, messages[2].Role)
		assert.Equal(t, model.RoleAssistant, messages[3].Role)
		assert.Equal(t, "123 * 456 = 56088", toolResultText(messages[2].Parts))
		assert.Equal(t, "The result is 56088.", textFromParts(messages[3].Parts))
	}

	calls := sess.AppendCalls()
	if assert.Len(t, calls, 3) {
		assert.Len(t, calls[0], 1)
		assert.Len(t, calls[1], 2)
		assert.Len(t, calls[2], 1)
		assert.Equal(t, model.RoleAssistant, calls[1][0].Role)
		assert.Equal(t, model.RoleTool, calls[1][1].Role)
	}
}

func TestLLMAgentPromptBuilderCanReadLoopSessionFromContext(t *testing.T) {
	provider := dummyprovider.NewProvider(dummyprovider.TextResponse("ok"))
	capture := &requestCaptureHook{}
	agent, err := blades.NewAgent(
		"assistant",
		blades.WithModel(provider),
		blades.WithHooks(capture),
		blades.WithPrompt(prompt.Section(func(ctx context.Context) ([]content.Part, error) {
			sess, ok := session.FromContext(ctx)
			if !ok {
				return nil, fmt.Errorf("session missing from context")
			}
			msgs, err := sess.Messages(ctx)
			if err != nil {
				return nil, err
			}
			return []content.Part{content.Text{Text: fmt.Sprintf("messages:%d", len(msgs))}}, nil
		})),
	)
	assert.NoError(t, err)

	inputs := make(chan event.Input, 1)
	inputs <- event.NewPromptText("hello")
	close(inputs)

	_, err = collectAllAgentOutputs(context.Background(), agent, inputs)
	assert.NoError(t, err)
	assert.Equal(t, "messages:1", capture.System())
}

func TestLLMAgentWithCompactUsesModelSummarizer(t *testing.T) {
	provider := newCaptureProvider(
		dummyprovider.TextResponse("summary text"),
		dummyprovider.TextResponse("final"),
	)
	promptCalls := 0
	agent, err := blades.NewAgent(
		"assistant",
		blades.WithModel(provider),
		blades.WithTools(testtools.NewCalculateTool()),
		blades.WithPrompt(prompt.Section(func(context.Context) ([]content.Part, error) {
			promptCalls++
			return []content.Part{content.Text{Text: "main prompt"}}, nil
		})),
		blades.WithCompact(compact.NewSummarize(
			compact.WithKeepRecentMessages(1),
			compact.WithSummarizer(compact.NewModelSummarizer(provider)),
		)),
	)
	assert.NoError(t, err)

	sess := session.NewSession(session.WithMessages(
		&model.Message{Role: model.RoleUser, Parts: []content.Part{content.Text{Text: "old user"}}},
		&model.Message{Role: model.RoleAssistant, Parts: []content.Part{content.Text{Text: "old assistant"}}},
	))
	ctx := session.NewContext(context.Background(), sess)
	inputs := make(chan event.Input, 1)
	inputs <- event.NewPromptText("new")
	close(inputs)

	outputs, err := collectAllAgentOutputs(ctx, agent, inputs)
	assert.NoError(t, err)
	turnEnd, ok := lastTurnEnd(outputs)
	assert.True(t, ok)
	assert.Equal(t, "final", textFromParts(turnEnd.Parts))
	assert.Equal(t, 1, promptCalls)

	requests := provider.Requests()
	if assert.Len(t, requests, 2) {
		assert.Contains(t, requests[0].System, "Your task is to create a detailed summary")
		assert.Empty(t, requests[0].Tools)
		assert.Contains(t, textFromRequest(requests[0]), "old user")
		assert.Equal(t, "main prompt", requests[1].System)
		assert.Len(t, requests[1].Tools, 1)
		assert.Len(t, requests[1].Messages, 2)
		assert.Contains(t, textFromParts(requests[1].Messages[0].Parts), "summary text")
		assert.Equal(t, "new", textFromParts(requests[1].Messages[1].Parts))
	}

	messages, err := sess.Messages(ctx)
	assert.NoError(t, err)
	if assert.Len(t, messages, 4) {
		assert.Equal(t, "old user", textFromParts(messages[0].Parts))
		assert.Equal(t, "old assistant", textFromParts(messages[1].Parts))
		assert.Equal(t, "new", textFromParts(messages[2].Parts))
		assert.Equal(t, "final", textFromParts(messages[3].Parts))
	}
}

func TestModelSummarizerCanUseSeparateProvider(t *testing.T) {
	mainProvider := newCaptureProvider(dummyprovider.TextResponse("final"))
	summaryProvider := newCaptureProvider(dummyprovider.TextResponse("override summary"))
	agent, err := blades.NewAgent(
		"assistant",
		blades.WithModel(mainProvider),
		blades.WithCompact(compact.NewSummarize(
			compact.WithKeepRecentMessages(1),
			compact.WithSummarizer(compact.NewModelSummarizer(summaryProvider)),
		)),
	)
	assert.NoError(t, err)

	sess := session.NewSession(session.WithMessages(
		&model.Message{Role: model.RoleUser, Parts: []content.Part{content.Text{Text: "old"}}},
	))
	ctx := session.NewContext(context.Background(), sess)
	inputs := make(chan event.Input, 1)
	inputs <- event.NewPromptText("new")
	close(inputs)

	outputs, err := collectAllAgentOutputs(ctx, agent, inputs)
	assert.NoError(t, err)
	turnEnd, ok := lastTurnEnd(outputs)
	assert.True(t, ok)
	assert.Equal(t, "final", textFromParts(turnEnd.Parts))

	assert.Len(t, summaryProvider.Requests(), 1)
	requests := mainProvider.Requests()
	if assert.Len(t, requests, 1) {
		assert.Contains(t, textFromParts(requests[0].Messages[0].Parts), "override summary")
	}
}

func TestLLMAgentInjectsRunningAgentIntoRuntimeExtensions(t *testing.T) {
	provider := dummyprovider.NewProvider(
		dummyprovider.ToolUseResponse("call_1", "capture", json.RawMessage(`"input"`)),
		dummyprovider.TextResponse("ok"),
	)
	promptCapture := &runningAgentCapture{}
	hookCapture := &runningAgentCapture{}
	toolCapture := &runningAgentCapture{}
	captureTool := &runningAgentCaptureTool{capture: toolCapture}
	var (
		forkName string
		forkDesc string
		forkErr  error
	)
	agent, err := blades.NewAgent(
		"assistant",
		blades.WithDescription("main agent"),
		blades.WithModel(provider),
		blades.WithTools(captureTool),
		blades.WithHooks(&runningAgentCaptureHook{capture: hookCapture}),
		blades.WithPrompt(prompt.Section(func(ctx context.Context) ([]content.Part, error) {
			promptCapture.Capture(ctx)
			running, ok := blades.FromContext(ctx)
			if ok && forkName == "" {
				var fork blades.Agent
				fork, forkErr = blades.Fork(running.Root(), blades.WithDescription("forked"))
				if forkErr == nil {
					forkName = fork.Name()
					forkDesc = fork.Description()
				}
			}
			return nil, nil
		})),
	)
	assert.NoError(t, err)

	inputs := make(chan event.Input, 1)
	inputs <- event.NewPromptText("hello")
	close(inputs)

	outputs, err := collectAllAgentOutputs(context.Background(), agent, inputs)
	assert.NoError(t, err)
	turnEnd, ok := lastTurnEnd(outputs)
	assert.True(t, ok)
	assert.NoError(t, turnEnd.Err)

	assertRunningAgentSnapshot(t, promptCapture.Snapshot(), "assistant", "main agent", false, "", "assistant")
	assertRunningAgentSnapshot(t, hookCapture.Snapshot(), "assistant", "main agent", false, "", "assistant")
	assertRunningAgentSnapshot(t, toolCapture.Snapshot(), "assistant", "main agent", false, "", "assistant")
	assert.NoError(t, forkErr)
	assert.Equal(t, "assistant-fork", forkName)
	assert.Equal(t, "forked", forkDesc)
}

func TestAgentToolSetsParentRunningAgentForSubAgent(t *testing.T) {
	mainProvider := dummyprovider.NewProvider(
		dummyprovider.ToolUseResponse("call_1", "delegate", json.RawMessage(`"work"`)),
		dummyprovider.TextResponse("main final"),
	)
	subProvider := dummyprovider.NewProvider(dummyprovider.TextResponse("sub final"))
	subCapture := &runningAgentCapture{}
	sub, err := blades.NewAgent(
		"delegate",
		blades.WithDescription("sub agent"),
		blades.WithModel(subProvider),
		blades.WithPrompt(prompt.Section(func(ctx context.Context) ([]content.Part, error) {
			subCapture.Capture(ctx)
			return nil, nil
		})),
	)
	assert.NoError(t, err)

	agent, err := blades.NewAgent(
		"main",
		blades.WithDescription("main agent"),
		blades.WithModel(mainProvider),
		blades.WithTools(blades.NewAgentTool(sub)),
	)
	assert.NoError(t, err)

	inputs := make(chan event.Input, 1)
	inputs <- event.NewPromptText("hello")
	close(inputs)

	outputs, err := collectAllAgentOutputs(context.Background(), agent, inputs)
	assert.NoError(t, err)
	turnEnd, ok := lastTurnEnd(outputs)
	assert.True(t, ok)
	assert.NoError(t, turnEnd.Err)
	assert.Equal(t, "main final", textFromParts(turnEnd.Parts))
	assertRunningAgentSnapshot(t, subCapture.Snapshot(), "delegate", "sub agent", true, "main", "main")
}

func TestRunningAgentFromContextMissing(t *testing.T) {
	ac, ok := blades.FromContext(context.Background())
	assert.False(t, ok)
	assert.Nil(t, ac)
}

func TestLLMAgentInstructionsAndPromptsMergeInOptionOrder(t *testing.T) {
	provider := dummyprovider.NewProvider(dummyprovider.TextResponse("ok"))
	capture := &requestCaptureHook{}
	agent, err := blades.NewAgent(
		"assistant",
		blades.WithModel(provider),
		blades.WithHooks(capture),
		blades.WithInstruction("first"),
		blades.WithPrompt(prompt.Section(func(context.Context) ([]content.Part, error) {
			return []content.Part{content.Text{Text: "second"}}, nil
		})),
		blades.WithInstruction("third"),
	)
	assert.NoError(t, err)

	inputs := make(chan event.Input, 1)
	inputs <- event.NewPromptText("hello")
	close(inputs)

	outputs, err := collectAllAgentOutputs(context.Background(), agent, inputs)
	assert.NoError(t, err)
	assert.Equal(t, "first\n\nsecond\n\nthird", capture.System())
	assert.Equal(t, 1, provider.CallCount())

	turnEnd, ok := lastTurnEnd(outputs)
	assert.True(t, ok)
	assert.NoError(t, turnEnd.Err)
}

func TestLLMAgentPromptBuilderErrorEndsTurn(t *testing.T) {
	want := errors.New("prompt failed")
	provider := dummyprovider.NewProvider(dummyprovider.TextResponse("unreached"))
	agent, err := blades.NewAgent(
		"assistant",
		blades.WithModel(provider),
		blades.WithPrompt(prompt.Section(func(context.Context) ([]content.Part, error) {
			return nil, want
		})),
	)
	assert.NoError(t, err)

	inputs := make(chan event.Input, 1)
	inputs <- event.NewPromptText("hello")
	close(inputs)

	outputs, err := collectAllAgentOutputs(context.Background(), agent, inputs)
	assert.NoError(t, err)
	assert.Equal(t, 0, provider.CallCount())

	turnEnd, ok := lastTurnEnd(outputs)
	assert.True(t, ok)
	assert.ErrorIs(t, turnEnd.Err, want)

	var foundError bool
	for _, output := range outputs {
		if errOutput, ok := output.(event.Error); ok {
			foundError = true
			assert.ErrorIs(t, errOutput.Err, want)
		}
	}
	assert.True(t, foundError)
}

func TestLLMAgentBeforeToolCanRewriteInput(t *testing.T) {
	provider := dummyprovider.NewProvider(
		dummyprovider.AssistantResponse(
			[]content.Part{
				dummyprovider.ToolUse("calc-1", "calculate", json.RawMessage(`{"expression":"1 + 1"}`)),
			},
			dummyprovider.WithStopReason(model.StopToolUse),
		),
		dummyprovider.TextResponse("done"),
	)
	rewrite := &rewriteToolInputHook{}
	agent, err := blades.NewAgent(
		"calculator",
		blades.WithModel(provider),
		blades.WithTools(testtools.NewCalculateTool()),
		blades.WithHooks(rewrite),
	)
	assert.NoError(t, err)

	inputs := make(chan event.Input, 1)
	inputs <- event.NewPromptText("calculate")
	close(inputs)

	outputs, err := collectAllAgentOutputs(context.Background(), agent, inputs)
	assert.NoError(t, err)

	toolEnd, ok := findToolEnd(outputs, "calc-1")
	assert.True(t, ok)
	assert.Equal(t, "2 + 3 = 5", textFromParts(toolEnd.Parts))
	assert.Equal(t, "calculate", rewrite.toolName)
}

func TestLLMAgentAfterToolCanRewriteResult(t *testing.T) {
	provider := dummyprovider.NewProvider(
		dummyprovider.AssistantResponse(
			[]content.Part{
				dummyprovider.ToolUse("calc-1", "calculate", json.RawMessage(`{"expression":"1 + 1"}`)),
			},
			dummyprovider.WithStopReason(model.StopToolUse),
		),
		dummyprovider.TextResponse("done"),
	)
	rewrite := &rewriteToolResultHook{}
	agent, err := blades.NewAgent(
		"calculator",
		blades.WithModel(provider),
		blades.WithTools(testtools.NewCalculateTool()),
		blades.WithHooks(rewrite),
	)
	assert.NoError(t, err)

	sess := session.NewSession()
	ctx := session.NewContext(context.Background(), sess)
	inputs := make(chan event.Input, 1)
	inputs <- event.NewPromptText("calculate")
	close(inputs)

	outputs, err := collectAllAgentOutputs(ctx, agent, inputs)
	assert.NoError(t, err)
	assert.NoError(t, rewrite.err)

	toolEnd, ok := findToolEnd(outputs, "calc-1")
	assert.True(t, ok)
	assert.Equal(t, "redacted", textFromParts(toolEnd.Parts))

	messages, err := sess.Messages(ctx)
	assert.NoError(t, err)
	if assert.Len(t, messages, 4) {
		assert.Equal(t, "redacted", toolResultText(messages[2].Parts))
	}
}

func TestLLMAgentPolicyAskDoesNotExecuteTool(t *testing.T) {
	provider := dummyprovider.NewProvider(
		dummyprovider.AssistantResponse(
			[]content.Part{
				dummyprovider.ToolUse("tool-1", "count", json.RawMessage(`{}`)),
			},
			dummyprovider.WithStopReason(model.StopToolUse),
		),
		dummyprovider.TextResponse("done"),
	)
	tool := &recordingTool{}
	agent, err := blades.NewAgent(
		"assistant",
		blades.WithModel(provider),
		blades.WithTools(tool),
		blades.WithPolicy(policy.PolicyFunc(func(context.Context, policy.ToolRequest) (policy.Decision, error) {
			return policy.Decision{Action: policy.Ask, Reason: "approval required"}, nil
		})),
	)
	assert.NoError(t, err)

	sess := session.NewSession()
	ctx := session.NewContext(context.Background(), sess)
	inputs := make(chan event.Input, 1)
	inputs <- event.NewPromptText("run")
	close(inputs)

	outputs, err := collectAllAgentOutputs(ctx, agent, inputs)
	assert.NoError(t, err)
	assert.Equal(t, 0, tool.Calls())

	toolEnd, ok := findToolEnd(outputs, "tool-1")
	assert.True(t, ok)
	assert.True(t, toolEnd.IsError)
	assert.Equal(t, "approval required", textFromParts(toolEnd.Parts))

	messages, err := sess.Messages(ctx)
	assert.NoError(t, err)
	if assert.Len(t, messages, 4) {
		assert.Equal(t, "approval required", toolResultText(messages[2].Parts))
	}
}

func TestLLMAgentExecutesToolBatchConcurrently(t *testing.T) {
	provider := dummyprovider.NewProvider(
		dummyprovider.AssistantResponse(
			[]content.Part{
				dummyprovider.ToolUse("slow-1", "slow", json.RawMessage(`{}`)),
				dummyprovider.ToolUse("slow-2", "slow", json.RawMessage(`{}`)),
			},
			dummyprovider.WithStopReason(model.StopToolUse),
		),
		dummyprovider.TextResponse("done"),
	)
	tool := &concurrencyTool{}
	agent, err := blades.NewAgent(
		"assistant",
		blades.WithModel(provider),
		blades.WithTools(tool),
	)
	assert.NoError(t, err)

	inputs := make(chan event.Input, 1)
	inputs <- event.NewPromptText("run")
	close(inputs)

	outputs, err := collectAllAgentOutputs(context.Background(), agent, inputs)
	assert.NoError(t, err)
	assert.Equal(t, 2, tool.Calls())
	assert.Greater(t, tool.MaxActive(), 1)
	lifecycle := toolLifecycle(outputs)
	if assert.GreaterOrEqual(t, len(lifecycle), 4) {
		assert.Equal(t, []string{
			"start:slow-1",
			"start:slow-2",
		}, lifecycle[:2])
		assert.Contains(t, lifecycle[2:], "end:slow-1")
		assert.Contains(t, lifecycle[2:], "end:slow-2")
	}
}

func TestLLMAgentParallelToolStartsBeforeAnyEnd(t *testing.T) {
	provider := dummyprovider.NewProvider(
		dummyprovider.AssistantResponse(
			[]content.Part{
				dummyprovider.ToolUse("slow-1", "slow", json.RawMessage(`{}`)),
				dummyprovider.ToolUse("slow-2", "slow", json.RawMessage(`{}`)),
			},
			dummyprovider.WithStopReason(model.StopToolUse),
		),
		dummyprovider.TextResponse("done"),
	)
	tool := &concurrencyTool{}
	agent, err := blades.NewAgent(
		"assistant",
		blades.WithModel(provider),
		blades.WithTools(tool),
	)
	assert.NoError(t, err)

	inputs := make(chan event.Input, 1)
	inputs <- event.NewPromptText("run")
	close(inputs)

	outputs, err := collectAllAgentOutputs(context.Background(), agent, inputs)
	assert.NoError(t, err)
	assert.Equal(t, 2, tool.Calls())
	assert.Greater(t, tool.MaxActive(), 1)
	lifecycle := toolLifecycle(outputs)
	if assert.GreaterOrEqual(t, len(lifecycle), 4) {
		assert.Equal(t, []string{
			"start:slow-1",
			"start:slow-2",
		}, lifecycle[:2])
	}
}

func TestLLMAgentParallelToolEndsInCompletionOrder(t *testing.T) {
	provider := dummyprovider.NewProvider(
		dummyprovider.AssistantResponse(
			[]content.Part{
				dummyprovider.ToolUse("slow", "delay", json.RawMessage(`{}`)),
				dummyprovider.ToolUse("fast", "delay", json.RawMessage(`{}`)),
			},
			dummyprovider.WithStopReason(model.StopToolUse),
		),
		dummyprovider.TextResponse("done"),
	)
	tool := &delayedTool{delays: map[string]time.Duration{"slow": 50 * time.Millisecond}}
	sess := session.NewSession()
	ctx := session.NewContext(context.Background(), sess)
	agent, err := blades.NewAgent(
		"assistant",
		blades.WithModel(provider),
		blades.WithTools(tool),
	)
	assert.NoError(t, err)

	inputs := make(chan event.Input, 1)
	inputs <- event.NewPromptText("run")
	close(inputs)

	outputs, err := collectAllAgentOutputs(ctx, agent, inputs)
	assert.NoError(t, err)
	assert.Equal(t, []string{
		"start:slow",
		"start:fast",
		"end:fast",
		"end:slow",
	}, toolLifecycle(outputs))

	messages, err := sess.Messages(ctx)
	assert.NoError(t, err)
	if assert.Len(t, messages, 4) {
		assert.Equal(t, []string{"slow", "fast"}, toolResultTexts(messages[2].Parts))
	}
}

func TestLLMAgentToolActionUsesSourceOrder(t *testing.T) {
	provider := dummyprovider.NewProvider(
		dummyprovider.AssistantResponse(
			[]content.Part{
				dummyprovider.ToolUse("exit-1", "exit", json.RawMessage(`{}`)),
				dummyprovider.ToolUse("handoff-1", "handoff", json.RawMessage(`{}`)),
			},
			dummyprovider.WithStopReason(model.StopToolUse),
		),
	)
	agent, err := blades.NewAgent(
		"assistant",
		blades.WithModel(provider),
		blades.WithTools(
			actionTool{name: "exit", err: &tools.ErrLoopExit{Escalate: true}},
			actionTool{name: "handoff", err: &tools.ErrHandoff{Agent: "next"}},
		),
	)
	assert.NoError(t, err)

	inputs := make(chan event.Input, 1)
	inputs <- event.NewPromptText("run")
	close(inputs)

	outputs, err := collectAllAgentOutputs(context.Background(), agent, inputs)
	assert.NoError(t, err)
	turnEnd, ok := lastTurnEnd(outputs)
	assert.True(t, ok)
	assert.Equal(t, event.LoopExit{Escalate: true}, turnEnd.Action)
}

func TestLLMAgentClosedInputDoesNotAbortActiveTurn(t *testing.T) {
	provider := dummyprovider.NewProvider(dummyprovider.TextResponse("done"))
	agent, err := blades.NewAgent("assistant", blades.WithModel(provider))
	assert.NoError(t, err)

	inputs := make(chan event.Input, 1)
	inputs <- event.NewPromptText("start")
	close(inputs)

	outputs, err := collectAllAgentOutputs(context.Background(), agent, inputs)
	assert.NoError(t, err)

	turnEnd, ok := lastTurnEnd(outputs)
	assert.True(t, ok)
	assert.Equal(t, event.StopEnd, turnEnd.StopReason)
	assert.Equal(t, "done", textFromParts(turnEnd.Parts))
}

func TestLLMAgentQueuesPromptArrivingDuringActiveTurn(t *testing.T) {
	provider := dummyprovider.NewProvider(
		dummyprovider.TextResponse("first"),
		dummyprovider.TextResponse("second"),
	)
	agent, err := blades.NewAgent("assistant", blades.WithModel(provider))
	assert.NoError(t, err)

	sess := session.NewSession()
	ctx := session.NewContext(context.Background(), sess)
	inputs := make(chan event.Input, 2)
	inputs <- event.NewPromptText("one")
	inputs <- event.NewPromptText("two")
	close(inputs)

	outputs, err := collectAllAgentOutputs(ctx, agent, inputs)
	assert.NoError(t, err)

	turns := turnEnds(outputs)
	if assert.Len(t, turns, 2) {
		assert.Equal(t, "first", textFromParts(turns[0].Parts))
		assert.Equal(t, "second", textFromParts(turns[1].Parts))
	}
	assert.Equal(t, 2, provider.CallCount())

	messages, err := sess.Messages(ctx)
	assert.NoError(t, err)
	if assert.Len(t, messages, 4) {
		assert.Equal(t, "one", textFromParts(messages[0].Parts))
		assert.Equal(t, "first", textFromParts(messages[1].Parts))
		assert.Equal(t, "two", textFromParts(messages[2].Parts))
		assert.Equal(t, "second", textFromParts(messages[3].Parts))
	}
}

func TestLLMAgentSteerContinuesCurrentTurn(t *testing.T) {
	provider := dummyprovider.NewProvider(
		dummyprovider.TextResponse("draft"),
		dummyprovider.TextResponse("final"),
	)
	agent, err := blades.NewAgent("assistant", blades.WithModel(provider))
	assert.NoError(t, err)

	sess := session.NewSession()
	ctx := session.NewContext(context.Background(), sess)
	inputs := make(chan event.Input, 2)
	inputs <- event.NewPromptText("start")
	inputs <- event.NewSteerText("revise")
	close(inputs)

	outputs, err := collectAllAgentOutputs(ctx, agent, inputs)
	assert.NoError(t, err)

	turns := turnEnds(outputs)
	if assert.Len(t, turns, 1) {
		assert.Equal(t, event.StopEnd, turns[0].StopReason)
		assert.Equal(t, "final", textFromParts(turns[0].Parts))
	}
	assert.Equal(t, 2, provider.CallCount())

	messages, err := sess.Messages(ctx)
	assert.NoError(t, err)
	if assert.Len(t, messages, 4) {
		assert.Equal(t, "start", textFromParts(messages[0].Parts))
		assert.Equal(t, "draft", textFromParts(messages[1].Parts))
		assert.Equal(t, "revise", textFromParts(messages[2].Parts))
		assert.Equal(t, "final", textFromParts(messages[3].Parts))
	}
}

func TestLLMAgentSteerDuringToolWaveContinuesCurrentTurn(t *testing.T) {
	releaseTool := make(chan struct{})
	provider := dummyprovider.NewProvider(
		dummyprovider.AssistantResponse(
			[]content.Part{
				dummyprovider.ToolUse("block-1", "block", json.RawMessage(`{}`)),
			},
			dummyprovider.WithStopReason(model.StopToolUse),
		),
		dummyprovider.TextResponse("final"),
	)
	agent, err := blades.NewAgent(
		"assistant",
		blades.WithModel(provider),
		blades.WithTools(blockingTool{name: "block", release: releaseTool}),
	)
	assert.NoError(t, err)

	sess := session.NewSession()
	ctx, cancel := context.WithTimeout(session.NewContext(context.Background(), sess), time.Second)
	defer cancel()
	inputs := make(chan event.Input, 2)
	inputs <- event.NewPromptText("start")

	outputs, err := collectAllAgentOutputsWithToolStartInput(ctx, agent, inputs, "block-1", event.NewSteerText("revise"), func() {
		close(releaseTool)
	})
	assert.NoError(t, err)

	turns := turnEnds(outputs)
	if assert.Len(t, turns, 1) {
		assert.Equal(t, event.StopEnd, turns[0].StopReason)
		assert.Equal(t, "final", textFromParts(turns[0].Parts))
	}
	assert.Equal(t, 2, provider.CallCount())

	messages, err := sess.Messages(ctx)
	assert.NoError(t, err)
	if assert.Len(t, messages, 5) {
		assert.Equal(t, model.RoleUser, messages[0].Role)
		assert.Equal(t, model.RoleAssistant, messages[1].Role)
		assert.Equal(t, model.RoleTool, messages[2].Role)
		assert.Equal(t, model.RoleUser, messages[3].Role)
		assert.Equal(t, model.RoleAssistant, messages[4].Role)
		assert.Equal(t, "start", textFromParts(messages[0].Parts))
		assert.Equal(t, "released", toolResultText(messages[2].Parts))
		assert.Equal(t, "revise", textFromParts(messages[3].Parts))
		assert.Equal(t, "final", textFromParts(messages[4].Parts))
	}
}

func TestLLMAgentPromptDuringToolWaveStartsNextTurn(t *testing.T) {
	releaseTool := make(chan struct{})
	provider := dummyprovider.NewProvider(
		dummyprovider.AssistantResponse(
			[]content.Part{
				dummyprovider.ToolUse("block-1", "block", json.RawMessage(`{}`)),
			},
			dummyprovider.WithStopReason(model.StopToolUse),
		),
		dummyprovider.TextResponse("first final"),
		dummyprovider.TextResponse("second final"),
	)
	agent, err := blades.NewAgent(
		"assistant",
		blades.WithModel(provider),
		blades.WithTools(blockingTool{name: "block", release: releaseTool}),
	)
	assert.NoError(t, err)

	sess := session.NewSession()
	ctx, cancel := context.WithTimeout(session.NewContext(context.Background(), sess), time.Second)
	defer cancel()
	inputs := make(chan event.Input, 2)
	inputs <- event.NewPromptText("start")

	outputs, err := collectAllAgentOutputsWithToolStartInput(ctx, agent, inputs, "block-1", event.NewPromptText("next"), func() {
		close(releaseTool)
	})
	assert.NoError(t, err)

	turns := turnEnds(outputs)
	if assert.Len(t, turns, 2) {
		assert.Equal(t, event.StopEnd, turns[0].StopReason)
		assert.Equal(t, "first final", textFromParts(turns[0].Parts))
		assert.Equal(t, event.StopEnd, turns[1].StopReason)
		assert.Equal(t, "second final", textFromParts(turns[1].Parts))
	}
	assert.Equal(t, 3, provider.CallCount())

	messages, err := sess.Messages(ctx)
	assert.NoError(t, err)
	if assert.Len(t, messages, 6) {
		assert.Equal(t, model.RoleUser, messages[0].Role)
		assert.Equal(t, model.RoleAssistant, messages[1].Role)
		assert.Equal(t, model.RoleTool, messages[2].Role)
		assert.Equal(t, model.RoleAssistant, messages[3].Role)
		assert.Equal(t, model.RoleUser, messages[4].Role)
		assert.Equal(t, model.RoleAssistant, messages[5].Role)
		assert.Equal(t, "start", textFromParts(messages[0].Parts))
		assert.Equal(t, "released", toolResultText(messages[2].Parts))
		assert.Equal(t, "first final", textFromParts(messages[3].Parts))
		assert.Equal(t, "next", textFromParts(messages[4].Parts))
		assert.Equal(t, "second final", textFromParts(messages[5].Parts))
	}
}

func TestLLMAgentAbortOnlyEndsCurrentTurn(t *testing.T) {
	provider := dummyprovider.NewProvider(
		dummyprovider.TextResponse("first"),
		dummyprovider.TextResponse("second"),
	)
	agent, err := blades.NewAgent("assistant", blades.WithModel(provider))
	assert.NoError(t, err)

	inputs := make(chan event.Input, 3)
	inputs <- event.NewPromptText("one")
	inputs <- event.NewPromptText("two")
	inputs <- event.Abort{Reason: "stop current"}
	close(inputs)

	outputs, err := collectAllAgentOutputs(context.Background(), agent, inputs)
	assert.NoError(t, err)

	turns := turnEnds(outputs)
	if assert.Len(t, turns, 2) {
		assert.Equal(t, event.StopAbort, turns[0].StopReason)
		assert.Equal(t, "first", textFromParts(turns[0].Parts))
		assert.Equal(t, event.StopEnd, turns[1].StopReason)
		assert.Equal(t, "second", textFromParts(turns[1].Parts))
	}
}

func collectAgentOutputs(ctx context.Context, agent blades.Agent, inputs <-chan event.Input) ([]event.Output, error) {
	outputs, err := agent.Run(ctx, inputs)
	if err != nil {
		return nil, err
	}

	var collected []event.Output
	for output := range outputs {
		collected = append(collected, output)
		if _, ok := output.(event.TurnEnd); ok {
			return collected, nil
		}
	}
	return collected, nil
}

func promptInputs(text string) <-chan event.Input {
	inputs := make(chan event.Input, 1)
	inputs <- event.NewPromptText(text)
	close(inputs)
	return inputs
}

func collectAllAgentOutputs(ctx context.Context, agent blades.Agent, inputs <-chan event.Input) ([]event.Output, error) {
	outputs, err := agent.Run(ctx, inputs)
	if err != nil {
		return nil, err
	}

	var collected []event.Output
	for output := range outputs {
		collected = append(collected, output)
	}
	return collected, nil
}

func collectAllAgentOutputsWithToolStartInput(ctx context.Context, agent blades.Agent, inputs chan event.Input, toolID string, in event.Input, release func()) ([]event.Output, error) {
	outputs, err := agent.Run(ctx, inputs)
	if err != nil {
		return nil, err
	}

	var collected []event.Output
	sent := false
	for output := range outputs {
		collected = append(collected, output)
		toolStart, ok := output.(event.ToolStart)
		if !ok || toolStart.ID != toolID || sent {
			continue
		}
		inputs <- in
		close(inputs)
		release()
		sent = true
	}
	return collected, nil
}

func countToolStarts(outputs []event.Output, id string) int {
	var count int
	for _, output := range outputs {
		toolStart, ok := output.(event.ToolStart)
		if ok && toolStart.ID == id {
			count++
		}
	}
	return count
}

func findToolEnd(outputs []event.Output, id string) (event.ToolEnd, bool) {
	for _, output := range outputs {
		toolEnd, ok := output.(event.ToolEnd)
		if ok && toolEnd.ID == id {
			return toolEnd, true
		}
	}
	return event.ToolEnd{}, false
}

func toolLifecycle(outputs []event.Output) []string {
	var lifecycle []string
	for _, output := range outputs {
		switch v := output.(type) {
		case event.ToolStart:
			lifecycle = append(lifecycle, "start:"+v.ID)
		case event.ToolEnd:
			lifecycle = append(lifecycle, "end:"+v.ID)
		}
	}
	return lifecycle
}

func lastTurnEnd(outputs []event.Output) (event.TurnEnd, bool) {
	var turnEnd event.TurnEnd
	found := false
	for _, output := range outputs {
		next, ok := output.(event.TurnEnd)
		if ok {
			turnEnd = next
			found = true
		}
	}
	return turnEnd, found
}

func turnEnds(outputs []event.Output) []event.TurnEnd {
	var turns []event.TurnEnd
	for _, output := range outputs {
		turnEnd, ok := output.(event.TurnEnd)
		if ok {
			turns = append(turns, turnEnd)
		}
	}
	return turns
}

func textFromParts(parts []content.Part) string {
	var text string
	for _, part := range parts {
		if textPart, ok := part.(content.Text); ok {
			text += textPart.Text
		}
	}
	return text
}

func toolResultText(parts []content.Part) string {
	for _, part := range parts {
		toolResult, ok := part.(content.ToolResult)
		if ok {
			return textFromParts(toolResult.Parts)
		}
	}
	return ""
}

func toolResultTexts(parts []content.Part) []string {
	var texts []string
	for _, part := range parts {
		toolResult, ok := part.(content.ToolResult)
		if ok {
			texts = append(texts, textFromParts(toolResult.Parts))
		}
	}
	return texts
}

type rewriteToolInputHook struct {
	hook.Noop
	toolName string
}

func (h *rewriteToolInputHook) BeforeTool(_ context.Context, call *hook.ToolCall) error {
	if call.Tool != nil {
		h.toolName = call.Tool.Spec().Name
	}
	call.Input = json.RawMessage(`{"expression":"2 + 3"}`)
	return nil
}

type rewriteToolResultHook struct {
	hook.Noop
	err error
}

func (h *rewriteToolResultHook) AfterTool(_ context.Context, _ *hook.ToolCall, result *tools.Result, err error) error {
	h.err = err
	result.Parts = []content.Part{content.Text{Text: "redacted"}}
	return nil
}

type runningAgentSnapshot struct {
	ok          bool
	name        string
	description string
	hasParent   bool
	parentName  string
	rootName    string
}

type runningAgentCapture struct {
	mu       sync.Mutex
	snapshot runningAgentSnapshot
}

func (c *runningAgentCapture) Capture(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snapshot = snapshotRunningAgent(ctx)
}

func (c *runningAgentCapture) Snapshot() runningAgentSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshot
}

func snapshotRunningAgent(ctx context.Context) runningAgentSnapshot {
	ac, ok := blades.FromContext(ctx)
	if !ok {
		return runningAgentSnapshot{}
	}
	snapshot := runningAgentSnapshot{
		ok:          true,
		name:        ac.Name(),
		description: ac.Description(),
		rootName:    ac.Root().Name(),
	}
	if parent, ok := ac.Parent(); ok {
		snapshot.hasParent = true
		snapshot.parentName = parent.Name()
	}
	return snapshot
}

func assertRunningAgentSnapshot(t *testing.T, snapshot runningAgentSnapshot, name, description string, hasParent bool, parentName, rootName string) {
	t.Helper()
	assert.True(t, snapshot.ok)
	assert.Equal(t, name, snapshot.name)
	assert.Equal(t, description, snapshot.description)
	assert.Equal(t, hasParent, snapshot.hasParent)
	assert.Equal(t, parentName, snapshot.parentName)
	assert.Equal(t, rootName, snapshot.rootName)
}

type runningAgentCaptureHook struct {
	hook.Noop
	capture *runningAgentCapture
}

func (h *runningAgentCaptureHook) BeforeModel(ctx context.Context, _ *model.Request) error {
	h.capture.Capture(ctx)
	return nil
}

type mutateSystemHook struct {
	hook.Noop
	system string
}

func (h *mutateSystemHook) BeforeModel(_ context.Context, req *model.Request) error {
	req.System = h.system
	return nil
}

type runningAgentCaptureTool struct {
	capture *runningAgentCapture
}

func (t *runningAgentCaptureTool) Spec() tools.ToolSpec {
	return tools.ToolSpec{Name: "capture", Description: "Capture running agent"}
}

func (t *runningAgentCaptureTool) Handle(ctx context.Context, _ json.RawMessage) (*tools.Result, error) {
	t.capture.Capture(ctx)
	return tools.TextResult("captured"), nil
}

type requestCaptureHook struct {
	hook.Noop
	mu     sync.Mutex
	system string
}

func (h *requestCaptureHook) BeforeModel(_ context.Context, req *model.Request) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.system = req.System
	return nil
}

func (h *requestCaptureHook) System() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.system
}

type recordingTool struct {
	mu    sync.Mutex
	calls int
}

func (t *recordingTool) Spec() tools.ToolSpec {
	return tools.ToolSpec{Name: "count", Description: "Count invocations"}
}

func (t *recordingTool) Handle(context.Context, json.RawMessage) (*tools.Result, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls++
	return tools.TextResult("executed"), nil
}

func (t *recordingTool) Calls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

type concurrencyTool struct {
	mu        sync.Mutex
	calls     int
	active    int
	maxActive int
}

func (t *concurrencyTool) Spec() tools.ToolSpec {
	return tools.ToolSpec{Name: "slow", Description: "Slow tool"}
}

func (t *concurrencyTool) Handle(context.Context, json.RawMessage) (*tools.Result, error) {
	t.mu.Lock()
	t.calls++
	t.active++
	if t.active > t.maxActive {
		t.maxActive = t.active
	}
	t.mu.Unlock()

	time.Sleep(10 * time.Millisecond)

	t.mu.Lock()
	t.active--
	t.mu.Unlock()

	return tools.TextResult("ok"), nil
}

func (t *concurrencyTool) Calls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

func (t *concurrencyTool) MaxActive() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.maxActive
}

type delayedTool struct {
	delays map[string]time.Duration
}

func (t *delayedTool) Spec() tools.ToolSpec {
	return tools.ToolSpec{Name: "delay", Description: "Delay by tool call ID"}
}

func (t *delayedTool) Handle(ctx context.Context, _ json.RawMessage) (*tools.Result, error) {
	tc, ok := tools.FromContext(ctx)
	if !ok {
		return tools.TextResult("missing tool context"), nil
	}
	if delay := t.delays[tc.ID()]; delay > 0 {
		time.Sleep(delay)
	}
	return tools.TextResult(tc.ID()), nil
}

type blockingTool struct {
	name    string
	release <-chan struct{}
}

func (t blockingTool) Spec() tools.ToolSpec {
	return tools.ToolSpec{Name: t.name, Description: "Block until released"}
}

func (t blockingTool) Handle(ctx context.Context, _ json.RawMessage) (*tools.Result, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-t.release:
		return tools.TextResult("released"), nil
	}
}

type actionTool struct {
	name string
	err  error
}

func (t actionTool) Spec() tools.ToolSpec {
	return tools.ToolSpec{Name: t.name, Description: t.name}
}

func (t actionTool) Handle(context.Context, json.RawMessage) (*tools.Result, error) {
	return nil, t.err
}

type captureProvider struct {
	mu        sync.Mutex
	responses []*model.Response
	requests  []*model.Request
}

type failingCountingProvider struct {
	*captureProvider
}

func (p *failingCountingProvider) CountTokens(context.Context, *model.Request) (model.TokenCount, error) {
	return model.TokenCount{}, errors.New("provider token counter should not be used")
}

func countRequestTokens(_ context.Context, req *model.Request) (model.TokenCount, error) {
	var messages int64
	for _, msg := range req.Messages {
		messages += int64(len(textFromParts(msg.Parts)))
	}
	count := model.TokenCount{
		System:   int64(len(req.System)),
		Messages: messages,
		Tools:    int64(len(req.Tools)),
	}
	count.Input = count.System + count.Messages + count.Tools
	return count, nil
}

func newCaptureProvider(responses ...*model.Response) *captureProvider {
	return &captureProvider{responses: responses}
}

func (p *captureProvider) Name() string {
	return "capture"
}

func (p *captureProvider) Generate(ctx context.Context, req *model.Request) (*model.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, req)
	if len(p.responses) == 0 {
		return nil, dummyprovider.ErrNoResponses
	}
	resp := p.responses[0]
	p.responses = p.responses[1:]
	return resp, nil
}

func (p *captureProvider) Stream(ctx context.Context, req *model.Request) iter.Seq2[*model.Chunk, error] {
	return func(yield func(*model.Chunk, error) bool) {
		resp, err := p.Generate(ctx, req)
		if err != nil {
			yield(nil, err)
			return
		}
		for _, chunk := range dummyprovider.ChunksFromResponse(resp) {
			if err := ctx.Err(); err != nil {
				yield(nil, err)
				return
			}
			if !yield(chunk, nil) {
				return
			}
		}
	}
}

func (p *captureProvider) Requests() []*model.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	cp := make([]*model.Request, len(p.requests))
	copy(cp, p.requests)
	return cp
}

func textFromRequest(req *model.Request) string {
	var text string
	for _, msg := range req.Messages {
		text += textFromParts(msg.Parts)
	}
	return text
}

type recordingSession struct {
	mu       sync.Mutex
	messages []*model.Message
	calls    [][]*model.Message
	state    map[string]any
}

func newRecordingSession() *recordingSession {
	return &recordingSession{state: make(map[string]any)}
}

func (s *recordingSession) ID() string {
	return "recording"
}

func (s *recordingSession) Metadata() map[string]any {
	return map[string]any{}
}

func (s *recordingSession) State() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make(map[string]any, len(s.state))
	for k, v := range s.state {
		cp[k] = v
	}
	return cp
}

func (s *recordingSession) SetState(key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state[key] = value
}

func (s *recordingSession) Append(_ context.Context, msgs ...*model.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, msgs...)
	call := make([]*model.Message, len(msgs))
	copy(call, msgs)
	s.calls = append(s.calls, call)
	return nil
}

func (s *recordingSession) Messages(_ context.Context) ([]*model.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]*model.Message, len(s.messages))
	copy(cp, s.messages)
	return cp, nil
}

func (s *recordingSession) AppendCalls() [][]*model.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([][]*model.Message, len(s.calls))
	for i, call := range s.calls {
		cp[i] = make([]*model.Message, len(call))
		copy(cp[i], call)
	}
	return cp
}
