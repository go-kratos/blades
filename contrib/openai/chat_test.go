package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
	openaisdk "github.com/openai/openai-go/v3"
)

func TestToChatCompletionParamsAssistantRole(t *testing.T) {
	t.Parallel()

	provider := &chatModel{model: "gpt-test"}
	req := &model.Request{
		Messages: []*model.Message{
			{Role: model.RoleUser, Parts: []content.Part{content.Text{Text: "hello"}}},
			{Role: model.RoleAssistant, Parts: []content.Part{content.Text{Text: "world"}}},
		},
	}
	params, err := provider.toChatCompletionParams(false, req)
	if err != nil {
		t.Fatalf("toChatCompletionParams returned error: %v", err)
	}

	payload, err := json.Marshal(params.Messages)
	if err != nil {
		t.Fatalf("marshal params messages: %v", err)
	}
	if got, want := bytes.Count(payload, []byte(`"role":"assistant"`)), 1; got != want {
		t.Fatalf("assistant role count = %d, want %d; payload=%s", got, want, payload)
	}
	if got, want := bytes.Count(payload, []byte(`"role":"user"`)), 1; got != want {
		t.Fatalf("user role count = %d, want %d; payload=%s", got, want, payload)
	}
}

func TestToChatCompletionParamsParallelToolCalls(t *testing.T) {
	t.Parallel()

	provider := NewChat("gpt-test", WithParallelToolCalls(false)).(*chatModel)
	params, err := provider.toChatCompletionParams(false, &model.Request{})
	if err != nil {
		t.Fatalf("toChatCompletionParams returned error: %v", err)
	}

	payload, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	if !bytes.Contains(payload, []byte(`"parallel_tool_calls":false`)) {
		t.Fatalf("parallel_tool_calls missing from payload: %s", payload)
	}
}

func TestToChatCompletionParamsParallelToolCallsRequestOverridesDefault(t *testing.T) {
	t.Parallel()

	provider := NewChat("gpt-test", WithParallelToolCalls(false)).(*chatModel)
	params, err := provider.toChatCompletionParams(false, &model.Request{
		Options: []model.Option{model.ParallelToolCalls{Enabled: true}},
	})
	if err != nil {
		t.Fatalf("toChatCompletionParams returned error: %v", err)
	}

	payload, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	if !bytes.Contains(payload, []byte(`"parallel_tool_calls":true`)) {
		t.Fatalf("parallel_tool_calls override missing from payload: %s", payload)
	}
}

func TestChoiceToResponseReturnsToolUses(t *testing.T) {
	t.Parallel()

	response, err := choiceToResponse(&openaisdk.ChatCompletion{
		Choices: []openaisdk.ChatCompletionChoice{
			{
				FinishReason: "tool_calls",
				Message: openaisdk.ChatCompletionMessage{
					ToolCalls: []openaisdk.ChatCompletionMessageToolCallUnion{
						{
							ID:   "call_1",
							Type: "function",
							Function: openaisdk.ChatCompletionMessageFunctionToolCallFunction{
								Name:      "get_weather",
								Arguments: `{"city":"Paris"}`,
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("choiceToResponse returned error: %v", err)
	}

	if got, want := response.StopReason, model.StopToolUse; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	toolUse, ok := response.Message.Parts[0].(content.ToolUse)
	if !ok {
		t.Fatalf("part type = %T, want content.ToolUse", response.Message.Parts[0])
	}
	if got, want := toolUse.ID, "call_1"; got != want {
		t.Fatalf("tool id = %q, want %q", got, want)
	}
	if got, want := toolUse.Name, "get_weather"; got != want {
		t.Fatalf("tool name = %q, want %q", got, want)
	}
}

func TestChatStreamAccumulatorEmitsToolUsesAfterDeltasComplete(t *testing.T) {
	t.Parallel()

	accumulator := newChatStreamAccumulator()
	chunks := []openaisdk.ChatCompletionChunk{
		{
			ID: "chatcmpl_1",
			Choices: []openaisdk.ChatCompletionChunkChoice{
				{
					Index: 0,
					Delta: openaisdk.ChatCompletionChunkChoiceDelta{
						ToolCalls: []openaisdk.ChatCompletionChunkChoiceDeltaToolCall{
							{
								Index: 0,
								ID:    "call_1",
								Type:  "function",
								Function: openaisdk.ChatCompletionChunkChoiceDeltaToolCallFunction{
									Name: "web_fetch",
								},
							},
						},
					},
				},
			},
		},
		{
			ID: "chatcmpl_1",
			Choices: []openaisdk.ChatCompletionChunkChoice{
				{
					Index: 0,
					Delta: openaisdk.ChatCompletionChunkChoiceDelta{
						ToolCalls: []openaisdk.ChatCompletionChunkChoiceDeltaToolCall{
							{
								Index: 0,
								Type:  "function",
								Function: openaisdk.ChatCompletionChunkChoiceDeltaToolCallFunction{
									Arguments: `{"url": "https`,
								},
							},
						},
					},
				},
			},
		},
		{
			ID: "chatcmpl_1",
			Choices: []openaisdk.ChatCompletionChunkChoice{
				{
					Index: 0,
					Delta: openaisdk.ChatCompletionChunkChoiceDelta{
						ToolCalls: []openaisdk.ChatCompletionChunkChoiceDeltaToolCall{
							{
								Index: 0,
								Type:  "function",
								Function: openaisdk.ChatCompletionChunkChoiceDeltaToolCallFunction{
									Arguments: `://example.test"}`,
								},
							},
						},
					},
				},
			},
		},
		{
			ID: "chatcmpl_1",
			Choices: []openaisdk.ChatCompletionChunkChoice{
				{
					Index:        0,
					FinishReason: "tool_calls",
					Delta:        openaisdk.ChatCompletionChunkChoiceDelta{},
				},
			},
		},
	}

	var (
		parts      []content.Part
		stopReason model.StopReason
	)
	for i, raw := range chunks {
		chunk, err := accumulator.addChunk(raw)
		if err != nil {
			t.Fatalf("addChunk[%d] returned error: %v", i, err)
		}
		if i < len(chunks)-1 && len(chunk.Parts) != 0 {
			t.Fatalf("addChunk[%d] emitted %d parts before tool call finished", i, len(chunk.Parts))
		}
		parts = append(parts, chunk.Parts...)
		if chunk.StopReason != "" {
			stopReason = chunk.StopReason
		}
	}

	if got, want := stopReason, model.StopToolUse; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := len(parts), 1; got != want {
		t.Fatalf("parts length = %d, want %d", got, want)
	}
	toolUse, ok := parts[0].(content.ToolUse)
	if !ok {
		t.Fatalf("part type = %T, want content.ToolUse", parts[0])
	}
	if got, want := toolUse.ID, "call_1"; got != want {
		t.Fatalf("tool id = %q, want %q", got, want)
	}
	if got, want := toolUse.Name, "web_fetch"; got != want {
		t.Fatalf("tool name = %q, want %q", got, want)
	}
	if got, want := string(toolUse.Input), `{"url": "https://example.test"}`; got != want {
		t.Fatalf("tool input = %q, want %q", got, want)
	}
	if _, err := json.Marshal(toolUse); err != nil {
		t.Fatalf("marshal tool use: %v", err)
	}
}

func TestChatStreamAccumulatorRejectsInvalidFinalToolInputJSON(t *testing.T) {
	t.Parallel()

	accumulator := newChatStreamAccumulator()
	chunks := []openaisdk.ChatCompletionChunk{
		{
			ID: "chatcmpl_1",
			Choices: []openaisdk.ChatCompletionChunkChoice{
				{
					Index: 0,
					Delta: openaisdk.ChatCompletionChunkChoiceDelta{
						ToolCalls: []openaisdk.ChatCompletionChunkChoiceDeltaToolCall{
							{
								Index: 0,
								ID:    "call_1",
								Type:  "function",
								Function: openaisdk.ChatCompletionChunkChoiceDeltaToolCallFunction{
									Name: "web_fetch",
								},
							},
						},
					},
				},
			},
		},
		{
			ID: "chatcmpl_1",
			Choices: []openaisdk.ChatCompletionChunkChoice{
				{
					Index: 0,
					Delta: openaisdk.ChatCompletionChunkChoiceDelta{
						ToolCalls: []openaisdk.ChatCompletionChunkChoiceDeltaToolCall{
							{
								Index: 0,
								Type:  "function",
								Function: openaisdk.ChatCompletionChunkChoiceDeltaToolCallFunction{
									Arguments: `{"url":`,
								},
							},
						},
					},
				},
			},
		},
		{
			ID: "chatcmpl_1",
			Choices: []openaisdk.ChatCompletionChunkChoice{
				{
					Index:        0,
					FinishReason: "tool_calls",
					Delta:        openaisdk.ChatCompletionChunkChoiceDelta{},
				},
			},
		},
	}

	for i, raw := range chunks[:len(chunks)-1] {
		if _, err := accumulator.addChunk(raw); err != nil {
			t.Fatalf("addChunk[%d] returned error: %v", i, err)
		}
	}
	_, err := accumulator.addChunk(chunks[len(chunks)-1])
	if err == nil {
		t.Fatal("addChunk returned nil error")
	}
	if got := err.Error(); !strings.Contains(got, `invalid tool input JSON for tool "web_fetch" (call_1)`) {
		t.Fatalf("error = %q, want invalid tool input JSON error", got)
	}
}

func TestChunkChoiceToResponseReturnsStopReasonOnlyForToolCallDeltas(t *testing.T) {
	t.Parallel()

	chunk := chunkToModelChunk(openaisdk.ChatCompletionChunk{
		Choices: []openaisdk.ChatCompletionChunkChoice{
			{
				FinishReason: "tool_calls",
				Delta: openaisdk.ChatCompletionChunkChoiceDelta{
					ToolCalls: []openaisdk.ChatCompletionChunkChoiceDeltaToolCall{
						{
							ID:   "call_1",
							Type: "function",
							Function: openaisdk.ChatCompletionChunkChoiceDeltaToolCallFunction{
								Name:      "get_weather",
								Arguments: `{"city":"Paris"}`,
							},
						},
					},
				},
			},
		},
	})

	if got, want := chunk.StopReason, model.StopToolUse; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if len(chunk.Parts) != 0 {
		t.Fatalf("parts length = %d, want 0", len(chunk.Parts))
	}
}

func TestGenerateRejectsNilRequest(t *testing.T) {
	t.Parallel()

	provider := &chatModel{model: "gpt-test"}
	_, err := provider.Generate(context.Background(), nil)
	if err == nil {
		t.Fatal("Generate returned nil error")
	}
}
