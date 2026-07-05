package openai

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestChunkToModelChunkMapsReasoningText(t *testing.T) {
	t.Parallel()

	var chunk openaisdk.ChatCompletionChunk
	if err := json.Unmarshal([]byte(`{
		"id":"chatcmpl-reasoning",
		"object":"chat.completion.chunk",
		"created":1,
		"model":"qwen-test",
		"choices":[
			{"index":0,"delta":{"role":"assistant","content":null,"reasoning_text":"先判断聊天上下文"}}
		]
	}`), &chunk); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	got := chunkToModelChunk(chunk)
	if len(got.Parts) != 1 {
		t.Fatalf("len(Parts) = %d, want 1", len(got.Parts))
	}
	thinking, ok := got.Parts[0].(content.Thinking)
	if !ok {
		t.Fatalf("Parts[0] = %T, want content.Thinking", got.Parts[0])
	}
	if thinking.Text != "先判断聊天上下文" {
		t.Fatalf("Thinking text = %q", thinking.Text)
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

func TestChunkChoiceToResponseReturnsToolUses(t *testing.T) {
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
	toolUse, ok := chunk.Parts[0].(content.ToolUse)
	if !ok {
		t.Fatalf("part type = %T, want content.ToolUse", chunk.Parts[0])
	}
	if got, want := toolUse.Name, "get_weather"; got != want {
		t.Fatalf("tool name = %q, want %q", got, want)
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
