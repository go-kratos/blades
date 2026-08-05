package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"reflect"
	"slices"
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
			dummyprovider.WithResponseUsage(model.Usage{TotalInputTokens: 10, TotalOutputTokens: 3, TotalTokens: 13}),
		),
		dummyprovider.TextResponse(
			"The result is 56088.",
			dummyprovider.WithResponseUsage(model.Usage{TotalInputTokens: 20, TotalOutputTokens: 4, TotalTokens: 24}),
		),
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
	inputs <- event.NewPrompt("What is 123 * 456?")
	close(inputs)

	outputs, err := collectAllAgentOutputs(ctx, agent, inputs)
	assert.NoError(t, err)

	toolEnd, ok := findToolEnd(outputs, "calc-1")
	assert.True(t, ok)
	assert.Equal(t, "calculate", toolEnd.Name)
	assert.False(t, toolEnd.IsError)
	assert.Equal(t, "123 * 456 = 56088", textFromParts(toolEnd.Parts))
	assert.Equal(t, 1, countToolStarts(outputs, "calc-1"))

	messageEnds := assistantMessageEnds(outputs)
	if assert.Len(t, messageEnds, 2) {
		assert.Equal(t, event.StopToolUse, messageEnds[0].StopReason)
		assert.Equal(t, model.Usage{TotalInputTokens: 10, TotalOutputTokens: 3, TotalTokens: 13}, messageEnds[0].Usage)
		assert.Equal(t, "Let me calculate that.", messageEnds[0].Text())
		assert.Equal(t, event.StopEnd, messageEnds[1].StopReason)
		assert.Equal(t, model.Usage{TotalInputTokens: 20, TotalOutputTokens: 4, TotalTokens: 24}, messageEnds[1].Usage)
		assert.Equal(t, "The result is 56088.", messageEnds[1].Text())
	}

	turns := turnEnds(outputs)
	if assert.Len(t, turns, 2) {
		assert.Equal(t, event.StopToolUse, turns[0].StopReason)
		assert.Equal(t, model.Usage{TotalInputTokens: 10, TotalOutputTokens: 3, TotalTokens: 13}, turns[0].Usage)
		assert.Equal(t, "Let me calculate that.", turns[0].Text())
		assert.Equal(t, event.StopEnd, turns[1].StopReason)
		assert.Equal(t, model.Usage{TotalInputTokens: 20, TotalOutputTokens: 4, TotalTokens: 24}, turns[1].Usage)
		assert.Equal(t, "The result is 56088.", turns[1].Text())
	}
	toolEndIndex := outputIndex(outputs, event.ToolEnd{})
	messageEndIndex := outputIndex(outputs, event.AssistantMessageEnd{})
	turnEndIndex := outputIndex(outputs, event.TurnEnd{})
	assert.NotEqual(t, -1, toolEndIndex)
	assert.NotEqual(t, -1, messageEndIndex)
	assert.NotEqual(t, -1, turnEndIndex)
	assert.Less(t, toolEndIndex, messageEndIndex)
	assert.Less(t, messageEndIndex, turnEndIndex)
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

func TestLLMAgentThinkingAsText(t *testing.T) {
	tests := []struct {
		name          string
		option        blades.AgentOption
		wantText      string
		wantFirstPart content.Part
	}{
		{
			name:          "disabled by default",
			wantFirstPart: content.Thinking{},
		},
		{
			name:          "enabled",
			option:        blades.WithThinkingAsText(true),
			wantText:      "I found the answer.",
			wantFirstPart: content.Text{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := dummyprovider.NewProvider(
				dummyprovider.AssistantResponse([]content.Part{
					dummyprovider.Thinking("I found the answer."),
				}),
			)
			opts := []blades.AgentOption{
				blades.WithModel(provider),
			}
			if tt.option != nil {
				opts = append(opts, tt.option)
			}
			agent, err := blades.NewAgent("assistant", opts...)
			assert.NoError(t, err)

			sess := session.NewSession()
			ctx := session.NewContext(context.Background(), sess)
			outputs, err := collectAllAgentOutputs(ctx, agent, promptInputs("calculate"))
			assert.NoError(t, err)

			var streamedText, streamedThinking string
			for _, output := range outputs {
				switch delta := output.(type) {
				case event.TextDelta:
					streamedText += delta.Text
				case event.ThinkingDelta:
					streamedThinking += delta.Text
				}
			}
			assert.Empty(t, streamedText)
			assert.Equal(t, "I found the answer.", streamedThinking)

			messageEnds := assistantMessageEnds(outputs)
			if assert.Len(t, messageEnds, 1) && assert.Len(t, messageEnds[0].Parts, 1) {
				assert.Equal(t, tt.wantText, messageEnds[0].Text())
				assert.IsType(t, tt.wantFirstPart, messageEnds[0].Parts[0])
			}

			turns := turnEnds(outputs)
			if assert.Len(t, turns, 1) && assert.Len(t, turns[0].Parts, 1) {
				assert.Empty(t, turns[0].Text())
				assert.IsType(t, content.Thinking{}, turns[0].Parts[0])
			}

			messages, err := sess.Messages(ctx)
			assert.NoError(t, err)
			if assert.Len(t, messages, 2) && assert.Len(t, messages[1].Parts, 1) {
				assert.Empty(t, textFromParts(messages[1].Parts))
				assert.IsType(t, content.Thinking{}, messages[1].Parts[0])
			}
		})
	}
}

func TestLLMAgentPreservesRawUsage(t *testing.T) {
	rawUsage := json.RawMessage(`{
		"input_tokens": 10,
		"output_tokens": 3,
		"cache_read_input_tokens": 7,
		"reasoning_tokens": 2
	}`)
	provider := dummyprovider.NewProvider(dummyprovider.TextResponse(
		"done",
		dummyprovider.WithResponseUsage(model.Usage{
			InputCachedTokens:     7,
			InputCacheMissTokens:  3,
			OutputReasoningTokens: 2,
			TotalInputTokens:      10,
			TotalOutputTokens:     3,
			TotalTokens:           13,
			Raw:                   rawUsage,
		}),
	))

	usageCapture := &afterModelUsageCapture{}
	turnCapture := &turnLifecycleCapture{}
	agent, err := blades.NewAgent(
		"usage",
		blades.WithModel(provider),
		blades.WithHooks(usageCapture, turnCapture),
	)
	assert.NoError(t, err)

	outputs, err := collectAllAgentOutputs(context.Background(), agent, promptInputs("hello"))
	assert.NoError(t, err)
	assert.Equal(t, model.Usage{
		InputCachedTokens:     7,
		InputCacheMissTokens:  3,
		OutputReasoningTokens: 2,
		TotalInputTokens:      10,
		TotalOutputTokens:     3,
		TotalTokens:           13,
		Raw:                   rawUsage,
	}, usageCapture.usage)

	messageEnds := assistantMessageEnds(outputs)
	if assert.Len(t, messageEnds, 1) {
		assert.Equal(t, model.Usage{
			InputCachedTokens:     7,
			InputCacheMissTokens:  3,
			OutputReasoningTokens: 2,
			TotalInputTokens:      10,
			TotalOutputTokens:     3,
			TotalTokens:           13,
			Raw:                   rawUsage,
		}, messageEnds[0].Usage)
	}
	turns := turnEnds(outputs)
	if assert.Len(t, turns, 1) {
		assert.Equal(t, model.Usage{
			InputCachedTokens:     7,
			InputCacheMissTokens:  3,
			OutputReasoningTokens: 2,
			TotalInputTokens:      10,
			TotalOutputTokens:     3,
			TotalTokens:           13,
			Raw:                   rawUsage,
		}, turns[0].Usage)
	}
	_, hookEnds, _ := turnCapture.Snapshot()
	if assert.Len(t, hookEnds, 1) {
		assert.Equal(t, model.Usage{
			InputCachedTokens:     7,
			InputCacheMissTokens:  3,
			OutputReasoningTokens: 2,
			TotalInputTokens:      10,
			TotalOutputTokens:     3,
			TotalTokens:           13,
			Raw:                   rawUsage,
		}, hookEnds[0].usage)
	}
}

func TestLLMAgentUsesLastUsageSnapshot(t *testing.T) {
	firstRaw := json.RawMessage(`{"total_tokens":7}`)
	lastRaw := json.RawMessage(`{"total_tokens":13}`)
	provider := &chunkProvider{chunks: []*model.Chunk{
		{Parts: []content.Part{content.Text{Text: "done"}}},
		{
			Usage: &model.Usage{
				TotalInputTokens:  3,
				TotalOutputTokens: 4,
				TotalTokens:       7,
				Raw:               firstRaw,
			},
		},
		{
			Usage: &model.Usage{
				InputCachedTokens: 5,
				TotalInputTokens:  8,
				TotalOutputTokens: 5,
				TotalTokens:       13,
				Raw:               lastRaw,
			},
		},
		{StopReason: model.StopEnd},
	}}

	usageCapture := &afterModelUsageCapture{}
	agent, err := blades.NewAgent(
		"usage-snapshot",
		blades.WithModel(provider),
		blades.WithHooks(usageCapture),
	)
	assert.NoError(t, err)

	outputs, err := collectAllAgentOutputs(context.Background(), agent, promptInputs("hello"))
	assert.NoError(t, err)
	assert.Equal(t, model.Usage{
		InputCachedTokens: 5,
		TotalInputTokens:  8,
		TotalOutputTokens: 5,
		TotalTokens:       13,
		Raw:               lastRaw,
	}, usageCapture.usage)

	messageEnds := assistantMessageEnds(outputs)
	if assert.Len(t, messageEnds, 1) {
		assert.Equal(t, event.StopEnd, messageEnds[0].StopReason)
		assert.Equal(t, model.Usage{
			InputCachedTokens: 5,
			TotalInputTokens:  8,
			TotalOutputTokens: 5,
			TotalTokens:       13,
			Raw:               lastRaw,
		}, messageEnds[0].Usage)
	}
	turns := turnEnds(outputs)
	if assert.Len(t, turns, 1) {
		assert.Equal(t, model.Usage{
			InputCachedTokens: 5,
			TotalInputTokens:  8,
			TotalOutputTokens: 5,
			TotalTokens:       13,
			Raw:               lastRaw,
		}, turns[0].Usage)
	}
}

func TestLLMAgentTurnHooksWrapOneModelCall(t *testing.T) {
	provider := dummyprovider.NewProvider(
		dummyprovider.ToolUseResponse(
			"calc-1",
			"calculate",
			json.RawMessage(`{"expression":"1 + 1"}`),
			dummyprovider.WithResponseUsage(model.Usage{TotalInputTokens: 10, TotalOutputTokens: 2, TotalTokens: 12}),
		),
		dummyprovider.TextResponse(
			"done",
			dummyprovider.WithResponseUsage(model.Usage{TotalInputTokens: 20, TotalOutputTokens: 3, TotalTokens: 23}),
		),
	)
	capture := &turnLifecycleCapture{}
	agent, err := blades.NewAgent(
		"calculator",
		blades.WithModel(provider),
		blades.WithTools(testtools.NewCalculateTool()),
		blades.WithHooks(capture),
	)
	assert.NoError(t, err)

	outputs, err := collectAllAgentOutputs(context.Background(), agent, promptInputs("calculate"))
	assert.NoError(t, err)
	assert.Len(t, turnEnds(outputs), 2)

	starts, ends, toolTurns := capture.Snapshot()
	if assert.Len(t, starts, 2) {
		assert.Equal(t, 1, starts[0].turn)
		assert.IsType(t, event.Prompt{}, starts[0].input)
		assert.Equal(t, 2, starts[1].turn)
		assert.Nil(t, starts[1].input)
	}
	if assert.Len(t, ends, 2) {
		assert.Equal(t, 1, ends[0].turn)
		assert.Equal(t, model.StopToolUse, ends[0].stopReason)
		assert.Equal(t, model.Usage{TotalInputTokens: 10, TotalOutputTokens: 2, TotalTokens: 12}, ends[0].usage)
		assert.Equal(t, 2, ends[1].turn)
		assert.Equal(t, model.StopEnd, ends[1].stopReason)
		assert.Equal(t, model.Usage{TotalInputTokens: 20, TotalOutputTokens: 3, TotalTokens: 23}, ends[1].usage)
	}
	assert.Equal(t, []int{1}, toolTurns)
}

func TestLLMAgentProviderFailureHasNoAssistantMessageEnd(t *testing.T) {
	agent, err := blades.NewAgent("assistant", blades.WithModel(dummyprovider.NewProvider()))
	assert.NoError(t, err)

	outputs, err := collectAllAgentOutputs(context.Background(), agent, promptInputs("hello"))
	assert.NoError(t, err)
	assert.Empty(t, assistantMessageEnds(outputs))

	turns := turnEnds(outputs)
	if assert.Len(t, turns, 1) {
		assert.ErrorIs(t, turns[0].Err, dummyprovider.ErrNoResponses)
	}
	assert.True(t, hasRuntimeError(outputs, dummyprovider.ErrNoResponses))
}

func TestLLMAgentAfterModelFailureDiscardsResponse(t *testing.T) {
	want := errors.New("after model failed")
	provider := dummyprovider.NewProvider(dummyprovider.TextResponse(
		"completed",
		dummyprovider.WithResponseUsage(model.Usage{TotalInputTokens: 4, TotalOutputTokens: 2, TotalTokens: 6}),
	))
	agent, err := blades.NewAgent(
		"assistant",
		blades.WithModel(provider),
		blades.WithHooks(afterModelErrorHook{err: want}),
	)
	assert.NoError(t, err)

	outputs, err := collectAllAgentOutputs(context.Background(), agent, promptInputs("hello"))
	assert.NoError(t, err)
	assert.Empty(t, assistantMessageEnds(outputs))
	turns := turnEnds(outputs)
	if assert.Len(t, turns, 1) {
		assert.ErrorIs(t, turns[0].Err, want)
		assert.Empty(t, turns[0].Parts)
		assert.Equal(t, model.Usage{}, turns[0].Usage)
	}
	assert.True(t, hasRuntimeError(outputs, want))
}

func TestLLMAgentResolvesToolUseNotListedUpfront(t *testing.T) {
	const deferredName = "mcp__time__get_current_time"
	provider := newCaptureProvider(
		dummyprovider.AssistantResponse(
			[]content.Part{
				dummyprovider.ToolUse("call-deferred", deferredName, json.RawMessage(`{"timezone":"Asia/Shanghai"}`)),
			},
			dummyprovider.WithStopReason(model.StopToolUse),
		),
		dummyprovider.TextResponse("done"),
	)
	resolver := &deferredToolResolver{
		visible: []tools.Tool{
			staticTextTool{name: "tool_search", description: "Search deferred tools", result: "found"},
		},
		byName: map[string]tools.Tool{
			deferredName: staticTextTool{
				name:        deferredName,
				description: "Get current time",
				result:      "2026-06-23T12:00:00+08:00",
			},
		},
	}
	agent, err := blades.NewAgent(
		"assistant",
		blades.WithModel(provider),
		blades.WithToolsResolver(resolver),
	)
	assert.NoError(t, err)

	inputs := make(chan event.Input, 1)
	inputs <- event.NewPrompt("what time is it?")
	close(inputs)

	outputs, err := collectAllAgentOutputs(context.Background(), agent, inputs)
	assert.NoError(t, err)

	requests := provider.Requests()
	if assert.Len(t, requests, 2) {
		if assert.Len(t, requests[0].Tools, 1) {
			assert.Equal(t, "tool_search", requests[0].Tools[0].Name)
		}
	}
	toolEnd, ok := findToolEnd(outputs, "call-deferred")
	assert.True(t, ok)
	assert.Equal(t, deferredName, toolEnd.Name)
	assert.False(t, toolEnd.IsError)
	assert.Equal(t, "2026-06-23T12:00:00+08:00", textFromParts(toolEnd.Parts))
	assert.Equal(t, 1, resolver.ResolveCalls(deferredName))
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
	inputs <- event.NewPrompt("hello")
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
		blades.WithContextWindow(model.ContextWindow{}),
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
	inputs <- event.NewPrompt("new")
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
		blades.WithContextWindow(model.ContextWindow{}),
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
	inputs <- event.NewPrompt("new")
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
	inputs <- event.NewPrompt("hello")
	close(inputs)

	outputs, err := collectAllAgentOutputs(context.Background(), agent, inputs)
	assert.NoError(t, err)
	turnEnd, ok := lastTurnEnd(outputs)
	assert.True(t, ok)
	assert.NoError(t, turnEnd.Err)

	assertRunningAgentSnapshot(
		t,
		promptCapture.Snapshot(),
		"assistant",
		"main agent",
		false,
		"",
		"assistant",
	)
	assertRunningAgentSnapshot(
		t,
		hookCapture.Snapshot(),
		"assistant",
		"main agent",
		false,
		"",
		"assistant",
	)
	assertRunningAgentSnapshot(
		t,
		toolCapture.Snapshot(),
		"assistant",
		"main agent",
		false,
		"",
		"assistant",
	)
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
	inputs <- event.NewPrompt("hello")
	close(inputs)

	outputs, err := collectAllAgentOutputs(context.Background(), agent, inputs)
	assert.NoError(t, err)
	turnEnd, ok := lastTurnEnd(outputs)
	assert.True(t, ok)
	assert.NoError(t, turnEnd.Err)
	assert.Equal(t, "main final", textFromParts(turnEnd.Parts))
	assertRunningAgentSnapshot(
		t,
		subCapture.Snapshot(),
		"delegate",
		"sub agent",
		true,
		"main",
		"main",
	)
}

func TestAgentToolReturnsOnlyFinalTurnParts(t *testing.T) {
	subProvider := dummyprovider.NewProvider(
		dummyprovider.AssistantResponse(
			[]content.Part{
				content.Text{Text: "intermediate"},
				dummyprovider.ToolUse("calc-1", "calculate", json.RawMessage(`{"expression":"1 + 1"}`)),
			},
			dummyprovider.WithStopReason(model.StopToolUse),
		),
		dummyprovider.TextResponse("final"),
	)
	sub, err := blades.NewAgent(
		"delegate",
		blades.WithModel(subProvider),
		blades.WithTools(testtools.NewCalculateTool()),
	)
	assert.NoError(t, err)

	result, err := blades.NewAgentTool(sub).Handle(context.Background(), json.RawMessage(`"calculate"`))
	assert.NoError(t, err)
	if assert.NotNil(t, result) {
		assert.Equal(t, "final", textFromParts(result.Parts))
	}
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
	inputs <- event.NewPrompt("hello")
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
	inputs <- event.NewPrompt("hello")
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
	inputs <- event.NewPrompt("calculate")
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
	inputs <- event.NewPrompt("calculate")
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
	inputs <- event.NewPrompt("run")
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
	inputs <- event.NewPrompt("run")
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
	inputs <- event.NewPrompt("run")
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
	inputs <- event.NewPrompt("run")
	close(inputs)

	outputs, err := collectAllAgentOutputs(ctx, agent, inputs)
	assert.NoError(t, err)
	assert.Equal(t, []string{
		"start:slow",
		"start:fast",
		"end:fast",
		"end:slow",
	}, toolLifecycle(outputs))
	messageEndIndex := outputIndex(outputs, event.AssistantMessageEnd{})
	assert.NotEqual(t, -1, messageEndIndex)
	for i, output := range outputs {
		if _, ok := output.(event.ToolEnd); ok {
			assert.Less(t, i, messageEndIndex)
		}
	}

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
	inputs <- event.NewPrompt("run")
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
	inputs <- event.NewPrompt("start")
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
	inputs <- event.NewPrompt("one")
	inputs <- event.NewPrompt("two")
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

func TestLLMAgentSteerStartsNextTurn(t *testing.T) {
	provider := dummyprovider.NewProvider(
		dummyprovider.TextResponse("draft"),
		dummyprovider.TextResponse("final"),
	)
	agent, err := blades.NewAgent("assistant", blades.WithModel(provider))
	assert.NoError(t, err)

	sess := session.NewSession()
	ctx := session.NewContext(context.Background(), sess)
	inputs := make(chan event.Input, 2)
	inputs <- event.NewPrompt("start")
	inputs <- event.NewSteer("revise")
	close(inputs)

	outputs, err := collectAllAgentOutputs(ctx, agent, inputs)
	assert.NoError(t, err)

	turns := turnEnds(outputs)
	if assert.Len(t, turns, 2) {
		assert.Equal(t, "draft", turns[0].Text())
		assert.Equal(t, event.StopEnd, turns[0].StopReason)
		assert.Equal(t, "final", turns[1].Text())
		assert.Equal(t, event.StopEnd, turns[1].StopReason)
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

func TestLLMAgentSteerDuringToolWaveStartsNextTurn(t *testing.T) {
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
	inputs <- event.NewPrompt("start")

	outputs, err := collectAllAgentOutputsWithToolStartInput(
		ctx,
		agent,
		inputs,
		"block-1",
		event.NewSteer("revise"),
		func() {
			close(releaseTool)
		},
	)
	assert.NoError(t, err)

	turns := turnEnds(outputs)
	if assert.Len(t, turns, 2) {
		assert.Equal(t, event.StopToolUse, turns[0].StopReason)
		assert.Equal(t, event.StopEnd, turns[1].StopReason)
		assert.Equal(t, "final", turns[1].Text())
	}
	assert.Equal(t, 2, provider.CallCount())

	messages, err := sess.Messages(ctx)
	assert.NoError(t, err)
	if assert.Len(t, messages, 4) {
		assert.Equal(t, model.RoleUser, messages[0].Role)
		assert.Equal(t, model.RoleAssistant, messages[1].Role)
		// The steer merges into the trailing tool message, keeping its tool role
		// while carrying both the tool result and the steer content.
		assert.Equal(t, model.RoleTool, messages[2].Role)
		assert.Equal(t, model.RoleAssistant, messages[3].Role)
		assert.Equal(t, "start", textFromParts(messages[0].Parts))
		assert.Equal(t, "released", toolResultText(messages[2].Parts))
		assert.Equal(t, "revise", textFromParts(messages[2].Parts))
		assert.Equal(t, "final", textFromParts(messages[3].Parts))
	}
}

func TestLLMAgentMultipleSteersDuringToolWavePreserveContentParts(t *testing.T) {
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
	inputs := make(chan event.Input, 3)
	inputs <- event.NewPrompt("start")

	outputs, err := agent.Run(ctx, inputs)
	assert.NoError(t, err)

	var collected []event.Output
	sent := false
	for output := range outputs {
		collected = append(collected, output)
		toolStart, ok := output.(event.ToolStart)
		if !ok || toolStart.ID != "block-1" || sent {
			continue
		}
		// Queue two steers at the same turn boundary; they merge into the trailing
		// tool message with separate parts.
		inputs <- event.NewSteer("revise once")
		inputs <- event.NewSteer(" and twice")
		close(inputs)
		close(releaseTool)
		sent = true
	}

	turns := turnEnds(collected)
	if assert.Len(t, turns, 2) {
		assert.Equal(t, event.StopToolUse, turns[0].StopReason)
		assert.Equal(t, "final", turns[1].Text())
	}

	messages, err := sess.Messages(ctx)
	assert.NoError(t, err)
	if assert.Len(t, messages, 4) {
		// Both steers merge into the trailing tool message, keeping its tool role
		// and preserving each content part alongside the tool result.
		assert.Equal(t, model.RoleTool, messages[2].Role)
		assert.Len(t, messages[2].Parts, 3)
		assert.Equal(t, "revise once and twice", textFromParts(messages[2].Parts))
		assert.Equal(t, model.RoleAssistant, messages[3].Role)
	}
}

func TestLLMAgentPromptDuringToolWaveWaitsForNextInteraction(t *testing.T) {
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
	inputs <- event.NewPrompt("start")

	outputs, err := collectAllAgentOutputsWithToolStartInput(
		ctx,
		agent,
		inputs,
		"block-1",
		event.NewPrompt("next"),
		func() {
			close(releaseTool)
		},
	)
	assert.NoError(t, err)

	turns := turnEnds(outputs)
	if assert.Len(t, turns, 3) {
		assert.Equal(t, event.StopToolUse, turns[0].StopReason)
		assert.Equal(t, event.StopEnd, turns[1].StopReason)
		assert.Equal(t, "first final", turns[1].Text())
		assert.Equal(t, event.StopEnd, turns[2].StopReason)
		assert.Equal(t, "second final", turns[2].Text())
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
	inputs <- event.NewPrompt("one")
	inputs <- event.NewPrompt("two")
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

func promptInputs(text string) <-chan event.Input {
	inputs := make(chan event.Input, 1)
	inputs <- event.NewPrompt(text)
	close(inputs)
	return inputs
}

func collectAllAgentOutputs(
	ctx context.Context,
	agent blades.Agent,
	inputs <-chan event.Input,
) ([]event.Output, error) {
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

func collectAllAgentOutputsWithToolStartInput(
	ctx context.Context,
	agent blades.Agent,
	inputs chan event.Input,
	toolID string,
	in event.Input,
	release func(),
) ([]event.Output, error) {
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

func assistantMessageEnds(outputs []event.Output) []event.AssistantMessageEnd {
	var messageEnds []event.AssistantMessageEnd
	for _, output := range outputs {
		messageEnd, ok := output.(event.AssistantMessageEnd)
		if ok {
			messageEnds = append(messageEnds, messageEnd)
		}
	}
	return messageEnds
}

func hasRuntimeError(outputs []event.Output, want error) bool {
	for _, output := range outputs {
		if runtimeErr, ok := output.(event.Error); ok && errors.Is(runtimeErr.Err, want) {
			return true
		}
	}
	return false
}

func outputIndex(outputs []event.Output, target event.Output) int {
	for i, output := range outputs {
		if reflect.TypeOf(output) == reflect.TypeOf(target) {
			return i
		}
	}
	return -1
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

type turnStartRecord struct {
	turn  int
	input event.Input
}

type turnEndRecord struct {
	turn       int
	stopReason model.StopReason
	usage      model.Usage
}

type turnLifecycleCapture struct {
	hook.Noop
	mu        sync.Mutex
	starts    []turnStartRecord
	ends      []turnEndRecord
	toolTurns []int
}

func (h *turnLifecycleCapture) BeforeTurn(_ context.Context, turn *hook.Turn) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.starts = append(h.starts, turnStartRecord{turn: turn.Turn, input: turn.Input})
	return nil
}

func (h *turnLifecycleCapture) AfterTurn(_ context.Context, turn *hook.Turn, summary *hook.TurnSummary, _ error) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	record := turnEndRecord{
		turn:       turn.Turn,
		stopReason: summary.StopReason,
		usage:      *summary.Usage,
	}
	h.ends = append(h.ends, record)
	return nil
}

func (h *turnLifecycleCapture) BeforeTool(_ context.Context, call *hook.ToolCall) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.toolTurns = append(h.toolTurns, call.Turn)
	return nil
}

func (h *turnLifecycleCapture) Snapshot() ([]turnStartRecord, []turnEndRecord, []int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.starts), slices.Clone(h.ends), slices.Clone(h.toolTurns)
}

type afterModelErrorHook struct {
	hook.Noop
	err error
}

func (h afterModelErrorHook) AfterModel(context.Context, *model.Request, *model.Response, error) error {
	return h.err
}

type afterModelUsageCapture struct {
	hook.Noop
	usage model.Usage
}

func (h *afterModelUsageCapture) AfterModel(
	_ context.Context,
	_ *model.Request,
	resp *model.Response,
	_ error,
) error {
	h.usage = resp.Usage
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

func assertRunningAgentSnapshot(
	t *testing.T,
	snapshot runningAgentSnapshot,
	name string,
	description string,
	hasParent bool,
	parentName string,
	rootName string,
) {
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

type deferredToolResolver struct {
	mu      sync.Mutex
	visible []tools.Tool
	byName  map[string]tools.Tool
	calls   map[string]int
}

func (r *deferredToolResolver) List(context.Context) ([]tools.Tool, error) {
	return append([]tools.Tool(nil), r.visible...), nil
}

func (r *deferredToolResolver) Resolve(_ context.Context, name string) (tools.Tool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.calls == nil {
		r.calls = make(map[string]int)
	}
	r.calls[name]++
	tool := r.byName[name]
	if tool == nil {
		return nil, fmt.Errorf("tool not found: %s", name)
	}
	return tool, nil
}

func (r *deferredToolResolver) ResolveCalls(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[name]
}

type staticTextTool struct {
	name        string
	description string
	result      string
}

func (t staticTextTool) Spec() tools.ToolSpec {
	return tools.ToolSpec{Name: t.name, Description: t.description}
}

func (t staticTextTool) Handle(context.Context, json.RawMessage) (*tools.Result, error) {
	return tools.TextResult(t.result), nil
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

type chunkProvider struct {
	chunks []*model.Chunk
}

func (p *chunkProvider) Name() string {
	return "chunks"
}

func (p *chunkProvider) Generate(ctx context.Context, req *model.Request) (*model.Response, error) {
	return model.Collect(p.Stream(ctx, req))
}

func (p *chunkProvider) Stream(ctx context.Context, _ *model.Request) iter.Seq2[*model.Chunk, error] {
	return func(yield func(*model.Chunk, error) bool) {
		for _, chunk := range p.chunks {
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

func (s *recordingSession) AppendUser(_ context.Context, parts ...content.Part) error {
	if len(parts) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if n := len(s.messages); n > 0 && s.messages[n-1].Role == model.RoleUser {
		last := s.messages[n-1]
		merged := make([]content.Part, 0, len(last.Parts)+len(parts))
		merged = append(merged, last.Parts...)
		merged = append(merged, parts...)
		msg := &model.Message{Role: model.RoleUser, Parts: content.Coalesce(merged)}
		s.messages[n-1] = msg
		s.calls = append(s.calls, []*model.Message{msg})
		return nil
	}
	msg := &model.Message{Role: model.RoleUser, Parts: content.Coalesce(parts)}
	s.messages = append(s.messages, msg)
	s.calls = append(s.calls, []*model.Message{msg})
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
