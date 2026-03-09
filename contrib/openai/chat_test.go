package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/go-kratos/blades"
	openaisdk "github.com/openai/openai-go/v3"
)

func TestToChatCompletionParamsAssistantRole(t *testing.T) {
	t.Parallel()

	model := &chatModel{model: "gpt-test"}
	req := &blades.ModelRequest{
		Messages: []*blades.Message{
			blades.UserMessage("hello"),
			blades.AssistantMessage("world"),
		},
	}
	params, err := model.toChatCompletionParams(false, req)
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

func TestChoiceToResponseMarksToolPartsInProgress(t *testing.T) {
	t.Parallel()

	response, err := choiceToResponse(context.Background(), openaisdk.ChatCompletionNewParams{}, &openaisdk.ChatCompletion{
		Choices: []openaisdk.ChatCompletionChoice{
			{
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

	toolPart, ok := response.Message.Parts[0].(blades.ToolPart)
	if !ok {
		t.Fatalf("part type = %T, want blades.ToolPart", response.Message.Parts[0])
	}
	if got, want := toolPart.Status, blades.StatusInProgress; got != want {
		t.Fatalf("tool status = %q, want %q", got, want)
	}
}

func TestChunkChoiceToResponseMarksToolPartsInProgress(t *testing.T) {
	t.Parallel()

	response, err := chunkChoiceToResponse(context.Background(), []openaisdk.ChatCompletionChunkChoice{
		{
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
	})
	if err != nil {
		t.Fatalf("chunkChoiceToResponse returned error: %v", err)
	}

	toolPart, ok := response.Message.Parts[0].(blades.ToolPart)
	if !ok {
		t.Fatalf("part type = %T, want blades.ToolPart", response.Message.Parts[0])
	}
	if got, want := toolPart.Status, blades.StatusInProgress; got != want {
		t.Fatalf("tool status = %q, want %q", got, want)
	}
}
